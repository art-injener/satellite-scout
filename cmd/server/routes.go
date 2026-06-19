package main

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/satnogs"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

type routeDeps struct {
	Cfg         *config.Config
	ConfigStore *config.Store
	Templates   fs.FS
	Static      fs.FS

	SSE      *handlers.SSEHub
	Tracking handlers.TrackingServiceInterface
	SatNOGS  *satnogs.Service

	Exclude   handlers.ExclusionAdder
	PassCache handlers.PassCacheInvalidator
	Group     handlers.GroupRefresher
}

// setupRoutes регистрирует все HTTP-маршруты приложения.
func setupRoutes(mux *http.ServeMux, deps *routeDeps) {
	pageHandler, err := handlers.NewPageHandler(
		deps.Templates,
		deps.Cfg.DevMode,
		deps.Cfg.UI.Theme,
		deps.ConfigStore,
	)
	if err != nil {
		slog.Error("failed to initialize page handler", "error", err)
		panic("page handler init failed: " + err.Error())
	}

	apiHandler := handlers.NewAPIHandler(deps.Cfg)
	trackingHandler := handlers.NewTrackingHandler(deps.Tracking)
	settingsHandler := handlers.NewSettingsHandler(deps.ConfigStore)
	sdrHandler := handlers.NewSDRHandler(sdr.NewService())
	exclusionsHandler := handlers.NewExclusionsHandler(deps.Exclude, deps.PassCache, deps.Group)

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(deps.Static))))

	// Страницы.
	mux.HandleFunc("GET /", pageHandler.Index)
	mux.HandleFunc("GET /tracking", pageHandler.Tracking)
	mux.HandleFunc("GET /settings", redirectFound("/tracking?settings=open"))

	// Legacy URL — единая страница сеанса
	mux.HandleFunc("GET /receiver", redirectFound("/tracking"))
	mux.HandleFunc("GET /simulation", redirectFound("/tracking"))

	// API.
	mux.HandleFunc("GET /api/health", apiHandler.HealthCheck)
	mux.HandleFunc("GET /api/config", apiHandler.GetConfig)
	mux.HandleFunc("GET /api/settings", settingsHandler.Get)
	mux.HandleFunc("PUT /api/settings", settingsHandler.Update)
	mux.HandleFunc("GET /api/sdr/devices", sdrHandler.ListDevices)
	mux.HandleFunc("POST /api/sdr/test", sdrHandler.Test)
	mux.HandleFunc("POST /api/tracking/current", trackingHandler.SetCurrent)
	mux.HandleFunc("POST /api/tracking/reset", trackingHandler.ResetCurrent)
	mux.HandleFunc("POST /api/exclusions", exclusionsHandler.Add)
	mux.HandleFunc("GET /api/exclusions", exclusionsHandler.List)
	mux.HandleFunc("DELETE /api/exclusions/{norad}", exclusionsHandler.Delete)

	if deps.SatNOGS != nil {
		satnogsHandler := handlers.NewSatNOGSHandler(deps.SatNOGS)
		mux.HandleFunc("GET /api/satnogs/transmitters/{norad}", satnogsHandler.GetTransmitters)
	}

	mux.Handle("GET /api/sse", deps.SSE)
}

func redirectFound(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, path, http.StatusFound)
	}
}
