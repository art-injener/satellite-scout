package modem

import "math"

const (
	qpskClockGain  = 0.3
	qpskPwrAlpha   = 0.05
	qpskMaxNCOFreq = 0.5
	qpskMinPwr     = 1e-18
)

// QPSKDemod — некогерентный по тактам приём QPSK: усреднение по символу, ФАПЧ тактов,
// Costas 4-го порядка, два бита Gray на символ. На выходе — поток бит без кадров.
type QPSKDemod struct {
	ncoPhase float64
	ncoFreq  float64
	avgPwr   float64 // сглаженная мощность для нормировки фазового детектора

	ring []complex128
	sum  complex128
	pos  int

	clock     float64
	clockStep float64
	lastQuad  int
	jitter    float64
}

// NewQPSKDemod создаёт демодулятор.
func NewQPSKDemod(cfg QPSKConfig) *QPSKDemod {
	window := max(int(math.Round(cfg.SampleRate/cfg.Baud)), 1)
	return &QPSKDemod{
		ring:      make([]complex128, window),
		clockStep: cfg.Baud / cfg.SampleRate,
		jitter:    jitterNoLock,
	}
}

// ClockJitter — ошибка фазы тактов, доли символа.
func (d *QPSKDemod) ClockJitter() float64 { return d.jitter }

// Demodulate дописывает биты (по два на символ) в dst.
func (d *QPSKDemod) Demodulate(dst []byte, iq []complex64) []byte {
	for _, s := range iq {
		x := complex128(s)
		sin, cos := math.Sincos(d.ncoPhase)
		y := x * complex(cos, -sin)
		// Нормированная ошибка Costas для QPSK: err = (sign(Re)·Im − sign(Im)·Re) / |y|.
		re, im := real(y), imag(y)
		mag2 := re*re + im*im
		if d.avgPwr == 0 {
			d.avgPwr = mag2
		} else {
			d.avgPwr += qpskPwrAlpha * (mag2 - d.avgPwr)
		}
		err := 0.0
		if den := max(mag2, 0.25*d.avgPwr); den > qpskMinPwr {
			err = (math.Copysign(1, re)*im - math.Copysign(1, im)*re) / math.Sqrt(den)
		}
		d.ncoFreq = clampFloat(d.ncoFreq+0.0005*err, -qpskMaxNCOFreq, qpskMaxNCOFreq)
		d.ncoPhase = math.Mod(d.ncoPhase+d.ncoFreq+0.02*err, 2*math.Pi)

		d.sum += y - d.ring[d.pos]
		d.ring[d.pos] = y
		d.pos = (d.pos + 1) % len(d.ring)
		sym := d.sum / complex(float64(len(d.ring)), 0)

		quad := qpskQuadrant(real(sym), imag(sym))
		if quad != d.lastQuad {
			d.jitter = trackJitter(d.jitter, d.clock)
			d.clock += qpskClockGain * (0.5 - d.clock)
			d.lastQuad = quad
		}

		d.clock += d.clockStep
		if d.clock < 1 {
			continue
		}
		d.clock--

		b0, b1 := byte(0), byte(0)
		if real(sym) < 0 {
			b0 = 1
		}
		if imag(sym) < 0 {
			b1 = 1
		}
		dst = append(dst, b0, b1)
	}
	return dst
}

func qpskQuadrant(re, im float64) int {
	switch {
	case re >= 0 && im >= 0:
		return 0
	case re < 0 && im >= 0:
		return 1
	case re < 0 && im < 0:
		return 2
	default:
		return 3
	}
}
