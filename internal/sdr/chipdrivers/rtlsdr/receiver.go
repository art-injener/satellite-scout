package rtlsdr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	// DefaultFrequencyHz — частота до первой перестройки: середина любительского участка 70 см.
	DefaultFrequencyHz = 436_500_000
	// RecommendedSampleRate — частота дискретизации, если в конфиге тракта не задана.
	// С запасом над видимыми 2,048 МГц: края окна отрезаются (usableFraction).
	RecommendedSampleRate = 2_560_000
	// usableFraction — годная середина окна. На краях завал от фильтра ПЧ тюнера и цифрового
	// ФНЧ демодулятора, туда же складываются зеркала из-за Найквиста.
	usableFraction = 0.8
	// ifOffsetTuneHz — ПЧ демодулятора выше середины фильтра тюнера.
	// 160 кГц (rate/16) уже вытаскивает край окна на спад фильтра: пол косой.
	// 10 кГц на фоне полосы в 2 МГц не видно, а срез выреза постоянной 100 Гц
	// несущую на таком отступе не задевает. После выреза спектр переносится
	// обратно, в центре остаётся сигнал, фильтр по-прежнему на частоте настройки.
	ifOffsetTuneHz = 10_000

	bulkEndpoint uint8 = 0x81
	// streamBuffers — запросов в очереди ядра: 16 буферов по блоку, при 2 МГц около 130 мс запаса.
	streamBuffers = 16
	// usbPacket — размер пакета bulk на высокой скорости; буфер запроса кратен ему.
	usbPacket = 512
	// autoGain — усиление, означающее АРУ.
	autoGain = -1
	// defaultGainDB — ручное усиление при старте, ступень R820T 32,8 дБ.
	// АРУ выключена: она поднимает усилитель до упора и перегружает вход.
	// 32,8 дБ — середина шкалы, потолок 49,6: слабый КА ещё слышно, сильное вещание рядом не забивает АЦП.
	defaultGainDB float32 = 32.8
)

// ErrStreamAlreadyRunning — поток уже запущен.
var ErrStreamAlreadyRunning = errors.New("rtlsdr: stream already running")

// usbDevice — открытый донгл с захваченным интерфейсом 0.
type usbDevice interface {
	controller
	ClearHalt(endpoint uint8) error
	OpenStream(endpoint uint8, buffers, size int) (bulkStream, error)
	Close() error
}

// bulkStream — очередь запросов bulk: Read отдаёт буфер, действительный до следующего Read.
type bulkStream interface {
	Read() ([]byte, error)
	Close() error
}

// Options — какой донгл открыть.
type Options struct {
	// Serial — серийный номер; пусто — по пути или первый найденный.
	Serial string
	// DevicePath — узел /dev/bus/usb/BBB/DDD: различает донглы с одинаковым серийным номером.
	DevicePath string
	// PPM — поправка частоты кварца в миллионных долях, как ключ -p у rtl_sdr.
	PPM int

	open func(Options) (usbDevice, error)
}

// Receiver — RTL-SDR с тюнером R820T как sdr.Receiver. Донгл открывается на время потока:
// без зрителей он свободен для других программ.
type Receiver struct {
	opts Options

	mu         sync.Mutex
	centerHz   uint64
	gainDB     float32
	sampleRate uint32
	shiftHz    int32 // цифровой перенос, Гц: минус сдвиг ПЧ демодулятора
	chip       *chip
	tuner      *r820t
	cancel     context.CancelFunc
	done       chan struct{}
}

// New создаёт приёмник; к донглу он обращается только при запуске потока.
func New(opts Options) *Receiver {
	if opts.open == nil {
		opts.open = openUSB
	}
	return &Receiver{opts: opts, centerHz: DefaultFrequencyHz, gainDB: defaultGainDB}
}

func (r *Receiver) Name() string { return "rtlsdr" }

