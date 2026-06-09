package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

// SDRHandler — REST API обнаружения и проверки SDR-приёмников (вкладка «Радиотракты»).
type SDRHandler struct {
	svc *sdr.Service
}

// NewSDRHandler создаёт обработчик SDR API.
func NewSDRHandler(svc *sdr.Service) *SDRHandler {
	return &SDRHandler{svc: svc}
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
