package dsp

import (
	"math"

	"gonum.org/v1/gonum/dsp/fourier"
)

// DBFloor — минимальное значение dBFS. Всё что тише — обрезаем до этого порога.
const DBFloor float32 = -200.0

// Spectrum превращает блок IQ-отсчётов в массив мощностей (спектр).
// Буферы переиспользуются, вызывать из одной горутины.
type Spectrum struct {
	size   int
	fft    *fourier.CmplxFFT
	buf    []complex128 // IQ в формате gonum (complex128)
	coeffs []complex128 // коэффициенты после FFT
	out    []float64    // готовый спектр мощности
}

// NewSpectrum создаёт вычислитель на фиксированный размер FFT.
func NewSpectrum(size int) *Spectrum {
	return &Spectrum{
		size:   size,
		fft:    fourier.NewCmplxFFT(size),
		buf:    make([]complex128, size),
		coeffs: make([]complex128, size),
		out:    make([]float64, size),
	}
}

// PowerSpectrum считает спектр мощности: IQ → FFT → |X|²/N².
// Результат в линейном масштабе, DC-составляющая посередине массива.
// Возвращённый слайс перезаписывается при следующем вызове.
func (s *Spectrum) PowerSpectrum(samples []complex64) []float64 {
	n := s.size
	for i := 0; i < n; i++ {
		s.buf[i] = complex128(samples[i])
	}

	s.coeffs = s.fft.Coefficients(s.coeffs, s.buf)

	invN2 := 1.0 / (float64(n) * float64(n))
	half := n / 2

	for i := 0; i < n; i++ {
		c := s.coeffs[i]
		re, im := real(c), imag(c)
		s.out[(i+half)%n] = (re*re + im*im) * invN2
	}
	return s.out
}

// PowerToDBFS переводит линейные мощности в децибелы (dBFS).
// Нули и отрицательные значения заменяются на DBFloor.
func PowerToDBFS(power []float64) []float32 {
	out := make([]float32, len(power))
	for i, p := range power {
		if p <= 0 {
			out[i] = DBFloor
			continue
		}
		db := float32(10.0 * math.Log10(p))
		if db < DBFloor {
			db = DBFloor
		}
		out[i] = db
	}
	return out
}
