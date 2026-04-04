package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// PassServiceInterface — интерфейс сервиса пролётов.
type PassServiceInterface interface {
	GetPasses(group string, hours int, minEl float64) ([]*tracker.Pass, error)
	GetAllGroupsPasses(hours int, minEl float64) ([]*tracker.Pass, error)
}

// PassHandler обрабатывает запросы к API пролётов.
type PassHandler struct {
	passService PassServiceInterface
}

// NewPassHandler создаёт новый обработчик API пролётов.
func NewPassHandler(passService PassServiceInterface) *PassHandler {
	return &PassHandler{
		passService: passService,
	}
}

// GetPasses обрабатывает GET /api/passes.
// Query параметры:
//   - group: группа спутников (опционально). Если не указан — возвращает пролёты всех групп.
//   - hours: горизонт прогноза в часах (по умолчанию 24)
//   - min_el: минимальный угол места в градусах (по умолчанию 5)
//
// Возвращает JSON массив пролётов, отсортированных по AOS.
func (h *PassHandler) GetPasses(w http.ResponseWriter, r *http.Request) {
	// Парсинг параметров.
	group := r.URL.Query().Get("group")

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

	var passes []*tracker.Pass
	var err error

	// Если группа не указана — возвращаем пролёты всех загруженных спутников.
	if group == "" {
		passes, err = h.passService.GetAllGroupsPasses(hours, minEl)
	} else {
		passes, err = h.passService.GetPasses(group, hours, minEl)
	}

	if err != nil {
		slog.Error("failed to get passes",
			slog.String("group", group),
			slog.Int("hours", hours),
			slog.Float64("min_el", minEl),
			slog.Any("error", err),
		)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "failed to compute passes",
		})
		return
	}

	// Для ответа: если группа пустая, обозначаем как "all".
	responseGroup := group
	if responseGroup == "" {
		responseGroup = "all"
	}

	// Ответ.
	writeJSON(w, http.StatusOK, PassesResponse{
		Passes: passes,
		Count:  len(passes),
		Params: PassesParams{
			Group: responseGroup,
			Hours: hours,
			MinEl: minEl,
		},
	})
}
