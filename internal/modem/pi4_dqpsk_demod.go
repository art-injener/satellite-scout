package modem

import (
	"math"
	"math/cmplx"
)

// PI4DQPSKDemod — приём π/4-DQPSK: один отсчёт на символ (фаза тактов),
// дифференц. детектирование. Выход — биты 0/1.
type PI4DQPSKDemod struct {
	clock     float64
	clockStep float64
	jitter    float64

	prev     complex128
	havePrev bool
	lastPh   float64
	havePh   bool
}

// NewPI4DQPSKDemod создаёт демодулятор.
func NewPI4DQPSKDemod(cfg PI4DQPSKConfig) *PI4DQPSKDemod {
	baud := PI4DQPSKBaudOrDefault(cfg.Baud)
	return &PI4DQPSKDemod{
		clock:     0.5,
		clockStep: baud / cfg.SampleRate,
		jitter:    jitterNoLock,
	}
}

// ClockJitter — ошибка фазы тактов, доли символа.
func (d *PI4DQPSKDemod) ClockJitter() float64 { return d.jitter }

// Demodulate дописывает биты (по два на символ) в dst.
func (d *PI4DQPSKDemod) Demodulate(dst []byte, iq []complex64) []byte {
	for _, s := range iq {
		x := complex128(s)
		d.clock += d.clockStep
		if d.clock < 1 {
			continue
		}
		d.clock--

		if !d.havePrev {
			d.prev, d.havePrev = x, true
			continue
		}
		diff := x * cmplx.Conj(d.prev)
		d.prev = x
		ph := cmplx.Phase(diff)
		if d.havePh {
			// Смена сектора дифф. фазы — для меры джиттера (захват тактов на чистом сигнале).
			if math.Abs(angleNear(ph, d.lastPh)) > math.Pi/6 {
				d.jitter = trackJitter(d.jitter, d.clock)
			}
		}
		d.lastPh, d.havePh = ph, true
		b0, b1 := pi4DecodeDiff(diff)
		dst = append(dst, b0, b1)
	}
	return dst
}

func pi4DecodeDiff(z complex128) (b0, b1 byte) {
	ph := cmplx.Phase(z)
	type cand struct {
		ph     float64
		b0, b1 byte
	}
	cands := []cand{
		{math.Pi / 4, 0, 0},
		{3 * math.Pi / 4, 0, 1},
		{-3 * math.Pi / 4, 1, 1},
		{-math.Pi / 4, 1, 0},
	}
	best, bestD := 0, math.Abs(angleNear(ph, cands[0].ph))
	for i := 1; i < len(cands); i++ {
		if d := math.Abs(angleNear(ph, cands[i].ph)); d < bestD {
			best, bestD = i, d
		}
	}
	return cands[best].b0, cands[best].b1
}

func angleNear(a, b float64) float64 {
	return math.Mod(a-b+math.Pi, twoPi) - math.Pi
}
