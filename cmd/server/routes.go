package main

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/services"
)

// setupRoutes регистрирует все HTTP-маршруты приложения.
func setupRoutes(
	mux *http.ServeMux,
	cfg *config.Config,
	sseHub *handlers.SSEHub,
	passService *services.PassService,
	trackingService handlers.TrackingServiceInterface,
	templatesFS fs.FS,
	staticFS fs.FS,
) {
	// Инициализация обработчиков.
	pageHandler, err := handlers.NewPageHandler(templatesFS, cfg.DevMode, cfg.Theme)
	if err != nil {
		slog.Error("failed to initialize page handler", "error", err)
		panic("page handler init failed: " + err.Error())
	}

	apiHandler := handlers.NewAPIHandler(cfg)
	passHandler := handlers.NewPassHandler(passService)
	trackingHandler := handlers.NewTrackingHandler(trackingService)

	// Статические файлы.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Маршруты страниц.
	mux.HandleFunc("GET /", pageHandler.Index)
	mux.HandleFunc("GET /tracking", pageHandler.Tracking)
	mux.HandleFunc("GET /passes", pageHandler.Passes)
	mux.HandleFunc("GET /receiver", pageHandler.Receiver)
	mux.HandleFunc("GET /simulation", pageHandler.Simulation)

	// API маршруты.
	mux.HandleFunc("GET /api/health", apiHandler.HealthCheck)
	mux.HandleFunc("GET /api/config", apiHandler.GetConfig)

	// API пролётов.
	mux.HandleFunc("GET /api/passes", passHandler.GetPasses)

	// API управления сопровождением.
	mux.HandleFunc("POST /api/tracking/current", trackingHandler.SetCurrent)
	mux.HandleFunc("POST /api/tracking/reset", trackingHandler.ResetCurrent)

	// SSE endpoint — EventSource-совместимый поток данных.
	// WriteTimeout для SSE-соединений отключается per-connection в ServeHTTP.
	mux.Handle("GET /api/sse", sseHub)

	// Частичные шаблоны (HTMX).
	mux.HandleFunc("GET /partials/passes", func(w http.ResponseWriter, r *http.Request) {
		// TODO: реализовать частичный шаблон расписания сеансов наблюдения
		w.Header().Set("Content-Type", "text/html")
		if _, writeErr := w.Write([]byte(`<p class="empty-state">Нет запланированных пролётов</p>`)); writeErr != nil {
			slog.Error("failed to write response", "error", writeErr)
		}
	})
}
