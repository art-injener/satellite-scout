package dsp

import (
	"math"
	"testing"
)

func TestSpectrum_PureTone(t *testing.T) {
	const N = 64
	const binIdx = 3

	samples := makeTone(N, binIdx)
	s := NewSpectrum(N)
	power := s.PowerSpectrum(samples)

	if len(power) != N {
		t.Fatalf("len=%d, want %d", len(power), N)
	}

	// После fftshift: bin k → позиция (k + N/2) % N
	peakPos := (binIdx + N/2) % N

	// Нормализованная мощность чистого тона = 1.0
	if math.Abs(power[peakPos]-1.0) > 1e-6 {
		t.Errorf("peak power[%d]=%g, want ~1.0", peakPos, power[peakPos])
	}

	// Все остальные бины — практически 0
	for i, p := range power {
		if i == peakPos {
			continue
		}
		if p > 1e-10 {
			t.Errorf("non-peak bin[%d]=%g, want <1e-10", i, p)
		}
	}
}

func TestSpectrum_DC(t *testing.T) {
	const N = 128
	samples := make([]complex64, N)
	for i := range samples {
		samples[i] = 1 + 0i
	}

	s := NewSpectrum(N)
	power := s.PowerSpectrum(samples)

	// DC (bin 0) после fftshift → позиция N/2
	dcPos := N / 2
	if math.Abs(power[dcPos]-1.0) > 1e-6 {
		t.Errorf("DC power[%d]=%g, want ~1.0", dcPos, power[dcPos])
	}
}

func TestSpectrum_NegativeFrequency(t *testing.T) {
	const N = 64
	const binIdx = N - 5 // отрицательная частота, bin 59

	samples := makeTone(N, binIdx)
	s := NewSpectrum(N)
	power := s.PowerSpectrum(samples)

	// fftshift: (59 + 32) % 64 = 27
	peakPos := (binIdx + N/2) % N
	if math.Abs(power[peakPos]-1.0) > 1e-6 {
		t.Errorf("neg freq peak[%d]=%g, want ~1.0", peakPos, power[peakPos])
	}
}

func TestSpectrum_AllZeros(t *testing.T) {
	const N = 64
	samples := make([]complex64, N)
	s := NewSpectrum(N)
	power := s.PowerSpectrum(samples)

	for i, p := range power {
		if p != 0 {
			t.Errorf("zero input bin[%d]=%g, want 0", i, p)
		}
	}
}

func TestPowerToDBFS(t *testing.T) {
	tests := []struct {
		in   float64
		want float32
	}{
		{1.0, 0},
		{0.1, -10},
		{0.01, -20},
		{0, DBFloor},
	}
	for _, tc := range tests {
		got := PowerToDBFS([]float64{tc.in})
		if math.Abs(float64(got[0]-tc.want)) > 0.01 {
			t.Errorf("PowerToDBFS(%g)=%g, want %g", tc.in, got[0], tc.want)
		}
	}
}

// makeTone генерирует комплексную экспоненту на частоте бина binIdx.
func makeTone(n, binIdx int) []complex64 {
	samples := make([]complex64, n)
	for i := range samples {
		phase := 2.0 * math.Pi * float64(binIdx) * float64(i) / float64(n)
		samples[i] = complex64(complex(math.Cos(phase), math.Sin(phase)))
	}
	return samples
}
