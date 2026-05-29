package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ExclusionAdder добавляет спутник в список исключений.
type ExclusionAdder interface {
	Add(norad int) error
}

// PassCacheInvalidator сбрасывает кеш пролётов после изменения списка исключений,
// чтобы следующее обновление группы пересчиталось без исключённого спутника.
type PassCacheInvalidator interface {
	InvalidateCache()
}

// GroupRefresher форсирует немедленный пересчёт и рассылку группы,
// чтобы скрытый спутник пропадал сразу, а не на следующем тике.
type GroupRefresher interface {
	ForceGroupUpdate()
}

// ExclusionsHandler обрабатывает запросы управления списком исключённых спутников.
type ExclusionsHandler struct {
	store       ExclusionAdder
	invalidator PassCacheInvalidator
	refresher   GroupRefresher
}

// NewExclusionsHandler создаёт обработчик списка исключений.
func NewExclusionsHandler(
	store ExclusionAdder,
	invalidator PassCacheInvalidator,
	refresher GroupRefresher,
) *ExclusionsHandler {
	return &ExclusionsHandler{store: store, invalidator: invalidator, refresher: refresher}
}

// ExclusionRequest — тело запроса POST /api/exclusions.
type ExclusionRequest struct {
	NoradID int `json:"norad_id"`
}

// Add обрабатывает POST /api/exclusions — добавление спутника в исключения.
//
// Тело запроса: {"norad_id": 25544}
// Ответ 200: {"status": "ok", "norad_id": 25544}
// Ответ 400: {"error": "..."}
func (h *ExclusionsHandler) Add(w http.ResponseWriter, r *http.Request) {
	var req ExclusionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.NoradID <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "norad_id must be positive"})
		return
	}

	if err := h.store.Add(req.NoradID); err != nil {
		slog.Error("failed to add exclusion",
			slog.Int("norad_id", req.NoradID), slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to persist exclusion"})
		return
	}

	// Сбрасываем кеш пролётов, затем форсируем пересчёт группы —
	// исключённый спутник пропадает из группы и таблицы сразу.
	if h.invalidator != nil {
		h.invalidator.InvalidateCache()
	}
	if h.refresher != nil {
		h.refresher.ForceGroupUpdate()
	}

	slog.Info("satellite excluded", slog.Int("norad_id", req.NoradID))
	writeJSON(w, http.StatusOK, TrackingResponse{Status: "ok", NoradID: req.NoradID})
}
