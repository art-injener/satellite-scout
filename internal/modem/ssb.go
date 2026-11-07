package modem

import (
	"math"
)

const (
	// ssbAGCTarget — пик звука, к которому АРУ приводит сигнал любого уровня.
	ssbAGCTarget = 0.6
	// ssbAGCRelease — постоянная спада АРУ, с: громкость не прыгает между слогами.
	ssbAGCRelease = 0.5
	// ssbAGCFloor — нижняя граница огибающей: тишина не делится на ноль.
	ssbAGCFloor = 1e-9
)

// SSBConfig — параметры демодулятора однополосного сигнала.
type SSBConfig struct {
	SampleRate float64 // частота отсчётов IQ канала, Гц
	AudioRate  float64 // частота отсчётов звука на выходе, Гц
	// ShiftHz — где середина боковой полосы в звуке: фильтр канала оставил её на нуле.
	// Плюс — верхняя боковая, минус — нижняя (её спектр в звуке зеркален).
	ShiftHz float64
}

// SSBDemod — демодулятор SSB: боковая полоса, вырезанная фильтром канала вокруг нуля,
// сдвигается на своё место в звуке, вещественная часть идёт в выход звука, АРУ по пику
// с мгновенной атакой и медленным спадом выравнивает громкость.
// Состояние сохраняется между вызовами Demodulate. Не потокобезопасен.
type SSBDemod struct {
	phase, step float64
	out         audioOut

	env, release float64
}

// NewSSBDemod создаёт демодулятор.
func NewSSBDemod(cfg SSBConfig) *SSBDemod {
	return &SSBDemod{
		step:    twoPi * cfg.ShiftHz / cfg.SampleRate,
		out:     newAudioOut(cfg.SampleRate, cfg.AudioRate),
		release: math.Exp(-1 / (ssbAGCRelease * cfg.AudioRate)),
	}
}

// Demodulate дописывает в dst звук для отсчётов IQ и возвращает расширенный срез.
func (d *SSBDemod) Demodulate(dst []float32, iq []complex64) []float32 {
	start := len(dst)
	for _, s := range iq {
		sin, cos := math.Sincos(d.phase)
		d.phase = math.Mod(d.phase+d.step, twoPi)
		dst = d.out.push(dst, float64(real(s))*cos-float64(imag(s))*sin)
	}
	for i := start; i < len(dst); i++ {
		y := float64(dst[i])
		d.env = max(math.Abs(y), d.env*d.release, ssbAGCFloor)
		dst[i] = float32(ssbAGCTarget * y / d.env)
	}
	return dst
}
