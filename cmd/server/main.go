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

	"github.com/art-injener/satwatch-go/internal/config"
	"github.com/art-injener/satwatch-go/internal/handlers"
)

const (
	slogKeyError = "error"
)

func main() {
	// Настройка структурированного логгера
	logger := slog.New(slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	slog.SetDefault(logger)

	// Загрузка конфигурации
	cfg := config.Load()
	slog.Info("configuration loaded",
		"port", cfg.Port,
		"observer_lat", cfg.ObserverLat,
		"observer_lon", cfg.ObserverLon,
	)

	// Инициализация обработчиков
	pageHandler, err := handlers.NewPageHandler("templates", true)
	if err != nil {
		slog.Error("failed to initialize page handler", slogKeyError, err)
		os.Exit(1)
	}

	apiHandler := handlers.NewAPIHandler(cfg)

	mux := http.NewServeMux()

	// Статические файлы
	staticFS := http.FileServer(http.Dir("static"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFS))

	// Маршруты страниц
	mux.HandleFunc("GET /", pageHandler.Index)
	mux.HandleFunc("GET /tracking", pageHandler.Tracking)
	mux.HandleFunc("GET /receiver", pageHandler.Receiver)
	mux.HandleFunc("GET /simulation", pageHandler.Simulation)

	// API маршруты
	mux.HandleFunc("GET /api/health", apiHandler.HealthCheck)
	mux.HandleFunc("GET /api/config", apiHandler.GetConfig)

	// Частичные шаблоны (HTMX)
	mux.HandleFunc("GET /partials/passes", func(w http.ResponseWriter, r *http.Request) {
		// TODO: реализовать частичный шаблон таблицы пролётов
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(`<p class="empty-state">Нет запланированных пролётов</p>`)); err != nil {
			slog.Error("failed to write response", slogKeyError, err)
		}
	})

	// Создание сервера с таймаутами
	server := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      loggingMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Канал для сигнализации об ошибках сервера
	serverErr := make(chan error, 1)

	go func() {
		slog.Info("starting server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Ожидание сигнала прерывания или ошибки сервера
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		slog.Error("server error", slogKeyError, err)
		os.Exit(1)
	case sig := <-quit:
		slog.Info("received shutdown signal", "signal", sig)
	}

	slog.Info("shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", slogKeyError, err)
		shutdownCancel()
		os.Exit(1)
	}
	shutdownCancel()

	slog.Info("server stopped gracefully")
}

// loggingMiddleware логирует HTTP запросы.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Обёртка response writer для захвата кода статуса
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Пропуск логирования статических файлов
		if len(r.URL.Path) > 7 && r.URL.Path[:8] == "/static/" {
			return
		}

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.status,
			"duration", time.Since(start),
		)
	})
}

// responseWriter оборачивает http.ResponseWriter для захвата кода статуса.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
