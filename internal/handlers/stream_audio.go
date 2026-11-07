package handlers

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/art-injener/satellite-scout/internal/demod"
	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

// audioChunkSamples — сколько звука собирается в один кадр: 100 мс, задержка
// незаметна, а кадров в секунду десять, а не по одному на блок IQ.
const audioChunkSamples = demod.AudioRate / 10

// WithAudio подключает раздачу звука демодулятора для потока /audio.
func (h *StreamHandler) WithAudio(audio *demod.AudioHub) *StreamHandler {
	h.audio = audio
	return h
}

// Audio обрабатывает GET /api/stream/v1/audio: звук канала тракта кадрами SSTM PCM_I16.
// В заголовке center_hz — частота канала, span_hz — частота отсчётов звука.
// Слушатель держит поток устройства, как зритель спектра: звук идёт и без спектра.
func (h *StreamHandler) Audio(w http.ResponseWriter, r *http.Request) {
	if h.audio == nil {
		http.Error(w, "audio is not available", http.StatusServiceUnavailable)
		return
	}
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
		slog.ErrorContext(r.Context(), "audio: failed to subscribe",
			slog.String("radio_path", radioPath), slog.Any(slogKeyError, err))
		if errors.Is(err, sdr.ErrUnknownRadioPath) {
			http.Error(w, "receiver not available: "+err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "stream start failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer unsubscribe()
	blocks, stopListening := h.audio.Subscribe(radioPath)
	defer stopListening()

	w.Header().Set("Content-Type", headerContentType)
	w.Header().Set("Cache-Control", headerCacheControl)
	w.Header().Set("X-Accel-Buffering", headerNginxBuffering)
	slog.InfoContext(r.Context(), "audio stream started", slog.String("radio_path", radioPath))

	serveAudioLoop(r.Context(), &audioWriter{w: w, flusher: flusher, hdr: make([]byte, dsp.HeaderSize)}, iqCh, blocks)
}

// serveAudioLoop пишет звук клиенту, пока он подключён и поток устройства идёт.
// Кадры IQ не нужны: подписка на них только держит поток устройства.
func serveAudioLoop(ctx context.Context, aw *audioWriter, iqCh <-chan sdr.IQFrame, blocks <-chan demod.AudioBlock) {
	idle := time.NewTicker(time.Second)
	defer idle.Stop()
	for {
		select {
		case _, open := <-iqCh:
			if !open {
				return
			}
		case b := <-blocks:
			if !aw.add(b) {
				return
			}
		case <-idle.C:
			if time.Since(aw.lastWrite) < heartbeatInterval {
				continue
			}
			dsp.EncodeHeartbeat(aw.hdr, aw.seq, int64(aw.centerHz), demod.AudioRate)
			if !aw.write(aw.hdr) {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// audioWriter собирает блоки звука в кадры по audioChunkSamples и пишет их в поток.
type audioWriter struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	hdr       []byte
	payload   []byte
	startMs   uint64 // начало первого блока в кадре
	centerHz  uint64
	seq       uint32
	lastWrite time.Time
}

// add дописывает блок; полный кадр уходит клиенту. false — клиент отключился.
func (aw *audioWriter) add(b demod.AudioBlock) bool {
	if len(aw.payload) == 0 {
		aw.startMs = b.TimestampMs
	}
	aw.centerHz = b.FrequencyHz
	for _, s := range b.Samples {
		aw.payload = binary.LittleEndian.AppendUint16(aw.payload, uint16(s))
	}
	if len(aw.payload) < 2*audioChunkSamples {
		return true
	}
	dsp.EncodeHeader(aw.hdr, dsp.FrameHeader{
		DataType:    dsp.DataTypePCMI16,
		TimestampMs: int64(aw.startMs),
		CenterHz:    int64(aw.centerHz),
		SpanHz:      demod.AudioRate,
		PayloadLen:  uint32(len(aw.payload)),
		Seq:         aw.seq,
	})
	ok := aw.write(aw.hdr, aw.payload)
	aw.payload = aw.payload[:0]
	return ok
}

func (aw *audioWriter) write(parts ...[]byte) bool {
	for _, p := range parts {
		if _, err := aw.w.Write(p); err != nil {
			return false
		}
	}
	aw.flusher.Flush()
	aw.seq++
	aw.lastWrite = time.Now()
	return true
}