func (r *Receiver) Capabilities() sdr.Capabilities {
	return sdr.Capabilities{
		MinFreqHz:      uint64(r820tMinHz),
		MaxFreqHz:      uint64(r820tMaxHz),
		MaxBandwidthHz: 2_400_000,
		SampleRateRanges: []sdr.SampleRateRange{
			{MinHz: minSampleRate, MaxHz: gapLow},
			{MinHz: gapHigh + 1, MaxHz: maxSampleRate},
		},
		RecommendedSampleRate: RecommendedSampleRate,
		UsableFraction:        usableFraction,
	}
}

// usableBand — ширина годной середины окна при частоте дискретизации rate.
func usableBand(rate uint32) uint32 {
	return sdr.Capabilities{UsableFraction: usableFraction}.UsableBandHz(rate)
}

func (r *Receiver) Tune(frequencyHz uint64) error {
	if frequencyHz < uint64(r820tMinHz) || frequencyHz > uint64(r820tMaxHz) {
		return fmt.Errorf("rtlsdr: %d Hz outside %d–%d Hz", frequencyHz, r820tMinHz, r820tMaxHz)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tuner != nil {
		if err := r.tuner.setFreq(uint32(frequencyHz)); err != nil {
			return err
		}
	}
	r.centerHz = frequencyHz
	return nil
}

// ifOffsetHz — на сколько ПЧ демодулятора выше середины фильтра тюнера.
// На очень низкой частоте дискретизации отступ уменьшается, чтобы остаться в полосе.
func ifOffsetHz(rate uint32) uint32 {
	if rate < ifOffsetTuneHz*4 {
		return rate / 4
	}
	return ifOffsetTuneHz
}

// SetGain задаёт усиление тюнера в дБ; отрицательное — АРУ.
func (r *Receiver) SetGain(db float32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tuner != nil {
		if err := applyGain(r.tuner, db); err != nil {
			return err
		}
	}
	r.gainDB = db
	return nil
}

func applyGain(t *r820t, db float32) error {
	if db < 0 {
		return t.setGain(false, 0)
	}
	return t.setGain(true, int(math.Round(float64(db)*10)))
}

func (r *Receiver) Status() sdr.ReceiverStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sdr.ReceiverStatus{
		CenterHz:   r.centerHz,
		SampleRate: r.sampleRate,
		GainDB:     r.gainDB,
		Running:    r.done != nil,
	}
}

