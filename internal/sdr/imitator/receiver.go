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

// Options — параметры создания имитатора.
type Options struct {
	// Source — внешний источник IQ (формат .cfile: float32 I, float32 Q, little-endian).
	// Если nil — генерируется синтетический тон с частотой ToneOffsetHz.
	Source io.Reader

	// ToneOffsetHz — смещение синтетического тона от центральной частоты (Гц).
	ToneOffsetHz float64
}

// DefaultOptions возвращает настройки с синтетическим тоном 1 кГц.
func DefaultOptions() Options {
	return Options{ToneOffsetHz: 1000}
}

// Receiver — имитатор SDR-приёмника для TDD без железа.
type Receiver struct {
	opts Options

	mu      sync.Mutex
	tuned   uint64
	cancel  context.CancelFunc
	stopped chan struct{}
	running bool
}

// New создаёт имитатор SDR.
func New(opts Options) *Receiver {
	return &Receiver{opts: opts}
}

func (r *Receiver) Name() string { return "imitator" }

func (r *Receiver) Capabilities() sdr.Capabilities {
	return sdr.Capabilities{
		MinFreqHz:      24_000_000,
		MaxFreqHz:      1_766_000_000,
		MaxBandwidthHz: 2_400_000,
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

func (r *Receiver) StartStream(ctx context.Context, cfg sdr.StreamConfig) (<-chan sdr.IQFrame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return nil, ErrStreamAlreadyRunning
	}
	r.running = true
	r.stopped = make(chan struct{})

	streamCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	blockSize := cfg.EffectiveBlockSize()
	ch := make(chan sdr.IQFrame, 4)

	go func() {
		defer close(ch)
		defer r.markStopped()

		if r.opts.Source != nil {
			r.streamFromReader(streamCtx, cfg, blockSize, ch)
		} else {
			r.streamSyntheticTone(streamCtx, cfg, blockSize, ch)
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

// streamSyntheticTone генерирует комплексную экспоненту exp(j*2π*f_offset*n/sampleRate).
func (r *Receiver) streamSyntheticTone(
	ctx context.Context,
	cfg sdr.StreamConfig,
	blockSize int,
	ch chan<- sdr.IQFrame,
) {
	phaseInc := 2 * math.Pi * r.opts.ToneOffsetHz / float64(cfg.SampleRate)
	sampleIdx := 0
	frameDuration := time.Duration(float64(blockSize) / float64(cfg.SampleRate) * float64(time.Second))

	for {
		samples := make([]complex64, blockSize)
		for i := range samples {
			phase := phaseInc * float64(sampleIdx)
			samples[i] = complex(float32(math.Cos(phase)), float32(math.Sin(phase)))
			sampleIdx++
		}

		frame := sdr.IQFrame{
			Samples:     samples,
			CenterHz:    cfg.CenterHz,
			SampleRate:  cfg.SampleRate,
			TimestampMs: nowMs(),
		}

		select {
		case <-ctx.Done():
			return
		case ch <- frame:
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(frameDuration):
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
	frame := sdr.IQFrame{
		Samples:     samples,
		CenterHz:    cfg.CenterHz,
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
