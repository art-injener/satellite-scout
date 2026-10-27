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
	// SigMF открывается как Dataset и тоже отдаёт cf32_le (см. internal/sigmf).
	Source io.Reader
	// SourceCloser — если задан, закрывается в Stop (файл SigMF / .cfile).
	SourceCloser io.Closer
	// SourceOpen — заново открывает запись перед каждым StartStream (после Stop файл уже закрыт).
	// Если задан, Source/SourceCloser выставляет сам приёмник.
	SourceOpen func() (io.ReadCloser, error)

	// ReplaySampleRate — частота записи для Capabilities; 0 — обычный имитатор.
	ReplaySampleRate uint32
	// ReplayCenterHz — начальная настройка на центр записи; 0 — не трогать.
	ReplayCenterHz uint64

	// Signals — сигналы в эфире; сдвигаются при перестройке приёмника.
	// Приёмник при создании настроен на частоту первого сигнала.
	Signals []Signal

	// NoiseDBFS — мощность общего шума приёмника во всей полосе NoiseRefRateHz, dBFS
	// при усилении 0 дБ; уровни сигналов — тоже при 0 дБ.
	// SNR сигнала в полосе B: LevelDBFS − (NoiseDBFS + 10·lg(B/NoiseRefRateHz)). 0 — без шума.
	NoiseDBFS float64
	// NoiseRefRateHz — частота дискретизации, при которой задан NoiseDBFS. Плотность шума
	// постоянна, как у настоящего приёмника: при частоте fs мощность шума в полосе
	// NoiseDBFS + 10·lg(fs/NoiseRefRateHz). 0 — NoiseDBFS при любой частоте потока.
	NoiseRefRateHz float64
	Seed           uint64 // зерно шума: одинаковое зерно даёт одинаковый шум

	// ToneOffsetHz — смещение синтетического тона от центральной частоты (Гц).
	ToneOffsetHz float64
}

const (
	// DefaultFrequencyHz — частота эфира по умолчанию: любительский спутниковый участок 435–438 МГц.
	DefaultFrequencyHz = 436_500_000
	// DefaultNoiseDBFS — общий шум приёмника по умолчанию: заметно ниже полной шкалы, как у реального SDR.
	DefaultNoiseDBFS = -60.0
	// RecommendedSampleRate — частота дискретизации, если в конфиге тракта не задана: как у RTL-SDR в SatNOGS.
	RecommendedSampleRate = 2_048_000
)

// DefaultOptions — эфир по умолчанию: маяки всех модемов на −50 dBFS поверх шума
// DefaultNoiseDBFS (SNR около 30 дБ в 15 кГц), кадр раз в DefaultBeaconPeriod.
func DefaultOptions() Options {
	signals := make([]Signal, 0, len(defaultBeacons))
	for _, b := range defaultBeacons {
		signals = append(signals, Signal{
			Kind:        SignalBeacon,
			FrequencyHz: b.freqHz,
			LevelDBFS:   -50,
			Frame:       DefaultBeaconFrame(b.modem),
			Period:      DefaultBeaconPeriod,
			Modem:       b.modem,
		})
	}
	return Options{
		Signals:        signals,
		NoiseDBFS:      DefaultNoiseDBFS,
		NoiseRefRateHz: RecommendedSampleRate,
		Seed:           1,
	}
}

