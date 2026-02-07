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

	// Контекст для фоновых сервисов (SSE Hub, будущие Position/Track сервисы).
	svcCtx, svcCancel := context.WithCancel(context.Background())
	defer svcCancel()

	// SSE Hub — единая точка рассылки real-time данных.
	sseHub := handlers.NewSSEHub()
	go sseHub.Run(svcCtx)

	// Маршруты.
	mux := http.NewServeMux()
	setupRoutes(mux, cfg, sseHub)

	// HTTP-сервер.
	server := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      loggingMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Запуск и graceful shutdown.
	run(server, sseHub, svcCancel)
}

// run запускает HTTP-сервер и обрабатывает graceful shutdown.
func run(server *http.Server, sseHub *handlers.SSEHub, svcCancel context.CancelFunc) {
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

	shutdown(server, sseHub, svcCancel)
}

// shutdown выполняет graceful shutdown: фоновые сервисы → HTTP-сервер.
func shutdown(server *http.Server, sseHub *handlers.SSEHub, svcCancel context.CancelFunc) {
	slog.Info("shutting down server...")

	// Останавливаем фоновые сервисы (SSE Hub отключает всех клиентов).
	svcCancel()

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
		os.Exit(1)
	}

	slog.Info("server stopped gracefully")
}
