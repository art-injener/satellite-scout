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

// GetConfig возвращает текущую конфигурацию (только публичная часть, без
// секретов и серверных деталей). Содержит координаты наблюдателя, вычисленный
// тип станции и компактный список радиотрактов — этого достаточно, чтобы
// фронтенд построил mode-bar и переключатель режимов. Полная схема для модалки
// настроек отдаётся через GET /api/settings.
func (h *APIHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	station := &h.config.Station

	paths := make([]RadioPathInfo, 0, len(station.RadioPaths))
	for _, rp := range station.RadioPaths {
		paths = append(paths, RadioPathInfo{
			ID:         rp.ID,
			Name:       rp.Name,
			HasRotator: rp.Rotator != nil,
		})
	}

	writeJSON(w, http.StatusOK, ConfigResponse{
		Observer: ObserverConfig{
			Lat: station.Observer.Lat,
			Lon: station.Observer.Lon,
			Alt: station.Observer.AltM,
		},
		StationType: station.StationType(),
		RadioPaths:  paths,
	})
}
