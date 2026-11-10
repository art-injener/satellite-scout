package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sweep"
)

// WithSweep подключает обход: без него поток панорамы недоступен.
func (h *StreamHandler) WithSweep(svc *sweep.Service) *StreamHandler {
	h.sweep = svc
	return h
}

// Panorama обрабатывает GET /api/stream/v1/panorama.
// Каждое окно обхода — обычный кадр FFT_DB с его центром и шириной в заголовке и временем снимка.
// Сначала уходят сохранённые снимки всех окон, затем новые снимки по мере обхода.
func (h *StreamHandler) Panorama(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	if h.sweep == nil {
		http.Error(w, "sweep not available", http.StatusServiceUnavailable)
		return
	}
	radioPath := r.URL.Query().Get(queryRadioPath)
	if radioPath == "" {
		http.Error(w, "radio_path is required", http.StatusBadRequest)
		return
	}
	stored, updates, cancel, err := h.sweep.Watch(radioPath)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, sweep.ErrNotConfigured) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", headerContentType)
	w.Header().Set("Cache-Control", headerCacheControl)
	w.Header().Set("X-Accel-Buffering", headerNginxBuffering)
	slog.Info("panorama stream started", slog.String("radio_path", radioPath), slog.Int("stored", len(stored)))

	pw := &panoramaWriter{w: w, flusher: flusher, hdr: make([]byte, dsp.HeaderSize)}
	for _, sn := range stored {
		if !pw.write(sn) {
			return
		}
	}
	if len(stored) == 0 && !pw.heartbeat() {
		return
	}
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case sn := <-updates:
			if !pw.write(sn) {
				return
			}
		case <-heartbeat.C:
			if time.Since(pw.lastWrite) >= heartbeatInterval && !pw.heartbeat() {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

type panoramaWriter struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	hdr       []byte
	seq       uint32
	lastWrite time.Time
}

func (pw *panoramaWriter) write(sn sweep.Snapshot) bool {
	payload := dsp.Float32ToBytes(sn.DBFS)
	dsp.EncodeHeader(pw.hdr, dsp.FrameHeader{
		DataType:    dsp.DataTypeFFTDB,
		DisplayMode: dsp.ModeAvg,
		TimestampMs: sn.TimeMs,
		CenterHz:    int64(sn.CenterHz),
		SpanHz:      int64(sn.SpanHz),
		PayloadLen:  uint32(len(payload)),
		Seq:         pw.seq,
	})
	return pw.send(pw.hdr, payload)
}

func (pw *panoramaWriter) heartbeat() bool {
	dsp.EncodeHeartbeat(pw.hdr, pw.seq, 0, 0)
	return pw.send(pw.hdr, nil)
}

func (pw *panoramaWriter) send(hdr, payload []byte) bool {
	if _, err := pw.w.Write(hdr); err != nil {
		return false
	}
	if len(payload) > 0 {
		if _, err := pw.w.Write(payload); err != nil {
			return false
		}
	}
	pw.flusher.Flush()
	pw.seq++
	pw.lastWrite = time.Now()
	return true
}
