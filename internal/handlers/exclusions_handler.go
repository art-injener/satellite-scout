package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
)

// ExclusionAdder добавляет спутник в список исключений.
//
// Список и удаление вынесены в отдельные методы интерфейса, чтобы упростить
// мокирование в тестах: модалке настроек нужны Add/List/Remove, а основной
// flow «скрыть спутник через ПКМ» — только Add.
type ExclusionAdder interface {
	Add(norad int) error
	Remove(norad int) error
	List() []int
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

// ExclusionItem — запись в ответе GET /api/exclusions.
type ExclusionItem struct {
	NoradID int `json:"norad_id"`
}

// ExclusionsListResponse — ответ GET /api/exclusions.
type ExclusionsListResponse struct {
	Exclusions []ExclusionItem `json:"exclusions"`
}

// List обрабатывает GET /api/exclusions — возвращает текущий список исключённых
// NORAD ID. Используется модалкой настроек («Исключения»).
func (h *ExclusionsHandler) List(w http.ResponseWriter, r *http.Request) {
	ids := h.store.List()
	items := make([]ExclusionItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, ExclusionItem{NoradID: id})
	}
	writeJSON(w, http.StatusOK, ExclusionsListResponse{Exclusions: items})
}

// Delete обрабатывает DELETE /api/exclusions/{norad} — снимает исключение и
// форсирует пересчёт группы, чтобы спутник вернулся в выдачу немедленно.
func (h *ExclusionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	noradStr := r.PathValue("norad")
	noradID, err := strconv.Atoi(noradStr)
	if err != nil || noradID <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "norad_id must be positive integer"})
		return
	}

	if removeErr := h.store.Remove(noradID); removeErr != nil {
		slog.Error("failed to remove exclusion",
			slog.Int("norad_id", noradID), slog.String("error", removeErr.Error()))
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to remove exclusion"})
		return
	}

	if h.invalidator != nil {
		h.invalidator.InvalidateCache()
	}
	if h.refresher != nil {
		h.refresher.ForceGroupUpdate()
	}

	slog.Info("satellite exclusion removed", slog.Int("norad_id", noradID))
	w.WriteHeader(http.StatusNoContent)
}
