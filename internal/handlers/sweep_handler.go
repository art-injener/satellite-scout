package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/sweep"
)

// SweepHandler — REST обхода тракта по диапазонам из конфига.
type SweepHandler struct {
	svc *sweep.Service
}

// NewSweepHandler создаёт обработчик; svc может быть nil — тогда обход недоступен.
func NewSweepHandler(svc *sweep.Service) *SweepHandler {
	return &SweepHandler{svc: svc}
}

// SweepRequest — тело POST /api/sdr/sweep: запустить или остановить обход.
type SweepRequest struct {
	RadioPath string `json:"radio_path"`
	Running   bool   `json:"running"`
}

// Get возвращает состояние обхода тракта; без обхода — окна при текущей полосе.
// GET /api/sdr/sweep?radio_path=…
func (h *SweepHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "no receiver hub configured"})
		return
	}
	pathID := r.URL.Query().Get("radio_path")
	if pathID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "radio_path is required"})
		return
	}
	h.writeState(w, pathID)
}

// Set запускает или останавливает обход и отвечает его состоянием.
// POST /api/sdr/sweep
func (h *SweepHandler) Set(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "no receiver hub configured"})
		return
	}
	var req SweepRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if req.RadioPath == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "radio_path is required"})
		return
	}
	if !req.Running {
		h.svc.Stop(req.RadioPath)
		h.writeState(w, req.RadioPath)
		return
	}
	if err := h.svc.Start(req.RadioPath); err != nil {
		slog.Error("sweep: start failed", slog.String("radio_path", req.RadioPath), slog.Any("error", err))
		writeSweepError(w, err)
		return
	}
	h.writeState(w, req.RadioPath)
}

func (h *SweepHandler) writeState(w http.ResponseWriter, pathID string) {
	st, err := h.svc.State(pathID)
	if err != nil {
		writeSweepError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func writeSweepError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, sweep.ErrNotConfigured) {
		code = http.StatusNotFound
	}
	writeJSON(w, code, ErrorResponse{Error: err.Error()})
}
