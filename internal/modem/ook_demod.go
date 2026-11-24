package modem

import (
	"math"
	"math/cmplx"
)

const (
	// DefaultOOKBaud — символьная скорость профиля ook, если в канале baud не задан.
	// Середина типичного ряда метеозондов ~400–500 бод.
	DefaultOOKBaud = 480.0

	// ookClockGain — доля ошибки фазы символа, снимаемая на каждом переходе огибающей.
	ookClockGain = 0.3
)

// OOKBaudOrDefault возвращает baud канала или DefaultOOKBaud при нуле и ниже.
func OOKBaudOrDefault(baud float64) float64 {
	if baud > 0 {
		return baud
	}
	return DefaultOOKBaud
}

// OOKDemod — демодулятор амплитудной манипуляции: огибающая |IQ|, усреднение по длине
// символа, адаптивный порог mark/space и ФАПЧ тактов по переходам уровня.
// На входе — IQ уже выделенного канала, на выходе — уровни линии 0/1.
// Состояние сохраняется между вызовами Demodulate. Не потокобезопасен.
type OOKDemod struct {
	ring []float64 // огибающая в окне усреднения
	sum  float64
	pos  int

	high, low float64 // уровни «есть» и «нет»
	threshold float64

	clock     float64
	clockStep float64
	lastHigh  bool
	haveLevel bool
	jitter    float64
}

// NewOOKDemod создаёт демодулятор; cfg.SampleRate — частота дискретизации канала.
func NewOOKDemod(cfg OOKConfig) *OOKDemod {
	baud := OOKBaudOrDefault(cfg.Baud)
	window := max(int(math.Round(cfg.SampleRate/baud)), 1)
	return &OOKDemod{
		ring:      make([]float64, window),
		clockStep: baud / cfg.SampleRate,
		jitter:    jitterNoLock,
	}
}

// ClockJitter — средняя ошибка фазы тактов на переходах уровня, в долях символа.
func (d *OOKDemod) ClockJitter() float64 { return d.jitter }

// Demodulate дописывает в dst принятые уровни линии и возвращает расширенный срез.
func (d *OOKDemod) Demodulate(dst []byte, iq []complex64) []byte {
	for _, s := range iq {
		env := cmplx.Abs(complex128(s))
		d.sum += env - d.ring[d.pos]
		d.ring[d.pos] = env
		d.pos = (d.pos + 1) % len(d.ring)
		y := d.sum / float64(len(d.ring))

		if !d.haveLevel {
			// Первое окно: порог посередине текущей огибающей, чтобы не залипнуть на нуле.
			d.high, d.low, d.threshold = y, y, y
			d.haveLevel = true
		}

		high := y > d.threshold
		if high != d.lastHigh {
			d.jitter = trackJitter(d.jitter, d.clock)
			d.clock += ookClockGain * (0.5 - d.clock)
			d.lastHigh = high
		}

		d.clock += d.clockStep
		if d.clock < 1 {
			continue
		}
		d.clock--

		d.high = trackLevel(d.high, y, y > d.high)
		d.low = trackLevel(d.low, y, y < d.low)
		d.threshold = (d.high + d.low) / 2
		bit := byte(0)
		if high {
			bit = 1
		}
		dst = append(dst, bit)
	}
	return dst
}
