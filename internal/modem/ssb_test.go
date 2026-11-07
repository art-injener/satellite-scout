package modem

import (
	"math"
	"testing"
)

const ssbTestRate = 32_000

// complexTone — e^{j2πft} at the SSB test rate with amplitude amp.
func complexTone(freq, amp float64, n int) []complex64 {
	iq := make([]complex64, n)
	for i := range iq {
		s, c := math.Sincos(twoPi * freq * float64(i) / ssbTestRate)
		iq[i] = complex(float32(amp*c), float32(amp*s))
	}
	return iq
}

// The channel filter leaves the sideband centered at zero; the demodulator moves it back
// by ShiftHz and takes the real part, so a tone at f-ShiftHz sounds at f.
func TestSSBDemod_ShiftsSidebandBackToAudio(t *testing.T) {
	for _, tc := range []struct {
		name    string
		shiftHz float64
		inHz    float64
		wantHz  float64
	}{
		{"upper", 1600, -400, 1200},
		{"lower", -1600, 400, 1200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewSSBDemod(SSBConfig{SampleRate: ssbTestRate, AudioRate: fmAudioRate, ShiftHz: tc.shiftHz})
			audio := d.Demodulate(nil, complexTone(tc.inHz, 0.01, ssbTestRate))
			if math.Abs(float64(len(audio)-fmAudioRate)) > 2 {
				t.Fatalf("audio samples=%d, want about %d for one second", len(audio), fmAudioRate)
			}
			settled := audio[fmAudioRate/4:]
			if f := toneFreq(settled, fmAudioRate); math.Abs(f-tc.wantHz) > 2 {
				t.Errorf("tone %.1f Hz, want %.0f", f, tc.wantHz)
			}
		})
	}
}

// SSB amplitude follows the signal level: the AGC brings weak and strong signals to one loudness.
func TestSSBDemod_AGCLevelsLoudness(t *testing.T) {
	amps := []float64{1e-4, 1e-2, 0.5}
	peaks := make([]float64, 0, len(amps))
	for _, amp := range amps {
		d := NewSSBDemod(SSBConfig{SampleRate: ssbTestRate, AudioRate: fmAudioRate, ShiftHz: 1600})
		audio := d.Demodulate(nil, complexTone(-400, amp, ssbTestRate))
		p := peak(audio[fmAudioRate/2:])
		if p < 0.3 || p > 0.9 {
			t.Errorf("input %.0e: audio peak %.3f, want within 0.3..0.9", amp, p)
		}
		peaks = append(peaks, p)
	}
	if math.Abs(peaks[0]-peaks[2]) > 0.1 {
		t.Errorf("peaks %.3f vs %.3f across 74 dB of input, want the same loudness", peaks[0], peaks[2])
	}
}

func TestSSBDemod_ContinuesAcrossCalls(t *testing.T) {
	iq := complexTone(-300, 0.05, 9_001)
	cfg := SSBConfig{SampleRate: ssbTestRate, AudioRate: fmAudioRate, ShiftHz: 1600}
	whole := NewSSBDemod(cfg).Demodulate(nil, iq)

	d := NewSSBDemod(cfg)
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
