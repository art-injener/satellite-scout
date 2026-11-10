package dsp

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestDCShift_RemovesConstant(t *testing.T) {
	const rate = 2_560_000.0
	s := NewDCShift(rate/16, rate)
	const n = 65536
	x := make([]complex64, n)
	for i := range x {
		x[i] = complex(0.4, -0.25)
	}
	s.Correct(x)
	if mag := meanAbs(x[n/2:]); mag > 1e-3 {
		t.Errorf("residual magnitude %g, want < 1e-3", mag)
	}
}

func TestDCShift_RecentersToneAndKeepsAmplitude(t *testing.T) {
	const rate = 2_560_000.0
	const offset = rate / 16
	s := NewDCShift(offset, rate)
	const n = 65536
	x := make([]complex64, n)
	for i := range x {
		ph := -2 * math.Pi * offset * float64(i) / rate
		// Constant beside the tone: the hardware DC spike.
		x[i] = complex(float32(0.3+0.5*math.Cos(ph)), float32(-0.2+0.5*math.Sin(ph)))
	}
	s.Correct(x)
	tail := x[n/2:]
	if mag := meanAbs(tail); math.Abs(mag-0.5) > 0.02 {
		t.Errorf("amplitude %g, want 0.5", mag)
	}
	if f := toneHz(tail, rate); math.Abs(f) > 5 {
		t.Errorf("frequency %g Hz, want 0", f)
	}
}

func TestDCShift_ShiftDirection(t *testing.T) {
	const (
		rate   = 2_560_000.0
		offset = rate / 16
		extra  = 25_000.0
	)
	s := NewDCShift(offset, rate)
	const n = 65536
	x := make([]complex64, n)
	for i := range x {
		ph := 2 * math.Pi * (-offset + extra) * float64(i) / rate
		x[i] = complex(float32(math.Cos(ph)), float32(math.Sin(ph)))
	}
	s.Correct(x)
	if f := toneHz(x[n/2:], rate); math.Abs(f-extra) > 5 {
		t.Errorf("frequency %g Hz, want %g", f, extra)
	}
}

func meanAbs(x []complex64) float64 {
	var sum float64
	for _, v := range x {
		sum += cmplx.Abs(complex128(v))
	}
	return sum / float64(len(x))
}

func toneHz(x []complex64, rate float64) float64 {
	var acc complex128
	for i := 0; i < len(x)-1; i++ {
		acc += complex128(x[i+1]) * cmplx.Conj(complex128(x[i]))
	}
	return cmplx.Phase(acc) * rate / (2 * math.Pi)
}
