package imitator

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

var (
	ErrFrequencyOutOfRange  = errors.New("frequency out of range")
	ErrStreamAlreadyRunning = errors.New("stream already running")
)

// Options — параметры создания имитатора. Источник выбирается по первому заданному:
// Source, затем эфир из Signals и NoiseDBFS, иначе синтетический тон.
type Options struct {
	// Source — внешний источник IQ (формат .cfile: float32 I, float32 Q, little-endian).
	Source io.Reader

	// Signals — сигналы в эфире; сдвигаются при перестройке приёмника.
	// Приёмник при создании настроен на частоту первого сигнала.
	Signals []Signal

	// NoiseDBFS — мощность общего шума приёмника во всей полосе дискретизации, dBFS.
	// SNR сигнала в полосе B: LevelDBFS − (NoiseDBFS + 10·lg(B/fs)). 0 — без шума.
	NoiseDBFS float64
	Seed      uint64 // зерно шума: одинаковое зерно даёт одинаковый шум

	// ToneOffsetHz — смещение синтетического тона от центральной частоты (Гц).
	ToneOffsetHz float64
}

const (
	// DefaultFrequencyHz — частота эфира по умолчанию: любительский спутниковый участок 435–438 МГц.
	DefaultFrequencyHz = 436_500_000
	// DefaultNoiseDBFS — общий шум приёмника по умолчанию: заметно ниже полной шкалы, как у реального SDR.
	DefaultNoiseDBFS = -60.0
)

// DefaultOptions — эфир по умолчанию: несущая ходит ±1 МГц вокруг DefaultFrequencyHz за минуту
// поверх шума DefaultNoiseDBFS, уровень по профилю пролёта за 45 с от −40 до −90 dBFS.
// SNR в полосе 12,5 кГц от 42 до −8 дБ: сигнал уходит под шум у горизонта.
func DefaultOptions() Options {
	return Options{
		Signals: []Signal{{
			Kind:        SignalCarrier,
			FrequencyHz: DefaultFrequencyHz,
			LevelDBFS:   -40,
			SweepHz:     1_000_000,
			SweepPeriod: time.Minute,
			PassSwingDB: 50,
			PassPeriod:  45 * time.Second,
		}},
		NoiseDBFS: DefaultNoiseDBFS,
		Seed:      1,
	}
}

// Receiver — имитатор SDR-приёмника для TDD без железа.
type Receiver struct {
	opts Options

	mu         sync.Mutex
	tuned      uint64
	gainDB     float32
	sampleRate uint32
	cancel     context.CancelFunc
	stopped    chan struct{}
	running    bool
}

// New создаёт имитатор SDR. Если частота первого сигнала в диапазоне приёмника, он сразу
// на ней: сигнал виден в спектре без перестройки.
func New(opts Options) *Receiver {
	r := &Receiver{opts: opts}
	if len(opts.Signals) > 0 {
		_ = r.Tune(opts.Signals[0].FrequencyHz)
	}
	return r
}

func (r *Receiver) Name() string { return "imitator" }

func (r *Receiver) Capabilities() sdr.Capabilities {
	return sdr.Capabilities{
		MinFreqHz:      24_000_000,
		MaxFreqHz:      1_766_000_000,
		MaxBandwidthHz: 2_400_000,
		// Рекомендуемая частота как у RTL-SDR в SatNOGS.
		SampleRateRanges:      []sdr.SampleRateRange{{MinHz: 48_000, MaxHz: 2_400_000}},
		RecommendedSampleRate: 2_048_000,
	}
}

func (r *Receiver) Tune(frequencyHz uint64) error {
	caps := r.Capabilities()
	if frequencyHz < caps.MinFreqHz || frequencyHz > caps.MaxFreqHz {
		return fmt.Errorf("%w: %d Hz not in [%d, %d]",
			ErrFrequencyOutOfRange, frequencyHz, caps.MinFreqHz, caps.MaxFreqHz)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tuned = frequencyHz
	return nil
}

func (r *Receiver) SetGain(db float32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gainDB = db
	return nil
}

func (r *Receiver) Status() sdr.ReceiverStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sdr.ReceiverStatus{
		CenterHz:   r.tuned,
		SampleRate: r.sampleRate,
		GainDB:     r.gainDB,
		Running:    r.running,
	}
}

func (r *Receiver) StartStream(ctx context.Context, cfg sdr.StreamConfig) (<-chan sdr.IQFrame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return nil, ErrStreamAlreadyRunning
	}
	if cfg.SampleRate == 0 {
		return nil, sdr.ErrSampleRateRequired
	}

	var air *scene
	if r.opts.Source == nil && (len(r.opts.Signals) > 0 || r.opts.NoiseDBFS != 0) {
		var err error
		if air, err = newScene(r.opts, float64(cfg.SampleRate)); err != nil {
			return nil, err
		}
	}

	r.running = true
	r.stopped = make(chan struct{})
	r.sampleRate = cfg.SampleRate
	if cfg.CenterHz != 0 && r.tuned == 0 {
		r.tuned = cfg.CenterHz
	}
	if cfg.GainDB != 0 && r.gainDB == 0 {
		r.gainDB = cfg.GainDB
	}

	streamCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	blockSize := cfg.EffectiveBlockSize()
	ch := make(chan sdr.IQFrame, 4)

	go func() {
		defer close(ch)
		defer r.markStopped()

		switch {
		case r.opts.Source != nil:
			r.streamFromReader(streamCtx, cfg, blockSize, ch)
		case air != nil:
			r.streamGenerated(streamCtx, cfg, blockSize, ch, air.fill)
		default:
			r.streamGenerated(streamCtx, cfg, blockSize, ch, r.syntheticTone(cfg.SampleRate))
		}
	}()

	return ch, nil
}

