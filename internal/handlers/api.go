package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/art-injener/satwatch-go/internal/config"
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
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

// GetConfig возвращает текущую конфигурацию.
func (h *APIHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"observer": map[string]float64{
			"lat": h.config.ObserverLat,
			"lon": h.config.ObserverLon,
			"alt": h.config.ObserverAlt,
		},
	})
}
