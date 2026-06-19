package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	assets "github.com/art-injener/satellite-scout"
	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/exclude"
	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/satnogs"
	"github.com/art-injener/satellite-scout/internal/services"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// app собирает все ключевые модули приложения в одном месте.
type app struct {
	cfg         *config.Config
	configStore *config.Store

	templates fs.FS
	static    fs.FS

	sseHub   *handlers.SSEHub                   // рассылка real-time данных
	tleStore *tracker.TLEStore                  // хранилище TLE
	tracking *services.SatelliteTrackingService // отслеживание спутников
	pass     *services.PassService              // расчёт пролётов
	exclude  *exclude.Store                     // исключённые NORAD ID

	satnogs        *satnogs.Service           // интеграция с SatNOGS
	satnogsAdapter *satnogsTransmitterAdapter // адаптер SatNOGS → services
}

// setupLogger — стандартный логгер без сторонних зависимостей.
func setupLogger() {
	logger := slog.New(slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	slog.SetDefault(logger)
}

// loadConfig загружает конфиг: читает data/config.json (или путь из
// SCOUT_CONFIG_PATH), а если файла нет — создаёт его с дефолтами.
func loadConfig() (*config.Store, error) {
	configStore, err := config.Bootstrap(config.ResolveConfigPath())
	if err != nil {
		return nil, err
	}
	cfg := configStore.Get()
	slog.Info("configuration loaded",
		"path", configStore.Path(),
		"port", cfg.Server.Port,
		"dev_mode", cfg.DevMode,
		"observer_lat", cfg.Station.Observer.Lat,
		"observer_lon", cfg.Station.Observer.Lon,
	)
	return configStore, nil
}

// NewApp создаёт все модули приложения и связывает их между собой.
func NewApp(configStore *config.Store) *app {
	cfg := configStore.Get()

	// Выбор источника шаблонов и статики.
	templates, static := resolveAssets(cfg.DevMode)

	// Хранилище TLE с автообновлением.
	tleStore := tracker.NewTLEStore(cfg.TLEStoreConfig())
	// SSEHub - eдиная точка рассылки данных для UI
	sseHub := handlers.NewSSEHub()

	// Наблюдатель
	observer := tracker.NewObserver(
		cfg.Station.Observer.Lat,
		cfg.Station.Observer.Lon,
		cfg.Station.Observer.AltM/1000.0, // Observer.AltM в метрах, переводим в километры для соответствия единицам измерения в TLEStore
	)

	// Считаем позиции, трассы и состав пролетающей над нами группы, шлём данные в SSE Hub.
	trackingService := services.NewSatelliteTrackingService(sseHub, tleStore, observer)

	// Исключённые спутники (NORAD ID) — не показываем их в группе, в пролётах и при ручном выборе.
	// Например, высокоорбитальные спутники могут давать странные траектории трасс в форме загогулины.
	excludeStore := exclude.NewStore(cfg.ExcludeNoradFile)
	slog.Info("exclusions loaded",
		slog.String("file", cfg.ExcludeNoradFile),
		slog.Int("count", len(excludeStore.List())),
	)

	// Расчёт и кеширование пролётов.
	passService := services.NewPassService(tleStore, observer).WithExcluder(excludeStore)

	// Пролёты для авто-трекинга и список исключений.
	trackingService.SetPassProvider(passService)
	trackingService.WithExcluder(excludeStore)

	return &app{
		cfg:         cfg,
		configStore: configStore,
		templates:   templates,
		static:      static,
		sseHub:      sseHub,
		tleStore:    tleStore,
		tracking:    trackingService,
		pass:        passService,
		exclude:     excludeStore,
	}
}

// Run запускает HTTP-сервер и фоновые сервисы, ждёт сигнала завершения.
// Сначала занимает порт — если занят, сразу выходим, без TLE и tracking.
func (a *app) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	setupRoutes(mux, a.routeDeps())
	server := newServer(a.cfg, loggingMiddleware(mux))

	var lc net.ListenConfig
	ln, listenErr := lc.Listen(ctx, "tcp", server.Addr)
	if listenErr != nil {
		slog.Error("server error", "error", listenErr)
		return listenErr
	}

	// TLEStore — фоновое обновление орбит.
	if startErr := a.tleStore.Start(ctx); startErr != nil {
		slog.Error("failed to start TLE store", "error", startErr)
	}
	go a.sseHub.Run(ctx)
	a.setupClientState()

	go a.tracking.Run(ctx)
	a.startSatNOGS(ctx)
	a.startHotReload()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("starting server", "addr", server.Addr)
		if serveErr := server.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serverErr <- serveErr
		}
	}()

	// Ждём: либо сигнал (ctx отменится), либо ошибку сервера.
	select {
	case serveErr := <-serverErr:
		slog.Error("server error", "error", serveErr)
		stop()
		a.shutdown(server)
		return serveErr
	case <-ctx.Done():
		slog.Info("received shutdown signal")
	}

	a.shutdown(server)
	return nil
}

// shutdown останавливает всё в правильном порядке.
func (a *app) shutdown(server *http.Server) {
	slog.Info("shutting down...")

	a.tleStore.Stop()
	slog.Info("TLE store stopped")

	// Ждём, пока SSE Hub отключит всех клиентов (горутина уже получила отмену ctx).
	select {
	case <-a.sseHub.Done():
		slog.Info("SSE hub stopped")
	case <-time.After(5 * time.Second):
		slog.Warn("SSE hub shutdown timeout")
	}

	// Свежий контекст: корневой ctx уже отменён по сигналу.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
		return
	}
	slog.Info("server stopped gracefully")
}

