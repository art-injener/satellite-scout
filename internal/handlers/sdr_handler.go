package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

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

// StatusResponse — состояние приёмника тракта вместе с настройками демодулятора.
// sample_rate — частота тракта из хаба (известна и без потока), sample_rates — варианты «Полосы».
type StatusResponse struct {
	sdr.ReceiverStatus
	SampleRates []uint32            `json:"sample_rates"`
	Channel     sdr.ChannelSettings `json:"channel"`
}

// TuneResponse — ответ POST /api/sdr/tune и POST /api/sdr/receiver.
type TuneResponse struct {
	OK     bool           `json:"ok"`
	Status StatusResponse `json:"status"`
}

// DemodRequest — тело POST /api/sdr/demod: только выключатель, без остальных настроек.
type DemodRequest struct {
	RadioPath string `json:"radio_path"`
	DemodOff  bool   `json:"demod_off"`
}

// ReceiverRequest — тело POST /api/sdr/receiver: только приёмник, кнопка секции «Приёмник».
// Отрицательное усиление означает АРУ; частота дискретизации — одна из sample_rates статуса.
type ReceiverRequest struct {
	RadioPath    string  `json:"radio_path"`
	CenterHz     uint64  `json:"center_hz"`
	SampleRateHz uint32  `json:"sample_rate_hz"`
	GainDB       float32 `json:"gain_db"`
}

// ChannelRequest — тело POST /api/sdr/channel: только демодулятор, кнопка секции «Демодулятор».
type ChannelRequest struct {
	RadioPath string `json:"radio_path"`
	sdr.ChannelSettings
}

// status собирает состояние приёмника, частоту тракта и настройки демодулятора.
func (h *SDRHandler) status(pathID string, receiver sdr.Receiver) (StatusResponse, error) {
	ch, err := h.hub.Channel(pathID)
	if err != nil {
		return StatusResponse{}, err
	}
	rate, err := h.hub.SampleRate(pathID)
	if err != nil {
		return StatusResponse{}, err
	}
	rates, err := h.hub.SampleRateChoices(pathID)
	if err != nil {
		return StatusResponse{}, err
	}
	st := receiver.Status()
	st.SampleRate = rate
	return StatusResponse{ReceiverStatus: st, SampleRates: rates, Channel: ch}, nil
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

	h.writeStatus(w, req.RadioPath, receiver, true)
}

// SetReceiver применяет частоту, полосу и усиление тракта и не трогает демодулятор.
// Сначала проверяется всё, затем меняется: отклонённый запрос ничего не трогает.
// Узкая полоса, в которую текущий канал не влезает, тоже отклоняется.
// POST /api/sdr/receiver
func (h *SDRHandler) SetReceiver(w http.ResponseWriter, r *http.Request) {
	if h.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			ErrorResponse{Error: "no receiver hub configured"})
		return
	}

	var req ReceiverRequest
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
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}
	rates, err := h.hub.SampleRateChoices(req.RadioPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}
	if !slices.Contains(rates, req.SampleRateHz) {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: fmt.Sprintf("sample_rate_hz %d not in %v", req.SampleRateHz, rates)})
		return
	}
	channel, err := h.hub.Channel(req.RadioPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}
	if err := channel.Validate(req.SampleRateHz); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if err := receiver.Tune(req.CenterHz); err != nil {
		slog.Error("receiver settings: tune failed",
			slog.String("radio_path", req.RadioPath), slog.Any("error", err))
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "tune failed: " + err.Error()})
		return
	}
	if err := receiver.SetGain(req.GainDB); err != nil {
		slog.Error("receiver settings: set gain failed",
			slog.String("radio_path", req.RadioPath), slog.Any("error", err))
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "set gain failed: " + err.Error()})
		return
	}
	if err := h.hub.SetSampleRate(req.RadioPath, req.SampleRateHz); err != nil {
		slog.Error("receiver settings: sample rate change failed",
			slog.String("radio_path", req.RadioPath), slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError,
			ErrorResponse{Error: "sample rate change failed: " + err.Error()})
		return
	}

	slog.Info("receiver settings applied",
		slog.String("radio_path", req.RadioPath),
		slog.Uint64("center_hz", req.CenterHz),
		slog.Int("sample_rate_hz", int(req.SampleRateHz)),
		slog.Float64("gain_db", float64(req.GainDB)))

	h.writeStatus(w, req.RadioPath, receiver, true)
}

// SetChannel применяет профиль, сдвиг, полосу канала и порог и не трогает приёмник.
// Проверка идёт по уже установленной полосе тракта. Отклонённый запрос ничего не меняет.
// POST /api/sdr/channel
func (h *SDRHandler) SetChannel(w http.ResponseWriter, r *http.Request) {
	if h.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			ErrorResponse{Error: "no receiver hub configured"})
		return
	}

	var req ChannelRequest
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

	receiver, err := h.hub.Get(req.RadioPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}
	rate, err := h.hub.SampleRate(req.RadioPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}
	if err := req.ChannelSettings.Validate(rate); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if err := h.hub.SetChannel(req.RadioPath, req.ChannelSettings); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	slog.Info("demodulator settings applied",
		slog.String("radio_path", req.RadioPath),
		slog.Int64("offset_hz", req.OffsetHz),
		slog.Int("bandwidth_hz", int(req.BandwidthHz)),
		slog.Float64("squelch_dbfs", req.SquelchDBFS),
		slog.String("demod", req.Demod))

	h.writeStatus(w, req.RadioPath, receiver, true)
}

// SetDemod включает или останавливает демодулятор тракта. Спектр и водопад не трогает.
// POST /api/sdr/demod
func (h *SDRHandler) SetDemod(w http.ResponseWriter, r *http.Request) {
	if h.hub == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			ErrorResponse{Error: "no receiver hub configured"})
		return
	}

	var req DemodRequest
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
	if _, err := h.hub.Get(req.RadioPath); err != nil {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}
	if err := h.hub.SetDemodOff(req.RadioPath, req.DemodOff); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	receiver, err := h.hub.Get(req.RadioPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}
	slog.Info("demodulator switch",
		slog.String("radio_path", req.RadioPath),
		slog.Bool("off", req.DemodOff))
	h.writeStatus(w, req.RadioPath, receiver, true)
}

// writeStatus отвечает состоянием тракта: в обёртке TuneResponse или как есть.
func (h *SDRHandler) writeStatus(w http.ResponseWriter, pathID string, receiver sdr.Receiver, wrapped bool) {
	st, err := h.status(pathID, receiver)
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			ErrorResponse{Error: "receiver not available: " + err.Error()})
		return
	}
	if wrapped {
		writeJSON(w, http.StatusOK, TuneResponse{OK: true, Status: st})
		return
	}
	writeJSON(w, http.StatusOK, st)
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

	h.writeStatus(w, radioPath, receiver, false)
}
