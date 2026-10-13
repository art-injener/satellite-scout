package imitator

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/modem"
)

// ErrInvalidSignal — сигнал имитатора задан неверно: неизвестный вид или ход без периода.
var ErrInvalidSignal = errors.New("invalid imitator signal")

// SignalKind — вид сигнала в эфире имитатора.
type SignalKind string

const (
	// SignalCarrier — немодулированная несущая.
	SignalCarrier SignalKind = "carrier"
	// SignalAFSKPRBS — непрерывный AFSK 1200, биты PRBS-9 идут на линию напрямую: эталон для BER.
	SignalAFSKPRBS SignalKind = "afsk_prbs"
	// SignalBeacon — маяк КА: кадр AX.25 в AFSK 1200 раз в Period.
	SignalBeacon SignalKind = "beacon"
)

// maxSweepShare — наибольший размах хода как доля частоты дискретизации: несущая остаётся внутри полосы.
const maxSweepShare = 0.45

// Signal — источник в эфире имитатора. Сдвиг от центра приёмника в момент t:
// FrequencyHz − центр + DopplerHz + DopplerRateHzS·t + ход(t).
type Signal struct {
	Kind        SignalKind
	FrequencyHz uint64  // несущая передатчика; для хода — его центр
	LevelDBFS   float64 // мощность на входе приёмника, dBFS; 0 — полная шкала

	DopplerHz      float64 // постоянный сдвиг Доплера, Гц
	DopplerRateHzS float64 // линейный дрейф Доплера, Гц/с

	// SweepHz — размах хода по частоте от FrequencyHz: туда-обратно с постоянной скоростью,
	// старт с центра вверх. Ограничен maxSweepShare частоты дискретизации. 0 — без хода.
	SweepHz     float64
	SweepPeriod time.Duration // полный цикл хода

	// PassSwingDB — профиль пролёта: уровень LevelDBFS в середине пролёта и на PassSwingDB ниже
	// у горизонта, затуханием пролёта по прямой: −10·lg(1 + k·x²), x от −1 до 1.
	// Старт с середины пролёта, пролёты повторяются через PassPeriod. 0 — уровень постоянный.
	PassSwingDB float64
	PassPeriod  time.Duration

	Frame  ax25.Frame    // кадр маяка
	Period time.Duration // интервал между началами кадров маяка
}

// cleanGen выдаёт сигнал без сдвига по частоте, с единичной амплитудой.
type cleanGen interface {
	fillClean(dst []complex64)
}

// signalSource переносит сигнал на его частоту относительно приёмника и задаёт уровень.
// Не потокобезопасен: работает в горутине потока.
type signalSource struct {
	sig        Signal
	sampleRate float64
	sweepHz    float64
	gen        cleanGen
	channel    *modem.Channel
	gain       float32 // амплитудный множитель уровня LevelDBFS
	passK      float64 // k профиля пролёта: 10^(PassSwingDB/10) − 1

	centerHz uint64 // частота настройки приёмника для текущего блока
	emitted  int    // отсчётов выдано с начала потока
	clean    []complex64
}

func newSignalSource(sig Signal, sampleRate float64) (*signalSource, error) {
	if sig.SweepHz != 0 && sig.SweepPeriod <= 0 {
		return nil, fmt.Errorf("%w: sweep without period", ErrInvalidSignal)
	}
	if sig.PassSwingDB < 0 || (sig.PassSwingDB != 0 && sig.PassPeriod <= 0) {
		return nil, fmt.Errorf("%w: pass swing must be non-negative and have a period", ErrInvalidSignal)
	}
	var gen cleanGen
	switch sig.Kind {
	case SignalCarrier:
		gen = carrierGen{}
	case SignalAFSKPRBS:
		gen = newAFSKPRBSGen(sampleRate)
	case SignalBeacon:
		b, err := newBeaconGen(sig, sampleRate)
		if err != nil {
			return nil, err
		}
		gen = b
	default:
		return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalidSignal, sig.Kind)
	}

	s := &signalSource{
		sig:        sig,
		sampleRate: sampleRate,
		sweepHz:    min(math.Abs(sig.SweepHz), maxSweepShare*sampleRate),
		gen:        gen,
		gain:       float32(math.Pow(10, sig.LevelDBFS/20)),
		passK:      math.Pow(10, sig.PassSwingDB/10) - 1,
	}
	s.channel = modem.NewChannel(modem.ChannelConfig{SampleRate: sampleRate, OffsetHz: s.offsetHz})
	return s, nil
}

