package dsp

import (
	"math"
	"testing"
)

func TestLevelDBFS_UnitToneIsFullScale(t *testing.T) {
	if got := LevelDBFS(makeToneC64(1000, 1000, 48000)); math.Abs(float64(got)) > 1e-4 {
		t.Errorf("unit tone level=%g dBFS, want 0", got)
	}
}

func TestLevelDBFS_ScalesWithAmplitude(t *testing.T) {
	x := makeToneC64(1000, 1000, 48000)
	for i := range x {
		x[i] *= 0.01
	}
	if got := LevelDBFS(x); math.Abs(float64(got)+40) > 1e-3 {
		t.Errorf("level=%g dBFS, want -40", got)
	}
}

func TestLevelDBFS_SilenceIsFloor(t *testing.T) {
	if got := LevelDBFS(make([]complex64, 100)); got != DBFloor {
		t.Errorf("silence level=%g, want DBFloor", got)
	}
	if got := LevelDBFS(nil); got != DBFloor {
		t.Errorf("empty block level=%g, want DBFloor", got)
	}
}

// Channel level measured after the channel filter must match what the imitator sets:
// the signal level, and the noise power in the band where the imitator defines SNR.
// The channel is cut in two stages: a single filter at the full rate with a few hundred
// taps has a transition band wider than 15 kHz and passes ~1 dB less noise than the band.
func TestSquelch_OpensAtThreshold(t *testing.T) {
	quiet := makeToneC64(256, 1000, 48000)
	for i := range quiet {
		quiet[i] *= 0.001 // -60 dBFS
	}
	loud := makeToneC64(256, 1000, 48000) // 0 dBFS

	sq := Squelch{ThresholdDBFS: -30}
	if sq.Open(quiet) {
		t.Error("squelch open for -60 dBFS at threshold -30")
	}
	if got := sq.LevelDBFS(); math.Abs(float64(got)+60) > 1e-3 {
		t.Errorf("last level=%g, want -60", got)
	}
	if !sq.Open(loud) {
		t.Error("squelch closed for 0 dBFS at threshold -30")
	}
	if got := sq.LevelDBFS(); math.Abs(float64(got)) > 1e-3 {
		t.Errorf("last level=%g, want 0", got)
	}
}

func TestSquelch_HoldsOpenAfterSignal(t *testing.T) {
	loud := makeToneC64(100, 1000, 48000)
	quiet := make([]complex64, 100)
	sq := Squelch{ThresholdDBFS: -30, HoldSamples: 250}

	if !sq.Open(loud) {
		t.Fatal("closed on a loud block")
	}
	// 250 samples of hold cover the next three quiet blocks of 100: 250, 150, 50 left.
	for i := range 3 {
		if !sq.Open(quiet) {
			t.Fatalf("quiet block %d: closed within hold", i)
		}
	}
	if sq.Open(quiet) {
		t.Error("still open after hold expired")
	}
	if sq.Open(quiet) {
		t.Error("reopened without signal")
	}
	if !sq.Open(loud) {
		t.Error("closed on a loud block after hold")
	}
}
