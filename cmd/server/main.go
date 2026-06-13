package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
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

func main() {
	// Настройка структурированного логгера.
	logger := slog.New(slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	slog.SetDefault(logger)

	// Загрузка конфигурации: открываем data/config.json (или путь из SS_CONFIG);
	// при первом запуске собираем конфиг из устаревших ENV и записываем файл —
	// дальше единственным источником правды становится файл.
	configStore, err := config.Bootstrap(config.ResolveConfigPath())
	if err != nil {
		slog.Error("failed to bootstrap configuration", "error", err)
		os.Exit(1)
	}
	cfg := configStore.Get()
	slog.Info("configuration loaded",
		"path", configStore.Path(),
		"port", cfg.Server.Port,
		"dev_mode", cfg.DevMode,
		"observer_lat", cfg.Station.Observer.Lat,
		"observer_lon", cfg.Station.Observer.Lon,
	)

	// Выбор источника шаблонов и статики.
	templatesFS, staticFS := resolveAssets(cfg.DevMode)

	// Контекст для фоновых сервисов (SSE Hub, TLEStore, Position/Track сервисы).
	svcCtx, svcCancel := context.WithCancel(context.Background())
	defer svcCancel()

	// TLEStore — хранилище TLE с автообновлением. Конфиг tracker'а собирается
	// из единого config.Config через адаптер TLEStoreConfig().
	tleStore := tracker.NewTLEStore(cfg.TLEStoreConfig())
	if startErr := tleStore.Start(svcCtx); startErr != nil {
		slog.Error("failed to start TLE store", "error", startErr)
	}

	// SSE Hub — единая точка рассылки real-time данных.
	sseHub := handlers.NewSSEHub()
	go sseHub.Run(svcCtx)

	// Наблюдатель (Observer.AltM в метрах → км).
	observer := tracker.NewObserver(
		cfg.Station.Observer.Lat,
		cfg.Station.Observer.Lon,
		cfg.Station.Observer.AltM/1000.0,
	)

	// Сервис отслеживания спутников — позиции (1/сек), трассы (1/30 сек), авто-трекинг (1/10 сек).
	trackingService := services.NewSatelliteTrackingService(sseHub, tleStore, observer)

	// Per-client state: при подключении SSE-клиента отправляем его tracking_id (TRACK-STATE-003).
	clientStore := trackingService.GetClientStateStore()
	sseHub.SetOnClientConnect(func(clientID string) []handlers.SSEEvent {
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

	// Список исключённых NORAD ID — не попадают в группу, список пролётов и ручное наблюдение.
	excludeStore := exclude.NewStore(cfg.ExcludeNoradFile)
	slog.Info("exclusions loaded",
		slog.String("file", cfg.ExcludeNoradFile), slog.Int("count", len(excludeStore.List())))

	// Сервис пролётов — расчёт и кеширование пролётов спутников.
	passService := services.NewPassService(tleStore, observer).WithExcluder(excludeStore)

	// Связываем trackingService с passService для авто-трекинга + жёсткий запрет на наблюдение исключённых.
	trackingService.SetPassProvider(passService)
	trackingService.WithExcluder(excludeStore)

	// SatNOGS — частоты и модуляция передатчиков спутников. Опционально:
	// при satnogs.enabled=false сервис не создаётся, поля freq_mhz/modulation в SSE пустые.
	var satnogsService *satnogs.Service
	var satnogsAdapter *satnogsTransmitterAdapter
	if cfg.SatNOGS.Enabled {
		// Нулевые значения параметров означают «использовать дефолт клиента/сервиса»,
		// поэтому опции применяем только при положительных значениях из конфига.
		clientOpts := []satnogs.Option{}
		if cfg.SatNOGS.Timeout > 0 {
			clientOpts = append(clientOpts, satnogs.WithTimeout(cfg.SatNOGS.Timeout.Duration()))
		}
		if cfg.SatNOGS.MaxRetries > 0 {
			clientOpts = append(clientOpts, satnogs.WithMaxRetries(cfg.SatNOGS.MaxRetries))
		}
		satnogsClient := satnogs.NewClient(clientOpts...)
		satnogsService = satnogs.NewService(satnogsClient).
			WithCacheTTL(cfg.SatNOGS.CacheTTL.Duration()).
			WithWorkers(cfg.SatNOGS.Workers)
		go satnogsService.Run(svcCtx)
		satnogsAdapter = newSatnogsTransmitterAdapter(satnogsService)
		trackingService.SetTransmitterProvider(satnogsAdapter)
		slog.Info("satnogs integration enabled", "cache_ttl", cfg.SatNOGS.CacheTTL.Duration())
	} else {
		slog.Info("satnogs integration disabled (config: satnogs.enabled=false)")
	}

	// Запускаем сервис отслеживания.
	go trackingService.Run(svcCtx)

	// Mock-генератор tx_cycle для UI Авто-режима (auto-link/heat-grid).
	// Запускается только при включённом SatNOGS — без каталога передатчиков
	// генерация бессмысленна. Будет заменён на ScanStrategy (см. ADR-004 §3).
	if satnogsAdapter != nil {
		txCycleMock := services.NewTxCycleMock(sseHub, trackingService, satnogsAdapter, services.DefaultTxCycleInterval)
		go txCycleMock.Run(svcCtx)
	}

	// Hot-reload подписка: при правке наблюдателя/темы через UI настроек
	// обновляем сервисы без перезапуска процесса.
	configStore.Subscribe(func(old, n *config.Config) {
		if old.Station.Observer != n.Station.Observer {
			newObs := tracker.NewObserver(
				n.Station.Observer.Lat,
				n.Station.Observer.Lon,
				n.Station.Observer.AltM/1000.0,
			)
			passService.SetObserver(newObs)
			trackingService.SetObserver(newObs)
			passService.InvalidateCache()
			trackingService.ForceGroupUpdate()
			slog.Info("observer hot-reloaded",
				slog.Float64("lat", n.Station.Observer.Lat),
				slog.Float64("lon", n.Station.Observer.Lon),
				slog.Float64("alt_m", n.Station.Observer.AltM),
			)
		}
		if old.UI.Theme != n.UI.Theme {
			themePayload, marshalErr := json.Marshal(map[string]string{"theme": n.UI.Theme})
			if marshalErr == nil {
				sseHub.Broadcast("theme_changed", themePayload)
				slog.Info("theme hot-reloaded", slog.String("theme", n.UI.Theme))
			}
		}
	})

	// Маршруты.
	mux := http.NewServeMux()
	setupRoutes(mux, &routeDeps{
		Cfg:         cfg,
		ConfigStore: configStore,
		Templates:   templatesFS,
		Static:      staticFS,
		SSE:         sseHub,
		Tracking:    trackingService,
		SatNOGS:     satnogsService,
		Exclude:     excludeStore,
		PassCache:   passService,
		Group:       trackingService,
	})

	// HTTP-сервер.
	// WriteTimeout не устанавливается глобально, т.к. он убивает SSE-соединения.
	// Таймауты для обычных запросов управляются через middleware/context.
	server := &http.Server{
		Addr:        cfg.Addr(),
		Handler:     loggingMiddleware(mux),
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	// Запуск и graceful shutdown.
	run(server, sseHub, tleStore, svcCancel)
}

// resolveAssets возвращает файловые системы для шаблонов и статики.
// DevMode: чтение с диска (горячая перезагрузка). Production: встроенные embed.FS.
// Если в dev-режиме директория templates/ не найдена (бинарник запущен не из корня проекта),
// используется embed.FS как fallback.
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

// run запускает HTTP-сервер и обрабатывает graceful shutdown.
func run(server *http.Server, sseHub *handlers.SSEHub, tleStore *tracker.TLEStore, svcCancel context.CancelFunc) {
	serverErr := make(chan error, 1)

	go func() {
		slog.Info("starting server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Ожидание сигнала прерывания или ошибки сервера.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		slog.Error("server error", "error", err)
		os.Exit(1)
	case sig := <-quit:
		slog.Info("received shutdown signal", "signal", sig)
	}

	shutdown(server, sseHub, tleStore, svcCancel)
}

// shutdown выполняет graceful shutdown: фоновые сервисы → TLEStore → SSE Hub → HTTP-сервер.
func shutdown(server *http.Server, sseHub *handlers.SSEHub, tleStore *tracker.TLEStore, svcCancel context.CancelFunc) {
	slog.Info("shutting down server...")

	// Останавливаем фоновые сервисы (PositionService, TrackService останавливаются по ctx).
	svcCancel()

	// Останавливаем TLEStore (фоновое обновление).
	tleStore.Stop()
	slog.Info("TLE store stopped")

	// Ожидаем остановку SSE Hub (отключает всех клиентов).
	select {
	case <-sseHub.Done():
		slog.Info("SSE hub shutdown complete")
	case <-time.After(5 * time.Second):
		slog.Warn("SSE hub shutdown timeout")
	}

	// Останавливаем HTTP-сервер (ожидаем завершения активных соединений).
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
		return
	}

	slog.Info("server stopped gracefully")
}
