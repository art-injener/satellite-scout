package dsp

import (
	"math"
	"testing"
)

func TestReducer_Current(t *testing.T) {
	r := NewReducer(3, ModeCurrent)
	r.Push([]float64{1.0, 0.1, 0.01})
	r.Push([]float64{0.5, 0.5, 0.5})

	got := r.Flush()
	// Current → dBFS последнего кадра
	want := PowerToDBFS([]float64{0.5, 0.5, 0.5})
	assertFloat32Slice(t, "Current", got, want, 0.01)
}

func TestReducer_Avg(t *testing.T) {
	r := NewReducer(2, ModeAvg)
	r.Push([]float64{1.0, 0.5})
	r.Push([]float64{3.0, 1.5})

	got := r.Flush()
	// Среднее в линейном масштабе: [2.0, 1.0] → dBFS
	want := PowerToDBFS([]float64{2.0, 1.0})
	assertFloat32Slice(t, "Avg", got, want, 0.01)
}

func TestReducer_MaxHold(t *testing.T) {
	r := NewReducer(3, ModeMaxHold)
	r.Push([]float64{1.0, 0.5, 0.3})
	r.Push([]float64{0.2, 0.8, 0.1})
	r.Push([]float64{0.5, 0.1, 0.9})

	got := r.Flush()
	// Max: [1.0, 0.8, 0.9]
	want := PowerToDBFS([]float64{1.0, 0.8, 0.9})
	assertFloat32Slice(t, "MaxHold", got, want, 0.01)
}

func TestReducer_MinHold(t *testing.T) {
	r := NewReducer(3, ModeMinHold)
	r.Push([]float64{1.0, 0.5, 0.3})
	r.Push([]float64{0.2, 0.8, 0.1})

	got := r.Flush()
	// Min: [0.2, 0.5, 0.1]
	want := PowerToDBFS([]float64{0.2, 0.5, 0.1})
	assertFloat32Slice(t, "MinHold", got, want, 0.01)
}

func TestReducer_ExpAvg(t *testing.T) {
	const alpha = 0.4
	r := NewReducer(2, ModeExpAvg, alpha)

	r.Push([]float64{1.0, 0.5})
	// Первый кадр: accum = [1.0, 0.5] (без сглаживания)

	r.Push([]float64{0.0, 1.0})
	// accum[0] = 0.4*0.0 + 0.6*1.0 = 0.6
	// accum[1] = 0.4*1.0 + 0.6*0.5 = 0.7

	got := r.Flush()
	want := PowerToDBFS([]float64{0.6, 0.7})
	assertFloat32Slice(t, "ExpAvg", got, want, 0.01)
}

func TestReducer_Flush_Empty(t *testing.T) {
	r := NewReducer(4, ModeAvg)
	got := r.Flush()
	if got != nil {
		t.Errorf("Flush без Push: got %v, want nil", got)
	}
}

func TestReducer_Reset(t *testing.T) {
	r := NewReducer(2, ModeAvg)
	r.Push([]float64{1.0, 1.0})
	r.Reset()

	got := r.Flush()
	if got != nil {
		t.Errorf("Flush после Reset: got %v, want nil", got)
	}
}

func TestReducer_Flush_Resets(t *testing.T) {
	r := NewReducer(2, ModeMaxHold)
	r.Push([]float64{1.0, 1.0})
	_ = r.Flush()

	r.Push([]float64{0.1, 0.1})
	got := r.Flush()
	// После Flush аккумулятор сбросился; max от одного кадра = он сам
	want := PowerToDBFS([]float64{0.1, 0.1})
	assertFloat32Slice(t, "Flush resets", got, want, 0.01)
}

// --- DownsampleMax ---

func TestDownsampleMax_Even(t *testing.T) {
	in := []float32{-10, -20, -5, -30, -8, -15, -3, -40}
	got := DownsampleMax(in, 4)

	// Группы по 2: [-10,-20]=−10; [-5,-30]=−5; [-8,-15]=−8; [-3,-40]=−3
	want := []float32{-10, -5, -8, -3}
	assertFloat32Slice(t, "DownsampleMax even", got, want, 0.001)
}

func TestDownsampleMax_Remainder(t *testing.T) {
	// 10 бинов → 3 группы по 3, хвост 1 игнорируется
	in := []float32{-1, -2, -3, -4, -5, -6, -7, -8, -9, -10}
	got := DownsampleMax(in, 3)

	// factor=3: [-1,-2,-3]→−1; [-4,-5,-6]→−4; [-7,-8,-9]→−7
	want := []float32{-1, -4, -7}
	assertFloat32Slice(t, "DownsampleMax remainder", got, want, 0.001)
}

func TestDownsampleMax_NoDownsample(t *testing.T) {
	in := []float32{-5, -10}
	got := DownsampleMax(in, 4) // target > len

	assertFloat32Slice(t, "no downsample", got, in, 0.001)
}

func TestDownsampleMax_NilInput(t *testing.T) {
	got := DownsampleMax(nil, 4)
	if got != nil {
		t.Errorf("nil input: got %v", got)
	}
}

// assertFloat32Slice проверяет поэлементное совпадение с допуском.
func assertFloat32Slice(t *testing.T, name string, got, want []float32, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: len=%d, want %d", name, len(got), len(want))
	}
	for i := range got {
		if math.Abs(float64(got[i]-want[i])) > tol {
			t.Errorf("%s[%d]=%g, want %g (±%g)", name, i, got[i], want[i], tol)
		}
	}
}
