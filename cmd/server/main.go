package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/handlers"
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

	// Загрузка конфигурации.
	cfg := config.Load()
	slog.Info("configuration loaded",
		"port", cfg.Port,
		"observer_lat", cfg.ObserverLat,
		"observer_lon", cfg.ObserverLon,
	)

	// Контекст для фоновых сервисов (SSE Hub, TLEStore, Position/Track сервисы).
	svcCtx, svcCancel := context.WithCancel(context.Background())
	defer svcCancel()

	// TLEStore — хранилище TLE с автообновлением.
	tleStore := tracker.NewTLEStore(cfg.TLE)
	if err := tleStore.Start(svcCtx); err != nil {
		slog.Error("failed to start TLE store", "error", err)
	}

	// SSE Hub — единая точка рассылки real-time данных.
	sseHub := handlers.NewSSEHub()
	go sseHub.Run(svcCtx)

	// Наблюдатель (ObserverAlt в метрах → км).
	observer := tracker.NewObserver(cfg.ObserverLat, cfg.ObserverLon, cfg.ObserverAlt/1000.0)

	// Сервис отслеживания спутников — позиции (1/сек), трассы (1/30 сек), авто-трекинг (1/10 сек).
	trackingService := services.NewSatelliteTrackingService(sseHub, tleStore, observer)

	// Сервис пролётов — расчёт и кеширование пролётов спутников.
	passService := services.NewPassService(tleStore, observer)

	// Связываем trackingService с passService для авто-трекинга.
	// При старте автоматически выбирается ближайший по расписанию спутник.
	trackingService.SetPassProvider(passService)

	// Запускаем сервис отслеживания (авто-трекинг включится автоматически).
	go trackingService.Run(svcCtx)

	// Маршруты.
	mux := http.NewServeMux()
	setupRoutes(mux, cfg, sseHub, passService)

	// HTTP-сервер.
	// WriteTimeout не устанавливается глобально, т.к. он убивает SSE-соединения.
	// Таймауты для обычных запросов управляются через middleware/context.
	server := &http.Server{
		Addr:        cfg.Addr(),
		Handler:     loggingMiddleware(mux),
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 120 * time.Second, // Увеличен для SSE
	}

	// Запуск и graceful shutdown.
	run(server, sseHub, tleStore, svcCancel)
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
