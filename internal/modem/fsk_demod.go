package modem

import (
	"math"
	"math/cmplx"
)

const (
	// Параметры тактовой петли Gardner TED (как в gr-satellites symbol_sync_ff,
	// с прореживанием до <= 10 отсч./символ) и адаптивного порога решения.
	fskMaxSPS             = 10.0
	fskClkBW              = 0.035
	fskClkDamping         = 1.0
	fskClkTEDGain         = 1.47
	fskClkLimit           = 0.004
	fskTEDClamp           = 1.5
	fskLevelAttack        = 1.0 / 4
	fskLevelDecay         = 1.0 / 64
	fskSwingGain          = 1.0 / 256
	fskHistLen            = 10
	fskHistSpanMin        = 0.8
	fskBimodalMargin      = 0.15
	fskBimodalMinPerSide  = 2
	fskBimodalMinCross    = 1
	fskLevelOutlierMargin = 1.2
	// Общая константа для mfsk.go.
	fskClockGain = 0.3
)

// FSKDemod — гибрид: квадратурный дискриминатор + boxcar + адаптивный порог решения
// с бимодальной перепривязкой (нулевая задержка для коротких вспышек) +
// тактовая синхронизация Gardner TED с кубическим интерполятором (по gr-satellites).
type FSKDemod struct {
	prev     complex128
	havePrev bool
	norm     float64

	ring       []float64
	sum        float64
	pos        int
	decim      int
	decimPhase int

	high, low float64
	threshold float64
	frozen    bool
	yHist     [fskHistLen]float64
	histPos   int
	histCount int

	interp    [4]float64
	omega     float64
	omegaMin  float64
	omegaMax  float64
	alpha     float64
	beta      float64
	countdown float64
	wantMid   bool
	midSym    float64
	prevSym   float64
	haveSym   bool

	lastHigh    bool
	jitter      float64
	deviationHz float64
	swing       float64
}

// NewFSKDemod создаёт демодулятор; cfg.SampleRate — частота дискретизации канала.
func NewFSKDemod(cfg FSKConfig) *FSKDemod {
	rawSPS := cfg.SampleRate / cfg.Baud
	window := max(int(math.Round(rawSPS)), 1)
	decim := 1
	if rawSPS > fskMaxSPS {
		decim = int(math.Ceil(rawSPS / fskMaxSPS))
	}
	sps := rawSPS / float64(decim)

	denom := 1.0 + 2.0*fskClkDamping*fskClkBW + fskClkBW*fskClkBW
	alpha := (4.0 * fskClkDamping * fskClkBW) / (denom * fskClkTEDGain) * sps
	beta := (4.0 * fskClkBW * fskClkBW) / (denom * fskClkTEDGain) * sps

	return &FSKDemod{
		norm:        cfg.SampleRate / (twoPi * cfg.DeviationHz),
		ring:        make([]float64, window),
		decim:       decim,
		omega:       sps,
		omegaMin:    sps * (1.0 - fskClkLimit),
		omegaMax:    sps * (1.0 + fskClkLimit),
		alpha:       alpha,
		beta:        beta,
		countdown:   0.5 * sps,
		wantMid:     true,
		jitter:      jitterNoLock,
		deviationHz: cfg.DeviationHz,
	}
}

// SetFreeze замораживает или размораживает адаптацию порогов и тактовой петли.
func (d *FSKDemod) SetFreeze(freeze bool) { d.frozen = freeze }

// DeviationHz — принятая девиация по середине символа до сглаживания.
func (d *FSKDemod) DeviationHz() float64 {
	return d.swing * d.deviationHz
}

// ClockJitter — средняя ошибка фазы тактов на переходах уровня, в долях символа.
func (d *FSKDemod) ClockJitter() float64 { return d.jitter }

