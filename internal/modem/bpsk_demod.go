package modem

import (
	"math"
	"math/cmplx"
)

const (
	bpskClockGain  = 0.3
	bpskPwrAlpha   = 0.05
	bpskMaxNCOFreq = 0.5
	bpskMinPwr     = 1e-18
)

// BPSKDemod — дифференциальный приём BPSK: усреднение по символу, ФАПЧ тактов по
// смене знака I после Costas-подобной подстройки фазы на два состояния, решение
// по Re(s·conj(prev)). На входе — IQ канала, на выходе — уровни линии 0/1.
// Состояние сохраняется между вызовами. Не потокобезопасен.
type BPSKDemod struct {
	ncoPhase float64
	ncoFreq  float64 // остаток несущей, рад/отсчёт
	avgPwr   float64 // сглаженная мощность для нормировки фазового детектора
	prevSym  complex128

	ring []complex128
	sum  complex128
	pos  int

	clock     float64
	clockStep float64
	lastSign  bool
	jitter    float64
	haveSym   bool
}

// NewBPSKDemod создаёт демодулятор; cfg.SampleRate — частота дискретизации канала.
func NewBPSKDemod(cfg BPSKConfig) *BPSKDemod {
	window := max(int(math.Round(cfg.SampleRate/cfg.Baud)), 1)
	return &BPSKDemod{
		ring:      make([]complex128, window),
		clockStep: cfg.Baud / cfg.SampleRate,
		jitter:    jitterNoLock,
	}
}

// ClockJitter — средняя ошибка фазы тактов на переходах, в долях символа.
func (d *BPSKDemod) ClockJitter() float64 { return d.jitter }

// Demodulate дописывает в dst принятые уровни линии и возвращает расширенный срез.
func (d *BPSKDemod) Demodulate(dst []byte, iq []complex64) []byte {
	for _, s := range iq {
		x := complex128(s)
		// Подстройка фазы: нормированная ошибка Costas 2-го порядка, err = Re(y)·Im(y) / |y|².
		sin, cos := math.Sincos(d.ncoPhase)
		y := x * complex(cos, -sin)
		mag2 := real(y)*real(y) + imag(y)*imag(y)
		if d.avgPwr == 0 {
			d.avgPwr = mag2
		} else {
			d.avgPwr += bpskPwrAlpha * (mag2 - d.avgPwr)
		}
		err := 0.0
		if den := max(mag2, 0.25*d.avgPwr); den > bpskMinPwr {
			err = (real(y) * imag(y)) / den
		}
		d.ncoFreq = clampFloat(d.ncoFreq+0.001*err, -bpskMaxNCOFreq, bpskMaxNCOFreq)
		d.ncoPhase = math.Mod(d.ncoPhase+d.ncoFreq+0.05*err, 2*math.Pi)

		d.sum += y - d.ring[d.pos]
		d.ring[d.pos] = y
		d.pos = (d.pos + 1) % len(d.ring)
		sym := d.sum / complex(float64(len(d.ring)), 0)

		sign := real(sym) >= 0
		if sign != d.lastSign {
			d.jitter = trackJitter(d.jitter, d.clock)
			d.clock += bpskClockGain * (0.5 - d.clock)
			d.lastSign = sign
		}

		d.clock += d.clockStep
		if d.clock < 1 {
			continue
		}
		d.clock--

		if !d.haveSym {
			d.prevSym, d.haveSym = sym, true
			continue
		}
		// Дифференциальное решение: смена фазы — уровень 1, удержание — 0.
		bit := byte(0)
		if real(sym*cmplx.Conj(d.prevSym)) < 0 {
			bit = 1
		}
		d.prevSym = sym
		dst = append(dst, bit)
	}
	return dst
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