func (r *Receiver) Stop() error {
	stopped := r.requestStop()
	if stopped == nil {
		return nil
	}
	<-stopped
	return nil
}

// requestStop отменяет контекст потока и возвращает канал ожидания остановки.
// nil — поток не был запущен.
func (r *Receiver) requestStop() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return nil
	}
	r.cancel()
	return r.stopped
}

// markStopped вызывается горутиной потока при завершении.
func (r *Receiver) markStopped() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = false
	close(r.stopped)
}

// syntheticTone возвращает генератор комплексной экспоненты exp(j*2π*f_offset*n/sampleRate).
func (r *Receiver) syntheticTone(sampleRate uint32) func(dst []complex64, centerHz uint64) {
	phaseInc := 2 * math.Pi * r.opts.ToneOffsetHz / float64(sampleRate)
	sampleIdx := 0
	return func(dst []complex64, _ uint64) {
		for i := range dst {
			phase := phaseInc * float64(sampleIdx)
			dst[i] = complex(float32(math.Cos(phase)), float32(math.Sin(phase)))
			sampleIdx++
		}
	}
}

// maxScheduleLagBlocks — на сколько блоков поток может отстать от расписания и догнать его.
// Больше: пропущенное не догоняется, расписание идёт от текущего момента. Иначе после
// задержки потребитель получил бы залп блоков и время эфира побежало бы быстрее.
const maxScheduleLagBlocks = 4

// streamGenerated выдаёт блоки от генератора в темпе реального времени: блок n уходит
// в момент «старт + n·длительность блока», время генерации из паузы вычитается.
// Генератор получает текущую частоту настройки приёмника.
func (r *Receiver) streamGenerated(
	ctx context.Context,
	cfg sdr.StreamConfig,
	blockSize int,
	ch chan<- sdr.IQFrame,
	generate func(dst []complex64, centerHz uint64),
) {
	frameDuration := time.Duration(float64(blockSize) / float64(cfg.SampleRate) * float64(time.Second))
	next := time.Now()

	for {
		r.mu.Lock()
		centerHz := r.tuned
		r.mu.Unlock()

		samples := make([]complex64, blockSize)
		generate(samples, centerHz)

		frame := sdr.IQFrame{
			Samples:     samples,
			CenterHz:    centerHz,
			SampleRate:  cfg.SampleRate,
			TimestampMs: nowMs(),
		}

		select {
		case <-ctx.Done():
			return
		case ch <- frame:
		}

		next = next.Add(frameDuration)
		wait := time.Until(next)
		if wait < -maxScheduleLagBlocks*frameDuration {
			next, wait = time.Now(), 0
		}
		if wait <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// streamFromReader читает IQ из io.Reader (формат .cfile: float32 I, float32 Q, LE).
// При EOF — отправляет остаток и завершает чтение.
func (r *Receiver) streamFromReader(ctx context.Context, cfg sdr.StreamConfig, blockSize int, ch chan<- sdr.IQFrame) {
	pairBuf := make([]byte, 8) // float32 I + float32 Q
	samples := make([]complex64, 0, blockSize)

	for {
		_, err := io.ReadFull(r.opts.Source, pairBuf)
		if err != nil {
			if len(samples) > 0 {
				r.sendFrame(ctx, ch, samples, cfg)
			}
			return
		}

		re := math.Float32frombits(binary.LittleEndian.Uint32(pairBuf[0:4]))
		im := math.Float32frombits(binary.LittleEndian.Uint32(pairBuf[4:8]))
		samples = append(samples, complex(re, im))

		if len(samples) == blockSize {
			if !r.sendFrame(ctx, ch, samples, cfg) {
				return
			}
			samples = make([]complex64, 0, blockSize)
		}
	}
}

func (r *Receiver) sendFrame(
	ctx context.Context,
	ch chan<- sdr.IQFrame,
	samples []complex64,
	cfg sdr.StreamConfig,
) bool {
	r.mu.Lock()
	centerHz := r.tuned
	r.mu.Unlock()

	frame := sdr.IQFrame{
		Samples:     samples,
		CenterHz:    centerHz,
		SampleRate:  cfg.SampleRate,
		TimestampMs: nowMs(),
	}
	select {
	case <-ctx.Done():
		return false
	case ch <- frame:
		return true
	}
}

func nowMs() uint64 {
	return uint64(time.Now().UnixMilli())
}
