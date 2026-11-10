package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"unsafe"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

// IQ обрабатывает GET /api/stream/v1/iq: отсчёты тракта кадрами SSTM IQ_C64
// (cf32, как их видит обнаружитель). Демодулятор подписан на то же кольцо отдельно
// и от этой записи не зависит.
func (h *StreamHandler) IQ(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	radioPath := r.URL.Query().Get(queryRadioPath)
	if radioPath == "" {
		http.Error(w, "radio_path is required", http.StatusBadRequest)
		return
	}

	// Сеанс устройства живёт дольше запроса, поэтому контекст запроса в него не передаётся.
	iqCh, unsubscribe, err := h.hub.Subscribe(radioPath) //nolint:contextcheck // см. комментарий выше
	if err != nil {
		slog.ErrorContext(r.Context(), "iq: failed to subscribe",
			slog.String("radio_path", radioPath), slog.Any("error", err))
		if errors.Is(err, sdr.ErrUnknownRadioPath) {
			http.Error(w, "receiver not available: "+err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "stream start failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer unsubscribe()

	w.Header().Set("Content-Type", headerContentType)
	w.Header().Set("Cache-Control", headerCacheControl)
	w.Header().Set("X-Accel-Buffering", headerNginxBuffering)
	slog.Info("iq stream started", slog.String("radio_path", radioPath))

	serveIQLoop(r.Context(), w, flusher, iqCh)
}

// serveIQLoop пишет каждый блок IQ клиенту, пока он подключён.
func serveIQLoop(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, iqCh <-chan sdr.IQFrame) {
	hdr := make([]byte, dsp.HeaderSize)
	var seq uint32
	for {
		select {
		case <-ctx.Done():
			return
		case f, open := <-iqCh:
			if !open {
				return
			}
			payload := complex64Bytes(f.Samples)
			dsp.EncodeHeader(hdr, dsp.FrameHeader{
				DataType:    dsp.DataTypeIQC64,
				TimestampMs: int64(f.TimestampMs),
				CenterHz:    int64(f.CenterHz),
				SpanHz:      int64(f.SampleRate),
				PayloadLen:  uint32(len(payload)),
				Seq:         seq,
			})
			if _, err := w.Write(hdr); err != nil {
				return
			}
			if len(payload) > 0 {
				if _, err := w.Write(payload); err != nil {
					return
				}
			}
			flusher.Flush()
			seq++
		}
	}
}

// complex64Bytes — байты cf32_le без копии. Действительны, пока жив срез.
func complex64Bytes(s []complex64) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*8)
}
