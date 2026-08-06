package dsp

import (
	"math"
	"testing"
)

func TestSpectrumPipeline_OutputLength(t *testing.T) {
	p := NewSpectrumPipeline(256, 128, ModeAvg, WindowHann)
	samples := make([]complex64, 256)
	p.Push(samples)

	out := p.Flush()
	if len(out) != 128 {
		t.Errorf("len(Flush)=%d, want 128", len(out))
	}
}

func TestSpectrumPipeline_NoTargetBins(t *testing.T) {
	p := NewSpectrumPipeline(512, 0, ModeAvg, WindowHann)
	if p.TargetBins() != 512 {
		t.Errorf("TargetBins=%d, want 512 (= fftSize when target=0)", p.TargetBins())
	}

	samples := make([]complex64, 512)
	p.Push(samples)
	out := p.Flush()
	if len(out) != 512 {
		t.Errorf("len(Flush)=%d, want 512", len(out))
	}
}

func TestSpectrumPipeline_FlushWithoutPush(t *testing.T) {
	p := NewSpectrumPipeline(256, 128, ModeAvg, WindowHann)
	if p.Flush() != nil {
		t.Error("Flush without Push should return nil")
	}
}

func TestSpectrumPipeline_ToneDetection(t *testing.T) {
	const (
		n  = 1024
		fs = 48000.0
		f0 = 5000.0
	)

	p := NewSpectrumPipeline(n, 0, ModeAvg, WindowHann)
	samples := make([]complex64, n)
	for i := range samples {
		phase := 2.0 * math.Pi * f0 * float64(i) / fs
		samples[i] = complex(float32(math.Cos(phase)), float32(math.Sin(phase)))
	}
	p.Push(samples)
	out := p.Flush()

	if len(out) != n {
		t.Fatalf("len=%d, want %d", len(out), n)
	}

	// DC в центре (fftshift), тон на +5000 Гц
	binHz := fs / float64(n)
	expectedBin := n/2 + int(f0/binHz)

	peak := out[expectedBin]
	// Уровень тона должен быть значительно выше шума
	noise := out[n/2+n/4] // далеко от тона
	if peak-noise < 30.0 {
		t.Errorf("peak-noise=%.1f dB (peak=%.1f, noise=%.1f), want > 30 dB",
			peak-noise, peak, noise)
	}
}

func TestSpectrumPipeline_Accumulation(t *testing.T) {
	const n = 256

	p := NewSpectrumPipeline(n, 0, ModeAvg, WindowHann)

	for k := 0; k < 5; k++ {
		samples := make([]complex64, n)
		for i := range samples {
			samples[i] = complex(0.1, 0)
		}
		p.Push(samples)
	}

	out := p.Flush()
	if out == nil {
		t.Fatal("Flush returned nil after 5 Pushes")
	}
	if len(out) != n {
		t.Errorf("len=%d, want %d", len(out), n)
	}

	// После 5 Push в ModeAvg результат должен быть finite dBFS
	for i, v := range out {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Errorf("out[%d] is NaN/Inf", i)
			break
		}
	}
}

func TestSpectrumPipeline_ResetClearsState(t *testing.T) {
	p := NewSpectrumPipeline(128, 64, ModeMaxHold, WindowBlackmanHarris)
	samples := make([]complex64, 128)
	for i := range samples {
		samples[i] = complex(1.0, 0)
	}
	p.Push(samples)
	p.Reset()

	if p.Flush() != nil {
		t.Error("Flush after Reset should return nil")
	}
}

func TestSpectrumPipeline_FFTSize(t *testing.T) {
	p := NewSpectrumPipeline(2048, 512, ModeAvg, WindowHann)
	if p.FFTSize() != 2048 {
		t.Errorf("FFTSize=%d, want 2048", p.FFTSize())
	}
}
