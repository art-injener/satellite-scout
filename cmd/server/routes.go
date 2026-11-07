package main

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/access"
	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/demod"
	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/products"
	"github.com/art-injener/satellite-scout/internal/satnogs"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

type routeDeps struct {
	Cfg         *config.Config
	ConfigStore *config.Store
	Templates   fs.FS
	Static      fs.FS

	SSE         *handlers.SSEHub
	Access      *access.Manager
	Tracking    handlers.TrackingServiceInterface
	SatNOGS     *satnogs.Service
	SDR         *sdr.Service
	ReceiverHub *sdr.ReceiverHub
	Products    *products.Store
	Audio       *demod.AudioHub

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
		slog.Error("failed to initialize page handler", slogKeyError, err)
		panic("page handler init failed: " + err.Error())
	}

	apiHandler := handlers.NewAPIHandler(deps.Cfg)
	trackingHandler := handlers.NewTrackingHandler(deps.Tracking)
	settingsHandler := handlers.NewSettingsHandler(deps.ConfigStore)
	sdrHandler := handlers.NewSDRHandler(deps.SDR, deps.ReceiverHub)
	exclusionsHandler := handlers.NewExclusionsHandler(deps.Exclude, deps.PassCache, deps.Group)
	accessHandler := handlers.NewAccessHandler(deps.Access)
	master := deps.Access.RequireMaster

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
	mux.HandleFunc("PUT /api/settings", master(settingsHandler.Update))
	mux.HandleFunc("GET /api/access", accessHandler.Status)
	mux.HandleFunc("POST /api/access/login", accessHandler.Login)
	mux.HandleFunc("POST /api/access/request", accessHandler.Request)
	mux.HandleFunc("POST /api/access/decide", master(accessHandler.Decide))
	mux.HandleFunc("POST /api/access/revoke", master(accessHandler.Revoke))
	mux.HandleFunc("POST /api/access/release", accessHandler.Release)
	mux.HandleFunc("GET /api/sdr/devices", sdrHandler.ListDevices)
	mux.HandleFunc("POST /api/sdr/test", sdrHandler.Test)
	mux.HandleFunc("POST /api/sdr/tune", deps.Access.RequireControl(sdrHandler.Tune))
	mux.HandleFunc("POST /api/sdr/receiver", deps.Access.RequireControl(sdrHandler.SetReceiver))
	mux.HandleFunc("POST /api/sdr/channel", deps.Access.RequireControl(sdrHandler.SetChannel))
	mux.HandleFunc("POST /api/sdr/demod", deps.Access.RequireControl(sdrHandler.SetDemod))
	mux.HandleFunc("GET /api/sdr/status", sdrHandler.GetStatus)
	mux.HandleFunc("POST /api/tracking/current", trackingHandler.SetCurrent)
	mux.HandleFunc("POST /api/tracking/reset", trackingHandler.ResetCurrent)
	mux.HandleFunc("POST /api/exclusions", exclusionsHandler.Add)
	mux.HandleFunc("GET /api/exclusions", exclusionsHandler.List)
	mux.HandleFunc("DELETE /api/exclusions/{norad}", exclusionsHandler.Delete)

	if deps.SatNOGS != nil {
		satnogsHandler := handlers.NewSatNOGSHandler(deps.SatNOGS)
		mux.HandleFunc("GET /api/satnogs/transmitters/{norad}", satnogsHandler.GetTransmitters)
	}

	if deps.Products != nil {
		productsHandler := handlers.NewProductsHandler(deps.Products)
		mux.HandleFunc("GET /api/products", productsHandler.List)
		mux.HandleFunc("GET /api/products/{id}", productsHandler.Image)
	}

	mux.Handle("GET /api/sse", deps.SSE)

	// Бинарные chunked-стримы (ADR-002)
	if deps.ReceiverHub != nil {
		streamHandler := handlers.NewStreamHandler(deps.ReceiverHub).WithAudio(deps.Audio)
		mux.HandleFunc("GET /api/stream/v1/spectrum", streamHandler.Spectrum)
		mux.HandleFunc("GET /api/stream/v1/audio", streamHandler.Audio)
	}
}

func redirectFound(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, path, http.StatusFound)
	}
}