// При подключении SSE-клиента отправляем ему сохранённый tracking_id —
// NORAD ID спутника из прошлой сессии (0, если никого не смотрел).
func (a *app) setupClientState() {
	clientStore := a.tracking.GetClientStateStore()
	a.sseHub.SetOnClientConnect(func(clientID string) []handlers.SSEEvent {
		if clientID == "" {
			return nil
		}
		clientStore.Touch(clientID)
		trackingID := clientStore.GetTracking(clientID)
		payload, marshalErr := json.Marshal(struct {
			TrackingID int   `json:"tracking_id"`
			TS         int64 `json:"ts"`
		}{
			TrackingID: trackingID,
			TS:         time.Now().UTC().UnixMilli(),
		})
		if marshalErr != nil {
			return nil
		}
		return []handlers.SSEEvent{{Type: "client_state_restore", Data: payload}}
	})
}

// Запускаем SatNOGS — частоты и модуляции передатчиков передаются в SSE вместе с трассами спутников.
// Параллельно mock tx_cycle для нижней панели Авто-режима (auto-link).
func (a *app) startSatNOGS(ctx context.Context) {
	if !a.cfg.SatNOGS.Enabled {
		slog.InfoContext(ctx, "satnogs integration disabled (config: satnogs.enabled=false)")
		return
	}

	clientOpts := []satnogs.Option{}
	if a.cfg.SatNOGS.Timeout > 0 {
		clientOpts = append(clientOpts, satnogs.WithTimeout(a.cfg.SatNOGS.Timeout.Duration()))
	}
	if a.cfg.SatNOGS.MaxRetries > 0 {
		clientOpts = append(clientOpts, satnogs.WithMaxRetries(a.cfg.SatNOGS.MaxRetries))
	}
	satnogsClient := satnogs.NewClient(clientOpts...)
	a.satnogs = satnogs.NewService(satnogsClient).
		WithCacheTTL(a.cfg.SatNOGS.CacheTTL.Duration()).
		WithWorkers(a.cfg.SatNOGS.Workers)
	go a.satnogs.Run(ctx)
	a.satnogsAdapter = newSatnogsTransmitterAdapter(a.satnogs)
	a.tracking.SetTransmitterProvider(a.satnogsAdapter)
	slog.InfoContext(ctx, "satnogs integration enabled", "cache_ttl", a.cfg.SatNOGS.CacheTTL.Duration())

	// Шлём события tx_cycle — обновляем таблицу передатчиков в auto-link.
	txCycleMock := services.NewTxCycleMock(a.sseHub, a.tracking, a.satnogsAdapter, services.DefaultTxCycleInterval)
	go txCycleMock.Run(ctx)
}

// startHotReload подписывается на изменения конфига: при правке наблюдателя
// или темы через UI обновляем модули прямо на лету, без перезапуска.
func (a *app) startHotReload() {
	a.configStore.Subscribe(func(old, n *config.Config) {
		if old.Station.Observer != n.Station.Observer {
			newObs := tracker.NewObserver(
				n.Station.Observer.Lat,
				n.Station.Observer.Lon,
				n.Station.Observer.AltM/1000.0,
			)
			a.pass.SetObserver(newObs)
			a.tracking.SetObserver(newObs)
			a.pass.InvalidateCache()
			a.tracking.ForceGroupUpdate()
			slog.Info("observer hot-reloaded",
				slog.Float64("lat", n.Station.Observer.Lat),
				slog.Float64("lon", n.Station.Observer.Lon),
				slog.Float64("alt_m", n.Station.Observer.AltM),
			)
		}
		if old.UI.Theme != n.UI.Theme {
			themePayload, marshalErr := json.Marshal(map[string]string{"theme": n.UI.Theme})
			if marshalErr == nil {
				a.sseHub.Broadcast("theme_changed", themePayload)
				slog.Info("theme hot-reloaded", slog.String("theme", n.UI.Theme))
			}
		}
	})
}

// routeDeps собирает зависимости для setupRoutes.
func (a *app) routeDeps() *routeDeps {
	return &routeDeps{
		Cfg:         a.cfg,
		ConfigStore: a.configStore,
		Templates:   a.templates,
		Static:      a.static,
		SSE:         a.sseHub,
		Tracking:    a.tracking,
		SatNOGS:     a.satnogs,
		Exclude:     a.exclude,
		PassCache:   a.pass,
		Group:       a.tracking,
	}
}

// newServer создаёт HTTP-сервер. WriteTimeout не ставим — он рвёт SSE.
// Таймауты для обычных запросов делаем через middleware/context.
func newServer(cfg *config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:        cfg.Addr(),
		Handler:     handler,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
}

// Выбираем, откуда отдавать шаблоны и статику: с диска (DevMode) или
// из embed в бинарнике. Если в DevMode нет папки templates/ — берём embed.
func resolveAssets(devMode bool) (fs.FS, fs.FS) {
	if devMode {
		if _, err := os.Stat("templates/layouts"); err == nil {
			slog.Info("assets: filesystem (dev mode)")
			return os.DirFS("templates"), os.DirFS("static")
		}
		slog.Warn("assets: templates/ not found in CWD, falling back to embedded FS (dev mode degraded)")
	}

	slog.Info("assets: embedded (production)")

	templatesFS, err := fs.Sub(assets.TemplatesFS, "templates")
	if err != nil {
		slog.Error("failed to create templates sub-FS", "error", err)
		os.Exit(1)
	}
	staticFS, err := fs.Sub(assets.StaticFS, "static")
	if err != nil {
		slog.Error("failed to create static sub-FS", "error", err)
		os.Exit(1)
	}
	return templatesFS, staticFS
}
