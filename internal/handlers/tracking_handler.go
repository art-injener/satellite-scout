package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// TrackingServiceInterface — интерфейс сервиса отслеживания для HTTP-обработчика.
type TrackingServiceInterface interface {
	// SetManualSelection устанавливает ручной выбор primary спутника.
	SetManualSelection(noradID int)
	// ResetManualSelection сбрасывает ручной выбор (возврат к авто-режиму).
	ResetManualSelection()
}

// TrackingRequest — тело запроса POST /api/tracking/current.
type TrackingRequest struct {
	NoradID int `json:"norad_id"`
}

// TrackingHandler обрабатывает запросы управления сопровождением спутника.
type TrackingHandler struct {
	trackingService TrackingServiceInterface
}

// NewTrackingHandler создаёт обработчик управления сопровождением.
func NewTrackingHandler(svc TrackingServiceInterface) *TrackingHandler {
	return &TrackingHandler{trackingService: svc}
}

// SetCurrent обрабатывает POST /api/tracking/current — ручной выбор primary КА.
//
// Тело запроса: {"norad_id": 25544}
// Ответ 200: {"status": "ok", "norad_id": 25544}
// Ответ 400: {"error": "..."}
func (h *TrackingHandler) SetCurrent(w http.ResponseWriter, r *http.Request) {
	var req TrackingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.NoradID <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "norad_id must be positive"})
		return
	}

	h.trackingService.SetManualSelection(req.NoradID)

	slog.Info("manual tracking set", "norad_id", req.NoradID)
	writeJSON(w, http.StatusOK, TrackingResponse{
		Status:  "ok",
		NoradID: req.NoradID,
	})
}

// ResetCurrent обрабатывает POST /api/tracking/reset — сброс в авто-режим.
func (h *TrackingHandler) ResetCurrent(w http.ResponseWriter, r *http.Request) {
	h.trackingService.ResetManualSelection()

	slog.Info("tracking reset to auto")
	writeJSON(w, http.StatusOK, TrackingResponse{
		Status:  "ok",
		NoradID: 0,
	})
}
