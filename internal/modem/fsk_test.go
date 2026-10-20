package modem

import (
	"math"
	"math/cmplx"
	"testing"
)

// instFreqHz returns the instantaneous frequency between neighbouring samples.
func instFreqHz(iq []complex64, sampleRate float64) []float64 {
	f := make([]float64, len(iq))
	for i := 1; i < len(iq); i++ {
		d := complex128(iq[i]) * cmplx.Conj(complex128(iq[i-1]))
		f[i] = cmplx.Phase(d) * sampleRate / (2 * math.Pi)
	}
	return f
}

func runLevels(runs ...int) []byte {
	var levels []byte
	for i, n := range runs {
		for range n {
			levels = append(levels, byte(1-i%2))
		}
	}
	return levels
}

func TestFSK_DeviationOnLongRuns(t *testing.T) {
	cfg := FSK9600(1_024_000)
	iq := NewFSK(cfg).Modulate(nil, runLevels(20, 20))
	f := instFreqHz(iq, cfg.SampleRate)
	sps := cfg.SampleRate / cfg.Baud

	// Middle of each run, shifted by the modulator delay.
	for _, c := range []struct {
		symbol float64
		want   float64
	}{{10, cfg.DeviationHz}, {30, -cfg.DeviationHz}} {
		i := int((c.symbol + fskDelaySymbols) * sps)
		if math.Abs(f[i]-c.want) > 0.01*cfg.DeviationHz {
			t.Errorf("symbol %.0f: frequency %.1f Hz, want %.1f", c.symbol, f[i], c.want)
		}
	}
}

func TestFSK_ConstantEnvelopeAndSampleCount(t *testing.T) {
	cfg := FSK9600(50_000) // 5.21 samples per symbol: not an integer
	levels := runLevels(3, 1, 2, 5, 1, 1, 4)
	iq := NewFSK(cfg).Modulate(nil, levels)
	if want := int(math.Round(float64(len(levels)) * cfg.SampleRate / cfg.Baud)); len(iq) != want {
		t.Errorf("samples=%d, want %d", len(iq), want)
	}
	for i, x := range iq {
		if a := cmplx.Abs(complex128(x)); math.Abs(a-1) > 1e-5 {
			t.Fatalf("sample %d amplitude %.6f, want 1", i, a)
		}
	}
}

func TestFSK_SplitBlocksMatchOneCall(t *testing.T) {
	cfg := FSK9600(50_000)
	levels := randomBits(300, 5)
	whole := NewFSK(cfg).Modulate(nil, levels)

	m := NewFSK(cfg)
	var split []complex64
	for i := 0; i < len(levels); i += 7 {
		split = m.Modulate(split, levels[i:min(i+7, len(levels))])
	}
	if len(split) != len(whole) {
		t.Fatalf("split samples=%d, want %d", len(split), len(whole))
	}
	for i := range whole {
		if cmplx.Abs(complex128(split[i]-whole[i])) > 1e-5 {
			t.Fatalf("sample %d differs: %v vs %v", i, split[i], whole[i])
		}
	}
}

// Gaussian shaping spreads a transition over about a symbol: rectangular FSK would
// jump by twice the deviation between two samples.
func TestFSK_GaussianShapingSmoothsTransitions(t *testing.T) {
	cfg := FSK9600(1_024_000)
	f := instFreqHz(NewFSK(cfg).Modulate(nil, randomBits(400, 6)), cfg.SampleRate)
	maxStep := 0.0
	for i := 2; i < len(f); i++ {
		maxStep = math.Max(maxStep, math.Abs(f[i]-f[i-1]))
	}
	if limit := 2 * cfg.DeviationHz / 20; maxStep > limit {
		t.Errorf("max frequency step %.0f Hz per sample, want below %.0f", maxStep, limit)
	}
	if maxStep == 0 {
		t.Error("frequency never changes")
	}
}
