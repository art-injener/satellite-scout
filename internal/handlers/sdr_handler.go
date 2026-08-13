package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

// SDRHandler — REST API управления SDR-приёмниками.
type SDRHandler struct {
	svc *sdr.Service
	hub *sdr.ReceiverHub
}

// NewSDRHandler создаёт обработчик SDR API.
// hub может быть nil — тогда Tune/Status недоступны (station_type=basic).
func NewSDRHandler(svc *sdr.Service, hub *sdr.ReceiverHub) *SDRHandler {
	return &SDRHandler{svc: svc, hub: hub}
}

// ListDevices возвращает список обнаруженных приёмников.
func (h *SDRHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.ListDevices())
}

// Test выполняет проверку выбранного приёмника.
func (h *SDRHandler) Test(w http.ResponseWriter, r *http.Request) {
	var req sdr.TestRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if req.Driver == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "driver is required"})
		return
	}
	writeJSON(w, http.StatusOK, h.svc.Test(req))
}

// TuneRequest — тело POST /api/sdr/tune.
type TuneRequest struct {
	RadioPath string   `json:"radio_path"`
	CenterHz  uint64   `json:"center_hz"`
	GainDB    *float32 `json:"gain_db,omitempty"`
}

// TuneResponse — ответ POST /api/sdr/tune.
type TuneResponse struct {
	OK     bool               `json:"ok"`
	Status sdr.ReceiverStatus `json:"status"`
}

// Tune перестраивает приёмник на новую частоту (и опционально меняет усиление).
// POST /api/sdr/tune
func (h *SDRHandler) Tune(w http.ResponseWriter, r *http.Request) {
	if h.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			ErrorResponse{Error: "no receiver hub configured"})
		return
	}

	var req TuneRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if req.RadioPath == "" {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "radio_path is required"})
		return
	}
	if req.CenterHz == 0 {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "center_hz is required"})
		return
	}

	receiver, err := h.hub.Get(req.RadioPath)
	if err != nil {
		slog.Error("tune: receiver not found",
			"radio_path", req.RadioPath, "error", err)
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}

	if err := receiver.Tune(req.CenterHz); err != nil {
		slog.Error("tune: failed",
			"radio_path", req.RadioPath, "center_hz", req.CenterHz, "error", err)
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "tune failed: " + err.Error()})
		return
	}

	if req.GainDB != nil {
		if err := receiver.SetGain(*req.GainDB); err != nil {
			slog.Error("tune: set gain failed",
				"radio_path", req.RadioPath, "gain_db", *req.GainDB, "error", err)
			writeJSON(w, http.StatusBadRequest,
				ErrorResponse{Error: "set gain failed: " + err.Error()})
			return
		}
	}

	slog.Info("receiver tuned",
		"radio_path", req.RadioPath,
		"center_hz", req.CenterHz)

	writeJSON(w, http.StatusOK, TuneResponse{
		OK:     true,
		Status: receiver.Status(),
	})
}

// GetStatus возвращает текущее состояние приёмника.
// GET /api/sdr/status?radio_path=…
func (h *SDRHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if h.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			ErrorResponse{Error: "no receiver hub configured"})
		return
	}

	radioPath := r.URL.Query().Get("radio_path")
	if radioPath == "" {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "radio_path is required"})
		return
	}

	receiver, err := h.hub.Get(radioPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, receiver.Status())
}
