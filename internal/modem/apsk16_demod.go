package modem

import "math"

// APSK16Demod — приём 16-APSK: DD-ФАПЧ несущей на каждом отсчёте, решение
// в середине символа по ближайшей точке. Выход — 4 бита на символ.
type APSK16Demod struct {
	ncoPhase float64
	ncoFreq  float64

	clock     float64
	clockStep float64
	jitter    float64
	lastIdx   int
}

// NewAPSK16Demod создаёт демодулятор.
func NewAPSK16Demod(cfg APSK16Config) *APSK16Demod {
	baud := APSK16BaudOrDefault(cfg.Baud)
	return &APSK16Demod{
		clock:     0.5,
		clockStep: baud / cfg.SampleRate,
		jitter:    jitterNoLock,
	}
}

// ClockJitter — ошибка фазы тактов, доли символа.
func (d *APSK16Demod) ClockJitter() float64 { return d.jitter }

// Demodulate дописывает биты (по четыре на символ) в dst.
func (d *APSK16Demod) Demodulate(dst []byte, iq []complex64) []byte {
	for _, s := range iq {
		x := complex128(s)
		sin, cos := math.Sincos(d.ncoPhase)
		y := x * complex(cos, -sin)

		idx := apsk16Nearest(y)
		ref := apsk16Points[idx]
		// Ошибка фазы: Im(y · conj(ref)).
		err := imag(y * complex(real(ref), -imag(ref)))
		d.ncoFreq += 0.001 * err
		d.ncoPhase = math.Mod(d.ncoPhase+d.ncoFreq+0.05*err, twoPi)

		d.clock += d.clockStep
		if d.clock < 1 {
			continue
		}
		d.clock--

		if idx != d.lastIdx {
			d.jitter = trackJitter(d.jitter, d.clock)
			d.lastIdx = idx
		}
		b0, b1, b2, b3 := apsk16IndexBits(idx)
		dst = append(dst, b0, b1, b2, b3)
	}
	return dst
}