func (r *Receiver) StartStream(ctx context.Context, cfg sdr.StreamConfig) (<-chan sdr.IQFrame, error) {
	if cfg.SampleRate == 0 {
		return nil, sdr.ErrSampleRateRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done != nil {
		return nil, ErrStreamAlreadyRunning
	}

	dev, err := r.opts.open(r.opts)
	if err != nil {
		return nil, err
	}
	c := newChip(dev)
	c.ppm = r.opts.PPM
	tuner, rate, err := bringUp(c, cfg.SampleRate, r.centerHz, r.gainDB)
	if err != nil {
		_ = c.powerOff()
		_ = dev.Close()
		return nil, err
	}
	size := (2*cfg.EffectiveBlockSize() + usbPacket - 1) / usbPacket * usbPacket
	stream, err := startBulk(c, dev, size)
	if err != nil {
		shutdown(c, tuner, dev)
		return nil, err
	}

	r.chip, r.tuner, r.sampleRate = c, tuner, rate
	r.shiftHz = -int32(ifOffsetHz(rate))
	streamCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	done := make(chan struct{})
	r.done = done
	ch := make(chan sdr.IQFrame, 4)
	go func() {
		<-streamCtx.Done()
		_ = stream.Close()
	}()
	go r.pump(streamCtx, stream, dev, rate, ch, done)
	return ch, nil
}

// bringUp запускает демодулятор и тюнер и настраивает их на частоту, полосу и усиление.
func bringUp(c *chip, rate uint32, centerHz uint64, gainDB float32) (*r820t, uint32, error) {
	// Первая запись после захвата интерфейса у части донглов теряется: она пробная.
	_ = c.writeReg(blockUSB, regSysctl, 0x09, 1)
	if err := c.initBaseband(); err != nil {
		return nil, 0, err
	}
	if !detectR820T(c) {
		return nil, 0, errors.New("rtlsdr: tuner R820T/R820T2 not found")
	}
	if err := prepareDemod(c); err != nil {
		return nil, 0, err
	}
	t := newR820T(c)
	if err := t.init(); err != nil {
		return nil, 0, err
	}
	actual, err := c.setSampleRate(rate)
	if err != nil {
		return nil, 0, err
	}
	// Фильтр ПЧ вокруг 6 МГц и на запрошенной частоте. Демодулятор смотрит на
	// ifOffsetTuneHz выше, поэтому постоянная не попадает в середину фильтра.
	// Гетеродин считает LO как частота плюс ПЧ фильтра, не плюс ПЧ демодулятора.
	ifHz, err := t.setBandwidth(ifBandwidthForRate(actual))
	if err != nil {
		return nil, 0, err
	}
	if err := c.setIFFreq(ifHz + ifOffsetHz(actual)); err != nil {
		return nil, 0, err
	}
	if err := t.setFreq(uint32(centerHz)); err != nil {
		return nil, 0, err
	}
	if err := applyGain(t, gainDB); err != nil {
		return nil, 0, err
	}
	return t, actual, nil
}

// startBulk сбрасывает FIFO и запускает очередь запросов. Остановку конечной точки, которую
// оставляет драйвер DVB ядра, снимают и на чипе (сброс FIFO), и на хосте.
func startBulk(c *chip, dev usbDevice, size int) (bulkStream, error) {
	if err := c.resetBuffer(); err != nil {
		return nil, err
	}
	if err := dev.ClearHalt(bulkEndpoint); err != nil {
		return nil, fmt.Errorf("rtlsdr: clear halt: %w", err)
	}
	return dev.OpenStream(bulkEndpoint, streamBuffers, size)
}

// shutdown усыпляет тюнер, выключает демодулятор и отпускает донгл.
func shutdown(c *chip, t *r820t, dev usbDevice) {
	if t != nil {
		_ = t.standby()
	}
	_ = c.powerOff()
	_ = dev.Close()
}

// pump переводит буферы донгла в кадры, пока поток не остановят или донгл не пропадёт.
func (r *Receiver) pump(ctx context.Context, stream bulkStream, dev usbDevice, rate uint32,
	ch chan<- sdr.IQFrame, done chan struct{},
) {
	defer close(done)
	defer close(ch)
	r.mu.Lock()
	corr := dsp.NewDCShift(float64(r.shiftHz), float64(rate))
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		// Отмена отпускает горутину, ждущую конца потока, и когда донгл пропал сам.
		r.cancel()
		shutdown(r.chip, r.tuner, dev)
		r.chip, r.tuner, r.done, r.cancel = nil, nil, nil, nil
		r.mu.Unlock()
	}()
	for {
		buf, err := stream.Read()
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("rtlsdr: stream ended", slog.Any("error", err))
			}
			_ = stream.Close()
			return
		}
		r.mu.Lock()
		center := r.centerHz
		shift := r.shiftHz
		r.mu.Unlock()
		samples := convertU8(buf)
		corr.SetOffset(float64(shift))
		corr.Correct(samples)
		frame := sdr.IQFrame{
			Samples:     samples,
			CenterHz:    center,
			SampleRate:  rate,
			TimestampMs: uint64(time.Now().UnixMilli()),
		}
		select {
		case ch <- frame:
		case <-ctx.Done():
			return
		}
	}
}

func (r *Receiver) Stop() error {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if done == nil {
		return nil
	}
	cancel()
	<-done
	return nil
}

// u8ToF32 — отсчёт донгла без знака в [-1, 1]: середина шкалы 127,5.
var u8ToF32 = func() (t [256]float32) {
	for i := range t {
		t[i] = (float32(i) - 127.5) / 127.5
	}
	return t
}()

func convertU8(buf []byte) []complex64 {
	out := make([]complex64, len(buf)/2)
	for i := range out {
		out[i] = complex(u8ToF32[buf[2*i]], u8ToF32[buf[2*i+1]])
	}
	return out
}
