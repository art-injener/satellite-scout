package imitator

import (
	"errors"
	"math"
	"testing"
)

func newTestScene(t *testing.T, opts Options) *scene {
	t.Helper()
	s, err := newScene(opts, testRate)
	if err != nil {
		t.Fatalf("newScene: %v", err)
	}
	return s
}

func TestScene_NoisePowerOverWholeBand(t *testing.T) {
	s := newTestScene(t, Options{NoiseDBFS: -60, Seed: 3})
	x := make([]complex64, int(testRate))
	s.fill(x, testCenter)
	if p := 10 * math.Log10(meanPower(x)); math.Abs(p+60) > 0.1 {
		t.Errorf("noise power %.2f dBFS, want -60", p)
	}
}

func TestScene_NoNoiseByDefault(t *testing.T) {
	s := newTestScene(t, Options{Signals: []Signal{testCarrier(30_000)}})
	x := make([]complex64, 4800)
	s.fill(x, testCenter)
	if !isSilent(x) {
		t.Error("zero NoiseDBFS must mean no noise")
	}
}

func TestScene_SignalsAddUp(t *testing.T) {
	a := testCarrier(5_000)
	a.LevelDBFS = -20
	b := testCarrier(-8_000)
	b.LevelDBFS = -30
	s := newTestScene(t, Options{Signals: []Signal{a, b}})

	x := make([]complex64, int(testRate))
	s.fill(x, testCenter)
	if p, want := meanPower(x), 0.01+0.001; math.Abs(p/want-1) > 0.01 {
		t.Errorf("power %.5f, want %.5f (sum of -20 and -30 dBFS)", p, want)
	}
}

func TestScene_SignalOverNoise(t *testing.T) {
	sig := testCarrier(2_000)
	sig.LevelDBFS = -40
	s := newTestScene(t, Options{NoiseDBFS: -50, Signals: []Signal{sig}, Seed: 5})

	x := make([]complex64, int(testRate))
	s.fill(x, testCenter)
	want := math.Pow(10, -4) + math.Pow(10, -5)
	if p := meanPower(x); math.Abs(p/want-1) > 0.02 {
		t.Errorf("power %.3g, want %.3g (signal plus noise)", p, want)
	}
}

func TestScene_InvalidSignalRejected(t *testing.T) {
	_, err := newScene(Options{Signals: []Signal{{Kind: "noise", FrequencyHz: testCenter}}}, testRate)
	if !errors.Is(err, ErrInvalidSignal) {
		t.Errorf("err=%v, want ErrInvalidSignal", err)
	}
}

// Одна секунда эфира по умолчанию при рекомендуемой частоте должна генерироваться заметно быстрее секунды.
func BenchmarkScene_DefaultFillOneSecond(b *testing.B) {
	opts := DefaultOptions()
	rate := New(opts).Capabilities().RecommendedSampleRate
	s, err := newScene(opts, float64(rate))
	if err != nil {
		b.Fatal(err)
	}
	block := make([]complex64, 16384)
	blocks := int(rate) / len(block)
	for b.Loop() {
		for range blocks {
			s.fill(block, DefaultFrequencyHz)
		}
	}
}
