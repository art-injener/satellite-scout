package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/config"
)

const (
	contentTypeJSON = "application/json"
)

// APIHandler обрабатывает REST API запросы.
type APIHandler struct {
	config *config.Config
}

// NewAPIHandler создаёт новый API обработчик.
func NewAPIHandler(cfg *config.Config) *APIHandler {
	return &APIHandler{
		config: cfg,
	}
}

// writeJSON записывает JSON ответ.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

// HealthCheck возвращает статус работоспособности сервера.
func (h *APIHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// GetConfig возвращает текущую конфигурацию.
func (h *APIHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ConfigResponse{
		Observer: ObserverConfig{
			Lat: h.config.ObserverLat,
			Lon: h.config.ObserverLon,
			Alt: h.config.ObserverAlt,
		},
	})
}
