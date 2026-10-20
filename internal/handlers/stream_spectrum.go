package handlers

import (
	"context"
	"errors"
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
	maxAvgFrames        = 100 // окно среднего, кадров UI: 5 с при 20 fps

	queryRadioPath = "radio_path"
	queryBins      = "bins"
	queryFPS       = "fps"
	queryMode      = "mode"
	queryWindowMs  = "window_ms"

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
	frames := avgFrames(q.Get(queryWindowMs), fps)

	// Поток устройства общий для всех зрителей тракта; FFT и накопление у каждого клиента свои.
	// Сеанс устройства живёт дольше запроса, поэтому контекст запроса в него не передаётся.
	iqCh, unsubscribe, err := h.hub.Subscribe(radioPath) //nolint:contextcheck // см. комментарий выше
	if err != nil {
		slog.ErrorContext(r.Context(), "spectrum: failed to subscribe",
			"radio_path", radioPath, "error", err)
		if errors.Is(err, sdr.ErrUnknownRadioPath) {
			http.Error(w, "receiver not available: "+err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "stream start failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer unsubscribe()

	fftSize := nextPow2(bins) // FFT — степень двойки >= bins
	bins = fftSize            // клиент получает ровно столько бинов, сколько FFT

	w.Header().Set("Content-Type", headerContentType)
	w.Header().Set("Cache-Control", headerCacheControl)
	w.Header().Set("X-Accel-Buffering", headerNginxBuffering)

	slog.Info("spectrum stream started",
		"radio_path", radioPath,
		"fft_size", fftSize,
		"bins", bins,
		"fps", fps,
		"mode", mode,
		"avg_frames", frames,
	)

	serveSpectrumLoop(r.Context(), w, flusher, iqCh, spectrumParams{
		fftSize:   fftSize,
		bins:      bins,
		fps:       fps,
		mode:      mode,
		avgFrames: frames,
	})
}

type spectrumParams struct {
	fftSize   int
	bins      int
	fps       int
	mode      dsp.DisplayMode
	avgFrames int // окно ModeAvg в кадрах UI
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
	pipeline.SetAvgFrames(p.avgFrames)
	chunker := newIQChunker(p.fftSize)

	frameDur := time.Second / time.Duration(p.fps)
	ticker := time.NewTicker(frameDur)
	defer ticker.Stop()

	sw := &spectrumWriter{
		w:       w,
		flusher: flusher,
		hdr:     make([]byte, dsp.HeaderSize),
		mode:    p.mode,
	}
	var center uint64 // частота приёмника в последнем блоке IQ
	var rate uint32   // частота дискретизации в последнем блоке IQ

	for {
		select {
		case frame, ok := <-iqCh:
			if !ok {
				sw.flushPipeline(pipeline)
				return
			}
			// Приёмник перестроили или сменили полосу: накопленное относится к прежней настройке
			if (center != 0 && frame.CenterHz != center) || (rate != 0 && frame.SampleRate != rate) {
				chunker.reset()
				pipeline.Reset()
			}
			center, rate = frame.CenterHz, frame.SampleRate
			sw.lastCenter = int64(frame.CenterHz)
			sw.lastSpan = int64(frame.SampleRate)
			chunker.feed(frame.Samples, pipeline.Push)

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

// iqChunker собирает отсчёты кадров устройства в блоки размера FFT клиента.
// Блок лежит в собственном буфере: кадр общий для всех зрителей,
// а оконная функция в SpectrumPipeline.Push меняет отсчёты на месте.
type iqChunker struct {
	buf []complex64
	n   int
}

func newIQChunker(size int) *iqChunker {
	return &iqChunker{buf: make([]complex64, size)}
}

// feed дописывает отсчёты и вызывает emit на каждый заполненный блок.
// Блок валиден только внутри emit; остаток ждёт следующего кадра.
func (c *iqChunker) feed(samples []complex64, emit func(block []complex64)) {
	for len(samples) > 0 {
		copied := copy(c.buf[c.n:], samples)
		c.n += copied
		samples = samples[copied:]
		if c.n == len(c.buf) {
			emit(c.buf)
			c.n = 0
		}
	}
}

// reset выбрасывает недособранный блок.
func (c *iqChunker) reset() { c.n = 0 }

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
// В режимах удержания за кадром удержания идёт мгновенный кадр с display_mode
// current: клиент рисует оба.
func (sw *spectrumWriter) flushPipeline(p *dsp.SpectrumPipeline) bool {
	dbfs := p.Flush()
	if dbfs == nil {
		return false
	}
	if !sw.writeFrame(dbfs, sw.mode) {
		return false
	}
	if live := p.FlushLive(); live != nil {
		return sw.writeFrame(live, dsp.ModeCurrent)
	}
	return true
}

func (sw *spectrumWriter) writeFrame(dbfs []float32, mode dsp.DisplayMode) bool {
	payload := dsp.Float32ToBytes(dbfs)
	dsp.EncodeHeader(sw.hdr, dsp.FrameHeader{
		DataType:    dsp.DataTypeFFTDB,
		DisplayMode: mode,
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

// avgFrames переводит окно усреднения window_ms в кадры UI при данном fps,
// с округлением; пусто или меньше кадра — один кадр.
func avgFrames(windowMs string, fps int) int {
	ms, err := strconv.Atoi(windowMs)
	if err != nil || ms <= 0 || fps <= 0 {
		return 1
	}
	n := (ms*fps + 500) / 1000
	return max(1, min(n, maxAvgFrames))
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