// defaultBeacons — маяки эфира по умолчанию, по одному на модем: приём каждого формата
// проверяется без правки конфига. Частоты — в пределах ±128 кГц от DefaultFrequencyHz,
// то есть в самой узкой «Полосе», и не ближе 50 кГц друг к другу. Приёмник стартует на
// частоте первого сигнала, поэтому первым идёт AFSK 1200 в центре: его принимают настройки
// канала по умолчанию (сдвиг 0, AFSK 1200).
var defaultBeacons = []struct {
	modem  BeaconModem
	freqHz uint64
}{
	{BeaconAFSK1200, DefaultFrequencyHz},
	{BeaconFSK9600G3RUH, DefaultFrequencyHz - 50_000},
	{BeaconFSK9600GEOSCAN, DefaultFrequencyHz + 50_000},
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
// на ней: сигнал виден в спектре без перестройки. ReplayCenterHz важнее сигналов.
func New(opts Options) *Receiver {
	r := &Receiver{opts: opts}
	switch {
	case opts.ReplayCenterHz != 0:
		_ = r.Tune(opts.ReplayCenterHz)
	case len(opts.Signals) > 0:
		_ = r.Tune(opts.Signals[0].FrequencyHz)
	}
	return r
}

func (r *Receiver) Name() string { return "imitator" }

func (r *Receiver) Capabilities() sdr.Capabilities {
	rec := uint32(RecommendedSampleRate)
	minRate := uint32(48_000)
	if r.opts.ReplaySampleRate != 0 {
		rec = r.opts.ReplaySampleRate
		if rec < minRate {
			minRate = rec
		}
	}
	return sdr.Capabilities{
		MinFreqHz:             24_000_000,
		MaxFreqHz:             1_766_000_000,
		MaxBandwidthHz:        2_400_000,
		SampleRateRanges:      []sdr.SampleRateRange{{MinHz: minRate, MaxHz: 2_400_000}},
		RecommendedSampleRate: rec,
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

	if err := r.openReplayLocked(); err != nil {
		return nil, err
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
	if r.opts.Source != nil {
		blockSize = replayBlockSize(cfg.SampleRate, blockSize)
	}
	ch := make(chan sdr.IQFrame, 4)

	go func() {
		defer close(ch)
		defer r.markStopped()

		switch {
		case r.opts.Source != nil:
			r.streamFromReader(streamCtx, cfg, blockSize, ch)
		case air != nil:
			r.streamGenerated(streamCtx, cfg, blockSize, ch, r.withGain(air.fill))
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
	r.closeSource()
	return nil
}

func (r *Receiver) closeSource() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeSourceLocked()
}

func (r *Receiver) closeSourceLocked() {
	if c := r.opts.SourceCloser; c != nil {
		_ = c.Close()
		r.opts.SourceCloser = nil
		r.opts.Source = nil
	}
}

// openReplayLocked готовит Source к чтению: заново открывает через SourceOpen или Seek(0).
func (r *Receiver) openReplayLocked() error {
	if r.opts.SourceOpen != nil {
		r.closeSourceLocked()
		rc, err := r.opts.SourceOpen()
		if err != nil {
			return fmt.Errorf("imitator replay open: %w", err)
		}
		r.opts.Source = rc
		r.opts.SourceCloser = rc
		return nil
	}
	if s, ok := r.opts.Source.(io.Seeker); ok {
		if _, err := s.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("imitator replay seek: %w", err)
		}
	}
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

// withGain умножает эфир на текущее усиление приёмника: уровни в конфиге заданы при
// усилении 0 дБ, как мощность на антенне, а dBFS растут вместе с усилением.
// Отрицательное усиление означает АРУ, эфир остаётся как в конфиге.
func (r *Receiver) withGain(generate func(dst []complex64, centerHz uint64)) func(dst []complex64, centerHz uint64) {
	return func(dst []complex64, centerHz uint64) {
		generate(dst, centerHz)
		r.mu.Lock()
		gainDB := r.gainDB
		r.mu.Unlock()
		if gainDB <= 0 {
			return
		}
		k := complex(float32(math.Pow(10, float64(gainDB)/20)), 0)
		for i := range dst {
			dst[i] *= k
		}
	}
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

// replayBlockSize — длина блока для файла: не длиннее ~50 мс (DefaultBlockSize при 32 ksps
// даёт полсекунды и редкие тяжёлые рывки), но не мельче 512 отсчётов.
func replayBlockSize(sampleRate uint32, want int) int {
	if sampleRate == 0 || want <= 0 {
		return want
	}
	maxBlock := int(sampleRate / 20)
	if maxBlock < 512 {
		maxBlock = 512
	}
	if want > maxBlock {
		return maxBlock
	}
	return want
}

// streamFromReader читает IQ из io.Reader (формат .cfile: float32 I, float32 Q, LE)
// в темпе реального времени. Если Source умеет Seek — при EOF крутит запись с начала;
// иначе отдаёт остаток кадра и завершает поток.
// Читает блоками, не по одной паре I/Q: иначе десятки тысяч syscall на кадр и UI «плывёт».
func (r *Receiver) streamFromReader(ctx context.Context, cfg sdr.StreamConfig, blockSize int, ch chan<- sdr.IQFrame) {
	byteBuf := make([]byte, blockSize*8)
	frameDuration := time.Duration(float64(blockSize) / float64(cfg.SampleRate) * float64(time.Second))
	next := time.Now()
	seeker, canLoop := r.opts.Source.(io.Seeker)

	for {
		samples := make([]complex64, 0, blockSize)
		for len(samples) < blockSize {
			need := (blockSize - len(samples)) * 8
			n, err := io.ReadFull(r.opts.Source, byteBuf[:need])
			n -= n % 8
			for off := 0; off < n; off += 8 {
				re := math.Float32frombits(binary.LittleEndian.Uint32(byteBuf[off : off+4]))
				im := math.Float32frombits(binary.LittleEndian.Uint32(byteBuf[off+4 : off+8]))
				samples = append(samples, complex(re, im))
			}
			if err == nil {
				continue
			}
			if canLoop && (err == io.EOF || err == io.ErrUnexpectedEOF) {
				if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr != nil {
					break
				}
				// Пустой файл: Seek снова даёт EOF — не крутить CPU вхолостую.
				if n == 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(frameDuration):
					}
				}
				continue
			}
			break
		}
		if len(samples) == 0 {
			return
		}
		if !r.sendFrame(ctx, ch, samples, cfg) {
			return
		}
		if len(samples) < blockSize && !canLoop {
			return
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