// offsetHz — сдвиг сигнала от центра приёмника в момент t от начала потока.
func (s *signalSource) offsetHz(t time.Duration) float64 {
	sec := t.Seconds()
	off := float64(s.sig.FrequencyHz) - float64(s.centerHz) + s.sig.DopplerHz + s.sig.DopplerRateHzS*sec
	if s.sweepHz != 0 {
		// Треугольник: 0 в начале, +1 на четверти периода, −1 на трёх четвертях.
		x := sec/s.sig.SweepPeriod.Seconds() + 0.25
		off += s.sweepHz * (1 - 4*math.Abs(x-math.Floor(x)-0.5))
	}
	return off
}

// passGain — амплитудный множитель в момент sec от начала потока по профилю пролёта:
// 10^(LevelDBFS/20) / √(1 + k·x²), то же, что −10·lg(1 + k·x²) в дБ.
func (s *signalSource) passGain(sec float64) float32 {
	u := sec/s.sig.PassPeriod.Seconds() + 0.5
	x := 2*(u-math.Floor(u)) - 1
	return s.gain / float32(math.Sqrt(1+s.passK*x*x))
}

// fill заполняет dst сигналом для приёмника, настроенного на centerHz.
func (s *signalSource) fill(dst []complex64, centerHz uint64) {
	s.centerHz = centerHz
	if cap(s.clean) < len(dst) {
		s.clean = make([]complex64, len(dst))
	}
	clean := s.clean[:len(dst)]
	s.gen.fillClean(clean)

	// Сигнал за пределами полосы приёмника не виден: без этого сдвиг частоты дал бы наложение спектра.
	start := s.emitted
	t := time.Duration(float64(start) / s.sampleRate * float64(time.Second))
	if math.Abs(s.offsetHz(t)) > s.sampleRate/2 {
		clear(clean)
	}
	s.emitted += len(dst)

	s.channel.Process(dst[:0], clean)
	switch {
	case s.sig.PassSwingDB != 0:
		for i := range dst {
			dst[i] *= complex(s.passGain(float64(start+i)/s.sampleRate), 0)
		}
	case s.gain != 1:
		g := complex(s.gain, 0)
		for i := range dst {
			dst[i] *= g
		}
	}
}

// carrierGen — немодулированная несущая.
type carrierGen struct{}

func (carrierGen) fillClean(dst []complex64) {
	for i := range dst {
		dst[i] = 1
	}
}

// afskPRBSGen — непрерывный AFSK 1200 по битам PRBS-9.
type afskPRBSGen struct {
	mod     *modem.AFSK
	prbs    *modem.PRBS9
	levels  []byte
	modBuf  []complex64 // буфер модулятора, переиспользуется
	pending []complex64 // промодулировано, но ещё не выдано (часть modBuf)
}

func newAFSKPRBSGen(sampleRate float64) *afskPRBSGen {
	return &afskPRBSGen{
		mod:    modem.NewAFSK(modem.AFSK1200(sampleRate)),
		prbs:   modem.NewPRBS9(),
		levels: make([]byte, symbolChunk),
	}
}

func (g *afskPRBSGen) fillClean(dst []complex64) {
	i := 0
	for i < len(dst) {
		if len(g.pending) == 0 {
			for k := range g.levels {
				g.levels[k] = g.prbs.Next()
			}
			g.modBuf = g.mod.Modulate(g.modBuf[:0], g.levels)
			g.pending = g.modBuf
		}
		n := copy(dst[i:], g.pending)
		g.pending = g.pending[n:]
		i += n
	}
}
