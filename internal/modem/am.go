package modem

import (
	"math/cmplx"
)

// AMConfig — амплитудная модуляция с несущей (DSB-AM) для голоса и маяка.
type AMConfig struct {
	SampleRate float64 // частота дискретизации IQ, Гц
	// Depth — глубина модуляции: звук ±1 даёт огибающую 1±Depth; 0…1.
	Depth float64
}

// AM — модулятор: огибающая 1+Depth·audio на несущей (I, Q=0). Состояние не нужно.
type AM struct {
	depth float64
}

// NewAM создаёт модулятор.
func NewAM(cfg AMConfig) *AM {
	d := cfg.Depth
	if d < 0 {
		d = 0
	}
	if d > 1 {
		d = 1
	}
	return &AM{depth: d}
}

// Modulate дописывает в dst отсчёты IQ для звука audio и возвращает расширенный срез.
// Звук на частоте отсчётов IQ; передискретизация — забота вызывающего.
func (m *AM) Modulate(dst []complex64, audio []float32) []complex64 {
	for _, a := range audio {
		amp := 1 + m.depth*float64(a)
		if amp < 0 {
			amp = 0
		}
		dst = append(dst, complex(float32(amp), 0))
	}
	return dst
}

// AMAudioConfig — параметры звукового АМ-демодулятора.
type AMAudioConfig struct {
	SampleRate float64 // частота отсчётов IQ канала, Гц
	AudioRate  float64 // частота отсчётов звука на выходе, Гц
}

// AMAudioDemod — демодулятор АМ: огибающая |IQ|, вычитание постоянной составляющей,
// выход звука (ФНЧ и передискретизация в AudioRate). Состояние сохраняется между
// вызовами Demodulate. Не потокобезопасен.
type AMAudioDemod struct {
	dc   float64 // оценка несущей (средняя огибающая)
	have bool
	out  audioOut
}

// NewAMAudioDemod создаёт демодулятор.
func NewAMAudioDemod(cfg AMAudioConfig) *AMAudioDemod {
	return &AMAudioDemod{out: newAudioOut(cfg.SampleRate, cfg.AudioRate)}
}

// amDCAttack — постоянная времени подхвата DC: около 20 мс на типичных 32 кГц.
const amDCAttack = 1.0 / 640

// Demodulate дописывает в dst звук для отсчётов IQ и возвращает расширенный срез.
func (d *AMAudioDemod) Demodulate(dst []float32, iq []complex64) []float32 {
	for _, s := range iq {
		env := cmplx.Abs(complex128(s))
		if !d.have {
			d.dc, d.have = env, true
		} else {
			d.dc += amDCAttack * (env - d.dc)
		}
		dst = d.out.push(dst, env-d.dc)
	}
	return dst
}

// DefaultAMDepth — типичная глубина АМ маяка/голоса (~80 %).
const DefaultAMDepth = 0.8

// AMVoice — пресет голоса/маяка АМ на частоте канала.
func AMVoice(sampleRate float64) AMConfig {
	return AMConfig{SampleRate: sampleRate, Depth: DefaultAMDepth}
}
