package modem

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestSweep_UpchirpCrossesBandwidth(t *testing.T) {
	const rate = 500_000.0
	sw := NewSweep(rate, 125_000, 7)
	n := sw.SymbolSamples()
	iq := make([]complex64, n)
	for i := range iq {
		iq[i] = sw.Next()
	}
	f := func(i int) float64 {
		return cmplx.Phase(complex128(iq[i+1])*cmplx.Conj(complex128(iq[i]))) * rate / (2 * math.Pi)
	}
	if rise := f(n-3) - f(1); rise < 100_000 {
		t.Fatalf("frequency rise %.0f Hz over one symbol, want about %.0f", rise, sw.BandwidthHz())
	}
	if math.Abs(sw.SlopeHzS()-125_000*rate/float64(n)) > 1 {
		t.Fatalf("slope %.0f", sw.SlopeHzS())
	}
}

func TestSweep_NarrowsWhenRateIsLow(t *testing.T) {
	sw := NewSweep(48_000, 125_000, 7)
	if sw.BandwidthHz() > 0.4*48_000+1 {
		t.Fatalf("bandwidth %.0f Hz does not fit 48 kHz", sw.BandwidthHz())
	}
}
