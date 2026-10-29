package modem

import (
	"math/rand/v2"
	"testing"
)

func noiseIQ(n int, seed uint64) []complex64 {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	iq := make([]complex64, n)
	for i := range iq {
		iq[i] = complex(float32(r.NormFloat64()), float32(r.NormFloat64()))
	}
	return iq
}

func TestFSKDemod_DeviationFromLevels(t *testing.T) {
	cfg := FSK9600(fskTestRate)
	d := NewFSKDemod(cfg)
	d.Demodulate(nil, modulatePRBSFSK(cfg, 5000))
	// Averaging over a symbol and Gaussian shaping pull the decision levels a little inside.
	if dev := d.DeviationHz(); dev < 0.75*cfg.DeviationHz || dev > 1.05*cfg.DeviationHz {
		t.Errorf("deviation %.0f Hz, want about %.0f Hz", dev, cfg.DeviationHz)
	}
}

func TestFSKDemod_NoDeviationBeforeSignal(t *testing.T) {
	if dev := NewFSKDemod(FSK9600(fskTestRate)).DeviationHz(); dev != 0 {
		t.Errorf("deviation %.0f Hz before any sample, want 0", dev)
	}
}

func TestFSKDemod_ClockJitterSmallOnCleanSignal(t *testing.T) {
	cfg := FSK9600(fskTestRate)
	d := NewFSKDemod(cfg)
	d.Demodulate(nil, modulatePRBSFSK(cfg, 5000))
	if j := d.ClockJitter(); j > 0.1 {
		t.Errorf("clock jitter %.3f symbol on a clean signal, want < 0.1", j)
	}
}

func TestFSKDemod_ClockJitterLargeOnNoise(t *testing.T) {
	d := NewFSKDemod(FSK9600(fskTestRate))
	d.Demodulate(nil, noiseIQ(fskTestRate, 1))
	// Transitions at random phase: mean |0.5 − phase| is a quarter of a symbol.
	if j := d.ClockJitter(); j < 0.15 {
		t.Errorf("clock jitter %.3f symbol on noise, want > 0.15", j)
	}
}

func TestAFSKDemod_ClockJitterSmallOnCleanSignal(t *testing.T) {
	cfg := AFSK1200(testSampleRate)
	d := NewAFSKDemod(cfg)
	d.Demodulate(nil, modulatePRBS(cfg, 3000))
	if j := d.ClockJitter(); j > 0.1 {
		t.Errorf("clock jitter %.3f symbol on a clean signal, want < 0.1", j)
	}
}

func TestAFSKDemod_ClockJitterLargeOnNoise(t *testing.T) {
	d := NewAFSKDemod(AFSK1200(testSampleRate))
	d.Demodulate(nil, noiseIQ(testSampleRate, 2))
	if j := d.ClockJitter(); j < 0.15 {
		t.Errorf("clock jitter %.3f symbol on noise, want > 0.15", j)
	}
}
