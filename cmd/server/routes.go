package main

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/satnogs"
)

// setupRoutes регистрирует все HTTP-маршруты приложения.
// satnogsService может быть nil — в этом случае REST-маршрут /api/satnogs/* не регистрируется.
func setupRoutes(
	mux *http.ServeMux,
	cfg *config.Config,
	sseHub *handlers.SSEHub,
	trackingService handlers.TrackingServiceInterface,
	satnogsService *satnogs.Service,
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
	trackingHandler := handlers.NewTrackingHandler(trackingService)

	// Статические файлы.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Маршруты страниц.
	mux.HandleFunc("GET /", pageHandler.Index)
	mux.HandleFunc("GET /tracking", pageHandler.Tracking)
	mux.HandleFunc("GET /receiver", pageHandler.Receiver)
	mux.HandleFunc("GET /simulation", pageHandler.Simulation)

	// API маршруты.
	mux.HandleFunc("GET /api/health", apiHandler.HealthCheck)
	mux.HandleFunc("GET /api/config", apiHandler.GetConfig)

	// API управления наблюдением (tracking).
	mux.HandleFunc("POST /api/tracking/current", trackingHandler.SetCurrent)
	mux.HandleFunc("POST /api/tracking/reset", trackingHandler.ResetCurrent)

	// API SatNOGS (полный список передатчиков по NORAD ID — для будущего dropdown).
	if satnogsService != nil {
		satnogsHandler := handlers.NewSatNOGSHandler(satnogsService)
		mux.HandleFunc("GET /api/satnogs/transmitters/{norad}", satnogsHandler.GetTransmitters)
	}

	// SSE endpoint — EventSource-совместимый поток данных.
	// WriteTimeout для SSE-соединений отключается per-connection в ServeHTTP.
	mux.Handle("GET /api/sse", sseHub)

}
