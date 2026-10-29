package modem

import (
	"math"
	"math/cmplx"

	"github.com/art-injener/satellite-scout/internal/dsp"
)

// FMConfig — узкополосная ЧМ со звуком.
type FMConfig struct {
	SampleRate  float64 // частота дискретизации IQ, Гц
	DeviationHz float64 // девиация при звуке ±1
}

// FM — модулятор звука в ЧМ с непрерывной фазой между вызовами Modulate.
// Звук идёт с частотой отсчётов IQ: передискретизация — забота вызывающего.
type FM struct {
	gain  float64
	phase float64
}

// NewFM создаёт модулятор.
func NewFM(cfg FMConfig) *FM {
	return &FM{gain: twoPi * cfg.DeviationHz / cfg.SampleRate}
}

// Modulate дописывает в dst отсчёты IQ для звука audio и возвращает расширенный срез.
func (m *FM) Modulate(dst []complex64, audio []float32) []complex64 {
	for _, a := range audio {
		m.phase = math.Mod(m.phase+m.gain*float64(a), twoPi)
		sin, cos := math.Sincos(m.phase)
		dst = append(dst, complex(float32(cos), float32(sin)))
	}
	return dst
}

// FMAudioConfig — параметры звукового ЧМ-демодулятора.
type FMAudioConfig struct {
	SampleRate  float64 // частота отсчётов IQ канала, Гц
	DeviationHz float64 // девиация, которой соответствует звук ±1
	AudioRate   float64 // частота отсчётов звука на выходе, Гц
	// DeemphasisTau — постоянная времени деэмфазиса, с; 0 — без него. У узкополосной
	// ЧМ любительских станций обычно 530 мкс (излом около 300 Гц).
	DeemphasisTau float64
}

// FMAudioDemod — звуковой ЧМ-демодулятор: дискриминатор, деэмфазис, ФНЧ звука и
// передискретизация в AudioRate линейной интерполяцией (после ФНЧ звук многократно
// передискретизирован, поэтому её погрешность мала). Остаток несущей даёт постоянную
// составляющую звука. Состояние сохраняется между вызовами Demodulate. Не потокобезопасен.
type FMAudioDemod struct {
	prev complex128
	norm float64 // радиан на отсчёт в долю девиации

	deemph, deemphA float64

	taps []float64
	hist []float64 // история ФНЧ дважды подряд: окно без деления по модулю
	hp   int

	step  float64 // входных отсчётов на выходной
	pos   float64 // положение следующего выходного отсчёта после prevY, доли входного
	prevY float64
}

// NewFMAudioDemod создаёт демодулятор.
func NewFMAudioDemod(cfg FMAudioConfig) *FMAudioDemod {
	// Полоса звука до 0,4 от выходной частоты, переход 0,1 от неё:
	// у окна Блэкмана-Харриса переход около 4·fs/N.
	n := int(math.Ceil(4*cfg.SampleRate/(0.1*cfg.AudioRate))) | 1
	d := &FMAudioDemod{
		norm: cfg.SampleRate / (twoPi * cfg.DeviationHz),
		taps: dsp.DesignLowpass(0.4*cfg.AudioRate, cfg.SampleRate, n, dsp.WindowBlackmanHarris),
		step: cfg.SampleRate / cfg.AudioRate,
	}
	d.hist = make([]float64, 2*len(d.taps))
	if cfg.DeemphasisTau > 0 {
		d.deemphA = 1 - math.Exp(-1/(cfg.DeemphasisTau*cfg.SampleRate))
	}
	return d
}

// Demodulate дописывает в dst звук для отсчётов IQ и возвращает расширенный срез.
func (d *FMAudioDemod) Demodulate(dst []float32, iq []complex64) []float32 {
	n := len(d.taps)
	for _, s := range iq {
		x := complex128(s)
		f := cmplx.Phase(x*cmplx.Conj(d.prev)) * d.norm
		d.prev = x
		if d.deemphA > 0 {
			d.deemph += d.deemphA * (f - d.deemph)
			f = d.deemph
		}

		d.hist[d.hp], d.hist[d.hp+n] = f, f
		d.hp = (d.hp + 1) % n
		y := 0.0
		for k, t := range d.taps {
			y += t * d.hist[d.hp+k]
		}

		for d.pos < 1 {
			dst = append(dst, float32(d.prevY+(y-d.prevY)*d.pos))
			d.pos += d.step
		}
		d.pos--
		d.prevY = y
	}
	return dst
}
