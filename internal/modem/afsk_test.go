package modem

import (
	"math"
	"math/cmplx"
	"testing"
)

const testSampleRate = 48000.0

// instFreq возвращает мгновенную частоту (Гц) по разности фаз соседних отсчётов — FM-дискриминатор.
func instFreq(x []complex64) []float64 {
	out := make([]float64, 0, len(x))
	for i := 1; i < len(x); i++ {
		d := complex128(x[i]) * cmplx.Conj(complex128(x[i-1]))
		out = append(out, cmplx.Phase(d)*testSampleRate/(2*math.Pi))
	}
	return out
}

func repeatLevel(level byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = level
	}
	return out
}

// zeroCrossingRate — частота синусоиды по числу переходов через ноль.
func zeroCrossingRate(a []float64, sampleRate float64) float64 {
	crossings := 0
	for i := 1; i < len(a); i++ {
		if (a[i-1] < 0) != (a[i] < 0) {
			crossings++
		}
	}
	return float64(crossings) / 2 / (float64(len(a)) / sampleRate)
}

func TestAFSK1200_Defaults(t *testing.T) {
	cfg := AFSK1200(testSampleRate)
	if cfg.Baud != 1200 || cfg.MarkHz != 1200 || cfg.SpaceHz != 2200 {
		t.Errorf("baud/mark/space=%v/%v/%v, want 1200/1200/2200", cfg.Baud, cfg.MarkHz, cfg.SpaceHz)
	}
	if cfg.DeviationHz != 3000 {
		t.Errorf("deviation=%v, want 3000", cfg.DeviationHz)
	}
}

func TestAFSK_ConstantEnvelope(t *testing.T) {
	m := NewAFSK(AFSK1200(testSampleRate))
	x := m.Modulate(nil, []byte{1, 0, 0, 1, 1, 1, 0, 1, 0, 0})
	for i, v := range x {
		if mag := cmplx.Abs(complex128(v)); math.Abs(mag-1) > 1e-5 {
			t.Fatalf("|x[%d]|=%g, want 1", i, mag)
		}
	}
}

func TestAFSK_SampleCountWithFractionalSymbol(t *testing.T) {
	// 50000 / 1200 = 41.67 отсчёта на символ: дробная часть не должна теряться.
	m := NewAFSK(AFSK1200(50000))
	x := m.Modulate(nil, repeatLevel(1, 120))
	if len(x) != 5000 {
		t.Errorf("len=%d, want 5000 (120 symbols * 50000 / 1200)", len(x))
	}
}

func TestAFSK_MarkToneOnFMAudio(t *testing.T) {
	m := NewAFSK(AFSK1200(testSampleRate))
	audio := instFreq(m.Modulate(nil, repeatLevel(1, 600)))
	if f := zeroCrossingRate(audio, testSampleRate); math.Abs(f-1200) > 5 {
		t.Errorf("mark tone=%.1f Hz, want 1200", f)
	}
}

func TestAFSK_SpaceToneOnFMAudio(t *testing.T) {
	m := NewAFSK(AFSK1200(testSampleRate))
	audio := instFreq(m.Modulate(nil, repeatLevel(0, 600)))
	if f := zeroCrossingRate(audio, testSampleRate); math.Abs(f-2200) > 5 {
		t.Errorf("space tone=%.1f Hz, want 2200", f)
	}
}

func TestAFSK_PeakDeviation(t *testing.T) {
	m := NewAFSK(AFSK1200(testSampleRate))
	audio := instFreq(m.Modulate(nil, repeatLevel(1, 120)))
	peak := 0.0
	for _, f := range audio {
		peak = math.Max(peak, math.Abs(f))
	}
	if math.Abs(peak-3000) > 30 {
		t.Errorf("peak deviation=%.1f Hz, want 3000", peak)
	}
}

func TestAFSK_AudioContinuousAtToneSwitch(t *testing.T) {
	cfg := AFSK1200(testSampleRate)
	m := NewAFSK(cfg)
	audio := instFreq(m.Modulate(nil, []byte{1, 0, 1, 1, 0, 0, 1, 0}))
	// Наибольший шаг непрерывного аудио — у тона space на пике крутизны синуса.
	maxStep := cfg.DeviationHz * 2 * math.Pi * cfg.SpaceHz / testSampleRate * 1.05
	for i := 1; i < len(audio); i++ {
		if step := math.Abs(audio[i] - audio[i-1]); step > maxStep {
			t.Fatalf("audio jump %.1f Hz at sample %d, max %.1f", step, i, maxStep)
		}
	}
}

func TestAFSK_StateContinuesAcrossCalls(t *testing.T) {
	levels := []byte{1, 0, 0, 1, 0, 1, 1, 0, 1, 0, 0, 0}
	cfg := AFSK1200(50000)

	whole := NewAFSK(cfg).Modulate(nil, levels)

	m := NewAFSK(cfg)
	parts := m.Modulate(nil, levels[:5])
	parts = m.Modulate(parts, levels[5:])

	if len(parts) != len(whole) {
		t.Fatalf("len=%d, want %d", len(parts), len(whole))
	}
	for i := range whole {
		if cmplx.Abs(complex128(whole[i]-parts[i])) > 1e-5 {
			t.Fatalf("sample %d differs: %v vs %v", i, parts[i], whole[i])
		}
	}
}
