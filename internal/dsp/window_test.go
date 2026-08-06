package dsp

import (
	"math"
	"testing"
)

func TestMakeWindowCoeffs_Hann_Size(t *testing.T) {
	for _, n := range []int{64, 128, 512, 1024} {
		c := MakeWindowCoeffs(n, WindowHann)
		if len(c) != n {
			t.Errorf("Hann(%d): len=%d, want %d", n, len(c), n)
		}
	}
}

func TestMakeWindowCoeffs_Hann_Endpoints(t *testing.T) {
	c := MakeWindowCoeffs(256, WindowHann)

	if c[0] != 0 {
		t.Errorf("Hann[0]=%g, want 0", c[0])
	}
	// Периодическое окно: w[N-1] ≈ 0 (но не точно)
	if c[255] > 1e-3 {
		t.Errorf("Hann[N-1]=%g, want <1e-3", c[255])
	}
}

func TestMakeWindowCoeffs_Hann_Midpoint(t *testing.T) {
	c := MakeWindowCoeffs(256, WindowHann)

	// Периодическое окно Хэнна: w[N/2] = 1.0 (точно для чётного N)
	if c[128] != 1.0 {
		t.Errorf("Hann[N/2]=%g, want 1.0", c[128])
	}
}

func TestMakeWindowCoeffs_BlackmanHarris_Size(t *testing.T) {
	c := MakeWindowCoeffs(512, WindowBlackmanHarris)
	if len(c) != 512 {
		t.Errorf("BH(512): len=%d", len(c))
	}
}

func TestMakeWindowCoeffs_BlackmanHarris_Endpoints(t *testing.T) {
	c := MakeWindowCoeffs(256, WindowBlackmanHarris)

	// BH: w[0] = a0 - a1 + a2 - a3 ≈ 0.00006
	if c[0] > 1e-4 {
		t.Errorf("BH[0]=%g, want <1e-4", c[0])
	}
}

func TestMakeWindowCoeffs_BlackmanHarris_Midpoint(t *testing.T) {
	c := MakeWindowCoeffs(256, WindowBlackmanHarris)

	// Периодическое BH: w[N/2] = a0 + a1 + a2 + a3 = 1.0
	if math.Abs(c[128]-1.0) > 1e-10 {
		t.Errorf("BH[N/2]=%g, want 1.0", c[128])
	}
}

func TestMakeWindowCoeffs_InvalidType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid WindowType")
		}
	}()
	MakeWindowCoeffs(64, WindowType(99))
}

func TestApplyWindow(t *testing.T) {
	coeffs := []float64{0.0, 0.5, 1.0, 0.5}
	samples := []complex64{1 + 1i, 2 + 2i, 3 + 3i, 4 + 4i}

	ApplyWindow(samples, coeffs)

	want := []complex64{0, 1 + 1i, 3 + 3i, 2 + 2i}
	for i, got := range samples {
		if got != want[i] {
			t.Errorf("[%d] got %v, want %v", i, got, want[i])
		}
	}
}

func TestApplyWindow_PanicOnMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on size mismatch")
		}
	}()
	ApplyWindow(make([]complex64, 4), make([]float64, 3))
}
