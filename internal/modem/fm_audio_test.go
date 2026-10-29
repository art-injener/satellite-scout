package modem

import (
	"math"
	"testing"
)

const (
	fmTestRate  = 32_000
	fmAudioRate = 12_000
)

// toneAudio — a sine of the given frequency and amplitude at the IQ rate.
func toneAudio(freq, amp float64, n int) []float32 {
	a := make([]float32, n)
	for i := range a {
		a[i] = float32(amp * math.Sin(twoPi*freq*float64(i)/fmTestRate))
	}
	return a
}

// toneFreq estimates the frequency of a sine by its upward zero crossings.
func toneFreq(x []float32, rate float64) float64 {
	first, last, n := -1.0, -1.0, 0
	for i := 1; i < len(x); i++ {
		if x[i-1] < 0 && x[i] >= 0 {
			t := float64(i-1) + float64(-x[i-1]/(x[i]-x[i-1]))
			if first < 0 {
				first = t
			}
			last = t
			n++
		}
	}
	if n < 2 {
		return 0
	}
	return float64(n-1) * rate / (last - first)
}

func peak(x []float32) float64 {
	m := 0.0
	for _, v := range x {
		m = max(m, math.Abs(float64(v)))
	}
	return m
}

func fmAudioConfig(tau float64) FMAudioConfig {
	return FMAudioConfig{SampleRate: fmTestRate, DeviationHz: 5000, AudioRate: fmAudioRate, DeemphasisTau: tau}
}

func TestFMAudioDemod_RecoversToneFrequencyAndLevel(t *testing.T) {
	iq := NewFM(FMConfig{SampleRate: fmTestRate, DeviationHz: 5000}).Modulate(nil, toneAudio(1900, 0.6, fmTestRate))
	audio := NewFMAudioDemod(fmAudioConfig(0)).Demodulate(nil, iq)

	if want := fmAudioRate; math.Abs(float64(len(audio)-want)) > 2 {
		t.Fatalf("audio samples=%d, want about %d for one second", len(audio), want)
	}
	settled := audio[fmAudioRate/10:]
	if f := toneFreq(settled, fmAudioRate); math.Abs(f-1900) > 2 {
		t.Errorf("tone %.1f Hz, want 1900", f)
	}
	if p := peak(settled); math.Abs(p-0.6) > 0.03 {
		t.Errorf("tone peak %.3f, want 0.6 of the deviation", p)
	}
}

func TestFMAudioDemod_SSTVToneRange(t *testing.T) {
	// Sync 1200 Hz, black 1500 Hz, white 2300 Hz.
	for _, f := range []float64{1200, 1500, 2300} {
		iq := NewFM(FMConfig{SampleRate: fmTestRate, DeviationHz: 5000}).Modulate(nil, toneAudio(f, 0.5, fmTestRate/4))
		audio := NewFMAudioDemod(fmAudioConfig(0)).Demodulate(nil, iq)
		if got := toneFreq(audio[300:], fmAudioRate); math.Abs(got-f) > 3 {
			t.Errorf("tone %.1f Hz, want %.0f", got, f)
		}
	}
}

func TestFMAudioDemod_Deemphasis(t *testing.T) {
	const tau = 530e-6 // NBFM: corner near 300 Hz
	level := func(f float64) float64 {
		iq := NewFM(FMConfig{SampleRate: fmTestRate, DeviationHz: 5000}).Modulate(nil, toneAudio(f, 0.5, fmTestRate/2))
		audio := NewFMAudioDemod(fmAudioConfig(tau)).Demodulate(nil, iq)
		return peak(audio[fmAudioRate/10:])
	}
	// First-order low-pass: |H(f)| = 1/sqrt(1+(2π f τ)²).
	h := func(f float64) float64 { return 1 / math.Hypot(1, twoPi*f*tau) }
	want := h(2300) / h(1200)
	if got := level(2300) / level(1200); math.Abs(got-want) > 0.03 {
		t.Errorf("2300/1200 Hz level ratio %.3f, want %.3f", got, want)
	}
}

func TestFMAudioDemod_ContinuesAcrossCalls(t *testing.T) {
	iq := NewFM(FMConfig{SampleRate: fmTestRate, DeviationHz: 5000}).Modulate(nil, toneAudio(1700, 0.5, 9_001))
	whole := NewFMAudioDemod(fmAudioConfig(530e-6)).Demodulate(nil, iq)

	d := NewFMAudioDemod(fmAudioConfig(530e-6))
	var parts []float32
	for i := 0; i < len(iq); i += 777 {
		parts = d.Demodulate(parts, iq[i:min(i+777, len(iq))])
	}
	if len(parts) != len(whole) {
		t.Fatalf("chunked samples=%d, whole=%d", len(parts), len(whole))
	}
	for i := range whole {
		if math.Abs(float64(parts[i]-whole[i])) > 1e-5 {
			t.Fatalf("sample %d: chunked %.6f, whole %.6f", i, parts[i], whole[i])
		}
	}
}

func TestFM_ContinuousPhaseAcrossCalls(t *testing.T) {
	audio := toneAudio(1000, 0.7, 1000)
	whole := NewFM(FMConfig{SampleRate: fmTestRate, DeviationHz: 5000}).Modulate(nil, audio)
	m := NewFM(FMConfig{SampleRate: fmTestRate, DeviationHz: 5000})
	parts := m.Modulate(nil, audio[:333])
	parts = m.Modulate(parts, audio[333:])
	for i := range whole {
		if d := whole[i] - parts[i]; math.Abs(float64(real(d)))+math.Abs(float64(imag(d))) > 1e-5 {
			t.Fatalf("sample %d differs: %v vs %v", i, whole[i], parts[i])
		}
	}
}