// Demodulate дописывает в dst принятые уровни линии и возвращает расширенный срез.
func (d *FSKDemod) Demodulate(dst []byte, iq []complex64) []byte {
	nWin := float64(len(d.ring))
	for _, s := range iq {
		x := complex128(s)
		if !d.havePrev {
			d.prev, d.havePrev = x, true
			continue
		}
		f := cmplx.Phase(x*cmplx.Conj(d.prev)) * d.norm
		d.prev = x

		d.sum += f - d.ring[d.pos]
		d.ring[d.pos] = f
		d.pos = (d.pos + 1) % len(d.ring)

		d.decimPhase++
		if d.decimPhase < d.decim {
			continue
		}
		d.decimPhase = 0

		y := d.sum / nWin
		d.interp[0], d.interp[1], d.interp[2], d.interp[3] = d.interp[1], d.interp[2], d.interp[3], y

		cur := d.interp[2] - d.threshold
		high := cur > 0
		if high != d.lastHigh {
			rem := d.countdown - 1.0
			phase := 1.0 - rem/d.omega
			if d.wantMid {
				phase = 0.5 - rem/d.omega
			}
			prev := d.interp[1] - d.threshold
			if denom := math.Abs(cur) + math.Abs(prev); denom > 0 {
				phase -= (math.Abs(cur) / denom) / d.omega
			}
			for phase < 0 {
				phase += 1.0
			}
			for phase >= 1 {
				phase -= 1.0
			}
			d.jitter = trackJitter(d.jitter, phase)
			d.lastHigh = high
		}

		d.countdown -= 1.0
		for d.countdown <= 0 {
			t := clampFloat(1.0+d.countdown, 0, 1)
			val := cubicInterp(d.interp, t)
			if d.wantMid {
				d.midSym = val
				d.wantMid = false
				d.countdown += 0.5 * d.omega
				continue
			}

			rawSym := val
			if d.frozen {
				d.histCount = 0
			} else {
				d.adaptLevels(rawSym)
			}

			sym := rawSym - d.threshold
			mid := d.midSym - d.threshold
			if d.haveSym && !d.frozen {
				amp := max(math.Abs(sym), math.Abs(d.prevSym), 1.0)
				tedErr := clampFloat((sym-d.prevSym)*mid/(amp*amp), -fskTEDClamp, fskTEDClamp)
				d.omega = clampFloat(d.omega-d.beta*tedErr, d.omegaMin, d.omegaMax)
				d.countdown -= d.alpha * tedErr
			}
			d.prevSym = sym
			d.haveSym = true

			midIdx := (d.pos + len(d.ring)/2) % len(d.ring)
			d.swing += fskSwingGain * (math.Abs(d.ring[midIdx]-d.threshold) - d.swing)

			bit := byte(0)
			if sym > 0 {
				bit = 1
			}
			dst = append(dst, bit)
			d.wantMid = true
			d.countdown += 0.5 * d.omega
		}
	}
	return dst
}

func (d *FSKDemod) adaptLevels(y float64) {
	d.yHist[d.histPos] = y
	d.histPos = (d.histPos + 1) % fskHistLen
	if d.histCount < fskHistLen {
		d.histCount++
	}

	d.high = trackLevel(d.high, y, y > d.high)
	d.low = trackLevel(d.low, y, y < d.low)
	d.threshold = (d.high + d.low) / 2

	if d.histCount < fskHistLen {
		return
	}
	lo, hi, ok := d.bimodalWindow()
	if !ok {
		return
	}
	margin := fskLevelOutlierMargin * (hi - lo)
	changed := false
	if d.high > hi+margin || d.high < lo {
		d.high = hi
		changed = true
	}
	if d.low < lo-margin || d.low > hi {
		d.low = lo
		changed = true
	}
	if changed {
		d.threshold = 0.5 * (d.high + d.low)
	}
}

func (d *FSKDemod) bimodalWindow() (float64, float64, bool) {
	start := d.histPos
	lo, hi := d.yHist[start], d.yHist[start]
	for i := 1; i < fskHistLen; i++ {
		v := d.yHist[(start+i)%fskHistLen]
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	span := hi - lo
	if span < fskHistSpanMin {
		return 0, 0, false
	}
	mid := 0.5 * (lo + hi)
	loCut := mid - fskBimodalMargin*span
	hiCut := mid + fskBimodalMargin*span
	var nLo, nHi, crosses int
	prevAbove := d.yHist[start] > mid
	for i := range fskHistLen {
		v := d.yHist[(start+i)%fskHistLen]
		if v <= loCut {
			nLo++
		} else if v >= hiCut {
			nHi++
		}
		above := v > mid
		if i > 0 && above != prevAbove {
			crosses++
			prevAbove = above
		}
	}
	if nLo < fskBimodalMinPerSide || nHi < fskBimodalMinPerSide || crosses < fskBimodalMinCross {
		return 0, 0, false
	}
	return lo, hi, true
}

func cubicInterp(h [4]float64, t float64) float64 {
	a0 := -0.5*h[0] + 1.5*h[1] - 1.5*h[2] + 0.5*h[3]
	a1 := h[0] - 2.5*h[1] + 2.0*h[2] - 0.5*h[3]
	a2 := -0.5*h[0] + 0.5*h[2]
	a3 := h[1]
	return ((a0*t+a1)*t+a2)*t + a3
}

func trackLevel(level, y float64, outside bool) float64 {
	if outside {
		return level + fskLevelAttack*(y-level)
	}
	return level + fskLevelDecay*(y-level)
}
