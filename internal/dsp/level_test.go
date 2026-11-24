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

func TestPeakDBFS_ImpulseAboveAverage(t *testing.T) {
	iq := make([]complex64, 10_000)
	for i := range iq {
		iq[i] = complex64(complex(1e-4, 0)) // −80 dBFS
	}
	// Ровно длина окна пика на −20 dBFS: короче блока, без разбавления окна.
	for i := 5000; i < 5000+peakWindow; i++ {
		iq[i] = complex64(complex(0.1, 0))
	}
	peak := PeakDBFS(iq)
	if math.Abs(float64(peak)+20) > 0.2 {
		t.Errorf("peak=%g dBFS, want −20", peak)
	}
	avg := LevelDBFS(iq)
	if avg > peak-15 {
		t.Errorf("average level=%g dBFS, want well below peak %g", avg, peak)
	}
}

func TestPeakDBFS_NoiseCrestStaysNearAverage(t *testing.T) {
	// Детерминированный «шум»: чередование амплитуд с той же средней мощностью −60 dBFS.
	iq := make([]complex64, 4096)
	a := float32(math.Sqrt(1e-6 * 2)) // две половины по половине отсчётов: mean power 1e-6
	for i := range iq {
		if i%2 == 0 {
			iq[i] = complex64(complex(a, 0))
		} else {
			iq[i] = 0
		}
	}
	level := LevelDBFS(iq)
	peak := PeakDBFS(iq)
	if math.Abs(float64(level)+60) > 0.5 {
		t.Fatalf("level=%g, want −60", level)
	}
	// Окно усредняет чередование: пик не должен уходить к уровню одиночного отсчёта (−57 dBFS).
	if peak-level > 3 {
		t.Errorf("window peak %g is %.1f dB above average %g; single-sample crest would be ~3 dB here",
			peak, peak-level, level)
	}
}

func TestSquelch_OpensOnPeakImpulse(t *testing.T) {
	iq := make([]complex64, 10_000)
	for i := range iq {
		iq[i] = complex64(complex(1e-4, 0)) // −80 dBFS average floor
	}
	for i := 1234; i < 1234+peakWindow; i++ {
		iq[i] = complex64(complex(0.1, 0)) // −20 dBFS pulse
	}
	sq := Squelch{ThresholdDBFS: -30}
	if !sq.Open(iq) {
		t.Fatal("squelch closed on an impulse whose peak clears the threshold")
	}
	if !sq.AboveThreshold() {
		t.Error("AboveThreshold false on the impulse block")
	}
	if got := sq.PeakDBFS(); math.Abs(float64(got)+20) > 0.2 {
		t.Errorf("peak=%g, want −20", got)
	}
	if got := sq.LevelDBFS(); got >= -30 {
		t.Errorf("average level=%g still above threshold; test setup wrong", got)
	}
}

func TestSquelch_IgnoresNoiseFloorTenDBBelowThreshold(t *testing.T) {
	// Как в тестах цепочки: шум канала −60 dBFS, порог −50. Окно не должно
	// открывать порог на гребне шума (в отличие от max по одному отсчёту).
	iq := makeToneC64(4096, 1000, 48000)
	for i := range iq {
		iq[i] *= 0.001 // −60 dBFS
	}
	sq := Squelch{ThresholdDBFS: -50}
	if sq.Open(iq) {
		t.Fatalf("opened on noise: level=%g peak=%g", sq.LevelDBFS(), sq.PeakDBFS())
	}
}

func TestSquelch_AboveThresholdFalseDuringHold(t *testing.T) {
	loud := makeToneC64(100, 1000, 48000)
	quiet := make([]complex64, 100)
	sq := Squelch{ThresholdDBFS: -30, HoldSamples: 200}
	if !sq.Open(loud) {
		t.Fatal("closed on loud")
	}
	if !sq.Open(quiet) {
		t.Fatal("closed within hold")
	}
	if sq.AboveThreshold() {
		t.Error("quiet hold block must not count as above threshold")
	}
}
