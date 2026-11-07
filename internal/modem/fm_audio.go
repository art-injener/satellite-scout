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

// FMAudioDemod — звуковой ЧМ-демодулятор: дискриминатор, деэмфазис и выход звука
// (ФНЧ и передискретизация в AudioRate). Остаток несущей даёт постоянную
// составляющую звука. Состояние сохраняется между вызовами Demodulate. Не потокобезопасен.
type FMAudioDemod struct {
	prev complex128
	norm float64 // радиан на отсчёт в долю девиации

	deemph, deemphA float64

	out audioOut
}

// NewFMAudioDemod создаёт демодулятор.
func NewFMAudioDemod(cfg FMAudioConfig) *FMAudioDemod {
	d := &FMAudioDemod{
		norm: cfg.SampleRate / (twoPi * cfg.DeviationHz),
		out:  newAudioOut(cfg.SampleRate, cfg.AudioRate),
	}
	if cfg.DeemphasisTau > 0 {
		d.deemphA = 1 - math.Exp(-1/(cfg.DeemphasisTau*cfg.SampleRate))
	}
	return d
}

// Demodulate дописывает в dst звук для отсчётов IQ и возвращает расширенный срез.
func (d *FMAudioDemod) Demodulate(dst []float32, iq []complex64) []float32 {
	for _, s := range iq {
		x := complex128(s)
		f := cmplx.Phase(x*cmplx.Conj(d.prev)) * d.norm
		d.prev = x
		if d.deemphA > 0 {
			d.deemph += d.deemphA * (f - d.deemph)
			f = d.deemph
		}
		dst = d.out.push(dst, f)
	}
	return dst
}

// audioOut — ФНЧ звука и передискретизация в частоту звука линейной интерполяцией:
// после ФНЧ звук многократно передискретизирован, поэтому её погрешность мала.
type audioOut struct {
	taps []float64
	hist []float64 // история ФНЧ дважды подряд: окно без деления по модулю
	hp   int

	step  float64 // входных отсчётов на выходной
	pos   float64 // положение следующего выходного отсчёта после prevY, доли входного
	prevY float64
}

func newAudioOut(inRate, audioRate float64) audioOut {
	// Полоса звука до 0,4 от выходной частоты, переход 0,1 от неё:
	// у окна Блэкмана-Харриса переход около 4·fs/N.
	n := int(math.Ceil(4*inRate/(0.1*audioRate))) | 1
	taps := dsp.DesignLowpass(0.4*audioRate, inRate, n, dsp.WindowBlackmanHarris)
	return audioOut{taps: taps, hist: make([]float64, 2*len(taps)), step: inRate / audioRate}
}

// push пропускает входной отсчёт x и дописывает в dst готовые отсчёты звука.
func (a *audioOut) push(dst []float32, x float64) []float32 {
	n := len(a.taps)
	a.hist[a.hp], a.hist[a.hp+n] = x, x
	a.hp = (a.hp + 1) % n
	y := 0.0
	for k, t := range a.taps {
		y += t * a.hist[a.hp+k]
	}
	for a.pos < 1 {
		dst = append(dst, float32(a.prevY+(y-a.prevY)*a.pos))
		a.pos += a.step
	}
	a.pos--
	a.prevY = y
	return dst
}
