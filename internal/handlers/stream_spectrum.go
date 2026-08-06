package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	defaultSpectrumBins = 1024
	defaultSpectrumFPS  = 20
	heartbeatInterval   = 15 * time.Second
	minFPS              = 10
	maxFPS              = 30
	minBins             = 64
	maxBins             = 8192

	queryRadioPath = "radio_path"
	queryBins      = "bins"
	queryFPS       = "fps"
	queryMode      = "mode"

	headerContentType    = "application/octet-stream"
	headerCacheControl   = "no-cache"
	headerNginxBuffering = "no"
)

// StreamHandler обслуживает бинарные chunked-стримы (Спектр, Панорама и т.д.).
type StreamHandler struct {
	hub *sdr.ReceiverHub
}

// NewStreamHandler создаёт обработчик бинарных потоков.
func NewStreamHandler(hub *sdr.ReceiverHub) *StreamHandler {
	return &StreamHandler{hub: hub}
}

// Spectrum обрабатывает GET /api/stream/v1/spectrum.
// Поток бинарных SSTM-фреймов (ADR-002): IQ → окно → FFT → reducer → dBFS → клиент.
func (h *StreamHandler) Spectrum(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()

	radioPath := q.Get(queryRadioPath)
	if radioPath == "" {
		http.Error(w, "radio_path is required", http.StatusBadRequest)
		return
	}

	bins := parseIntClamped(q.Get(queryBins), defaultSpectrumBins, minBins, maxBins)
	fps := parseIntClamped(q.Get(queryFPS), defaultSpectrumFPS, minFPS, maxFPS)
	mode := parseDisplayMode(q.Get(queryMode))

	receiver, err := h.hub.Get(radioPath)
	if err != nil {
		slog.Error("spectrum: receiver not found",
			"radio_path", radioPath, "error", err)
		http.Error(w, "receiver not available: "+err.Error(), http.StatusBadRequest)
		return
	}

	fftSize := nextPow2(bins) // FFT — степень двойки >= bins
	streamCfg := sdr.StreamConfig{
		BlockSize: fftSize,
	}
	bins = fftSize // клиент получает ровно столько бинов, сколько FFT
	iqCh, err := receiver.StartStream(r.Context(), streamCfg)
	if err != nil {
		slog.Error("spectrum: failed to start stream",
			"radio_path", radioPath, "error", err)
		http.Error(w, "stream start failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", headerContentType)
	w.Header().Set("Cache-Control", headerCacheControl)
	w.Header().Set("X-Accel-Buffering", headerNginxBuffering)

	slog.Info("spectrum stream started",
		"radio_path", radioPath,
		"fft_size", fftSize,
		"bins", bins,
		"fps", fps,
		"mode", mode,
	)

	serveSpectrumLoop(r.Context(), w, flusher, iqCh, spectrumParams{
		fftSize: fftSize,
		bins:    bins,
		fps:     fps,
		mode:    mode,
	})
}

type spectrumParams struct {
	fftSize int
	bins    int
	fps     int
	mode    dsp.DisplayMode
}

// serveSpectrumLoop — основной цикл: IQ → DSP → SSTM → HTTP.
func serveSpectrumLoop(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	iqCh <-chan sdr.IQFrame,
	p spectrumParams,
) {
	pipeline := dsp.NewSpectrumPipeline(p.fftSize, p.bins, p.mode, dsp.WindowBlackmanHarris)

	frameDur := time.Second / time.Duration(p.fps)
	ticker := time.NewTicker(frameDur)
	defer ticker.Stop()

	sw := &spectrumWriter{
		w:       w,
		flusher: flusher,
		hdr:     make([]byte, dsp.HeaderSize),
		mode:    p.mode,
	}

	for {
		select {
		case frame, ok := <-iqCh:
			if !ok {
				sw.flushPipeline(pipeline)
				return
			}
			sw.lastCenter = int64(frame.CenterHz)
			sw.lastSpan = int64(frame.SampleRate)
			pipeline.Push(frame.Samples)

		case <-ticker.C:
			if !sw.flushPipeline(pipeline) {
				if time.Since(sw.lastWrite) >= heartbeatInterval {
					if !sw.writeHeartbeat() {
						return
					}
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

// spectrumWriter инкапсулирует запись SSTM-фреймов в HTTP-поток.
type spectrumWriter struct {
	w          http.ResponseWriter
	flusher    http.Flusher
	hdr        []byte
	mode       dsp.DisplayMode
	seq        uint32
	lastCenter int64
	lastSpan   int64
	lastWrite  time.Time
}

// flushPipeline сбрасывает pipeline в HTTP. Возвращает true, если записан фрейм.
func (sw *spectrumWriter) flushPipeline(p *dsp.SpectrumPipeline) bool {
	dbfs := p.Flush()
	if dbfs == nil {
		return false
	}

	payload := dsp.Float32ToBytes(dbfs)
	dsp.EncodeHeader(sw.hdr, dsp.FrameHeader{
		DataType:    dsp.DataTypeFFTDB,
		DisplayMode: sw.mode,
		TimestampMs: time.Now().UnixMilli(),
		CenterHz:    sw.lastCenter,
		SpanHz:      sw.lastSpan,
		PayloadLen:  uint32(len(payload)),
		Seq:         sw.seq,
	})

	if _, err := sw.w.Write(sw.hdr); err != nil {
		return false
	}
	if _, err := sw.w.Write(payload); err != nil {
		return false
	}
	sw.flusher.Flush()
	sw.seq++
	sw.lastWrite = time.Now()
	return true
}

func (sw *spectrumWriter) writeHeartbeat() bool {
	dsp.EncodeHeartbeat(sw.hdr, sw.seq, sw.lastCenter, sw.lastSpan)
	if _, err := sw.w.Write(sw.hdr); err != nil {
		return false
	}
	sw.flusher.Flush()
	sw.seq++
	sw.lastWrite = time.Now()
	return true
}

// --- вспомогательные парсеры ---

func parseIntClamped(s string, def, lo, hi int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func parseDisplayMode(s string) dsp.DisplayMode {
	switch s {
	case "current":
		return dsp.ModeCurrent
	case "avg", "":
		return dsp.ModeAvg
	case "maxhold":
		return dsp.ModeMaxHold
	case "minhold":
		return dsp.ModeMinHold
	case "exp_avg":
		return dsp.ModeExpAvg
	default:
		return dsp.ModeAvg
	}
}

// nextPow2 возвращает ближайшую степень двойки >= n.
func nextPow2(n int) int {
	if n <= 0 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}
