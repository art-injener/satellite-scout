package dsp

import "math"

// WindowType — какое окно применить к IQ-отсчётам перед FFT.
type WindowType uint8

const (
	WindowHann WindowType = iota + 1
	WindowBlackmanHarris
)

// MakeWindowCoeffs возвращает массив весов оконной функции длины n.
// Используется периодическая форма (делитель N) — она корректна для FFT.
func MakeWindowCoeffs(n int, wt WindowType) []float64 {
	c := make([]float64, n)
	nf := float64(n)

	switch wt {
	case WindowHann:
		// w[k] = 0.5 · (1 − cos(2πk/N))
		for k := 0; k < n; k++ {
			c[k] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(k)/nf))
		}

	case WindowBlackmanHarris:
		// 4-членная формула: a0 − a1·cos(2πk/N) + a2·cos(4πk/N) − a3·cos(6πk/N)
		const (
			a0 = 0.35875
			a1 = 0.48829
			a2 = 0.14128
			a3 = 0.01168
		)
		for k := 0; k < n; k++ {
			x := 2.0 * math.Pi * float64(k) / nf
			c[k] = a0 - a1*math.Cos(x) + a2*math.Cos(2.0*x) - a3*math.Cos(3.0*x)
		}

	default:
		panic("dsp: unknown WindowType")
	}
	return c
}

// ApplyWindow домножает каждый IQ-отсчёт на соответствующий вес окна.
// Меняет samples на месте. Длины должны совпадать.
func ApplyWindow(samples []complex64, coeffs []float64) {
	if len(samples) != len(coeffs) {
		panic("dsp: ApplyWindow: len(samples) != len(coeffs)")
	}
	for i, c := range coeffs {
		samples[i] = complex64(complex128(samples[i]) * complex(c, 0))
	}
}
