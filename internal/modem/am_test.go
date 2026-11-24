package modem

import (
	"math"
	"testing"
)

func TestAMAudioDemod_RecoversToneFrequency(t *testing.T) {
	const toneHz = 1000.0
	mod := NewAM(AMVoice(fmTestRate))
	iq := mod.Modulate(nil, toneAudio(toneHz, 1, fmTestRate))
	audio := NewAMAudioDemod(AMAudioConfig{SampleRate: fmTestRate, AudioRate: fmAudioRate}).Demodulate(nil, iq)
	if want := fmAudioRate; math.Abs(float64(len(audio)-want)) > 2 {
		t.Fatalf("audio samples=%d, want about %d", len(audio), want)
	}
	settled := audio[fmAudioRate/5:]
	if f := toneFreq(settled, fmAudioRate); math.Abs(f-toneHz) > 5 {
		t.Errorf("tone %.1f Hz, want %.0f", f, toneHz)
	}
	if p := peak(settled); p < 0.2 {
		t.Errorf("tone peak %.3f too low", p)
	}
}

func TestAM_EnvelopeFollowsDepth(t *testing.T) {
	iq := NewAM(AMConfig{SampleRate: 1000, Depth: 0.5}).Modulate(nil, []float32{1, -1, 0})
	want := []float64{1.5, 0.5, 1.0}
	for i, x := range iq {
		if imag(x) != 0 {
			t.Fatalf("sample %d has Q≠0: %v", i, x)
		}
		if math.Abs(float64(real(x))-want[i]) > 1e-6 {
			t.Errorf("sample %d amp %.4f, want %.1f", i, real(x), want[i])
		}
	}
}
