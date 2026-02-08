package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// PassServiceInterface — интерфейс сервиса пролётов.
type PassServiceInterface interface {
	GetPasses(group string, hours int, minEl float64) ([]*tracker.Pass, error)
}

// TrackingServiceInterface — интерфейс сервиса отслеживания (для POST /api/tracking/current).
type TrackingServiceInterface interface {
	TrackSatellite(noradID int) error
}

// PassHandler обрабатывает запросы к API пролётов.
type PassHandler struct {
	passService     PassServiceInterface
	trackingService TrackingServiceInterface
}

// NewPassHandler создаёт новый обработчик API пролётов.
func NewPassHandler(passService PassServiceInterface, trackingService TrackingServiceInterface) *PassHandler {
	return &PassHandler{
		passService:     passService,
		trackingService: trackingService,
	}
}

// GetPasses обрабатывает GET /api/passes.
// Query параметры:
//   - group: группа спутников (по умолчанию "amateur")
//   - hours: горизонт прогноза в часах (по умолчанию 24)
//   - min_el: минимальный угол места в градусах (по умолчанию 5)
//
// Возвращает JSON массив пролётов, отсортированных по AOS.
func (h *PassHandler) GetPasses(w http.ResponseWriter, r *http.Request) {
	// Парсинг параметров.
	group := r.URL.Query().Get("group")
	if group == "" {
		group = "amateur"
	}

	hours := 24
	if hoursStr := r.URL.Query().Get("hours"); hoursStr != "" {
		if parsed, err := strconv.Atoi(hoursStr); err == nil && parsed > 0 && parsed <= 168 {
			hours = parsed
		}
	}

	minEl := 5.0
	if minElStr := r.URL.Query().Get("min_el"); minElStr != "" {
		if parsed, err := strconv.ParseFloat(minElStr, 64); err == nil && parsed >= 0 && parsed <= 90 {
			minEl = parsed
		}
	}

	// Получение пролётов.
	passes, err := h.passService.GetPasses(group, hours, minEl)
	if err != nil {
		slog.Error("failed to get passes",
			"group", group,
			"hours", hours,
			"min_el", minEl,
			"error", err,
		)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "failed to compute passes",
		})
		return
	}

	// Ответ.
	writeJSON(w, http.StatusOK, PassesResponse{
		Passes: passes,
		Count:  len(passes),
		Params: PassesParams{
			Group: group,
			Hours: hours,
			MinEl: minEl,
		},
	})
}

// trackingRequest — тело запроса POST /api/tracking/current.
type trackingRequest struct {
	NoradID int `json:"norad_id"`
}

// SetTrackingCurrent обрабатывает POST /api/tracking/current.
// Устанавливает спутник для отслеживания (добавляет в SatelliteTrackingService).
func (h *PassHandler) SetTrackingCurrent(w http.ResponseWriter, r *http.Request) {
	// Парсинг body.
	var req trackingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.NoradID <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "norad_id is required and must be positive",
		})
		return
	}

	// Добавление в отслеживание.
	if err := h.trackingService.TrackSatellite(req.NoradID); err != nil {
		slog.Error("failed to track satellite",
			"norad_id", req.NoradID,
			"error", err,
		)
		// Определяем тип ошибки для корректного статуса.
		status := http.StatusInternalServerError
		msg := "failed to track satellite"
		// Если спутник не найден — 404.
		if err.Error() == "satellite not found in TLE store: "+strconv.Itoa(req.NoradID) {
			status = http.StatusNotFound
			msg = "satellite not found"
		}
		writeJSON(w, status, ErrorResponse{Error: msg})
		return
	}

	slog.Info("satellite tracking started via API", "norad_id", req.NoradID)

	writeJSON(w, http.StatusOK, TrackingResponse{
		Status:  "tracking",
		NoradID: req.NoradID,
	})
}
