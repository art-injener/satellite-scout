package demod

import (
	"math"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sstv"
)

// toAir moves baseband to testOffset from the tuning, adds noise at snr dB in 15 kHz
// and scales it to testLevel.
func toAir(clean []complex64, rate, snr float64) []complex64 {
	ch := modem.NewChannel(modem.ChannelConfig{
		SampleRate:       rate,
		OffsetHz:         func(time.Duration) float64 { return testOffset },
		SNRdB:            snr,
		NoiseBandwidthHz: 15_000,
		Seed:             5,
	})
	air := ch.Process(nil, clean)
	gain := complex(float32(math.Pow(10, testLevel/20)), 0)
	for i := range air {
		air[i] *= gain
	}
	return air
}

// fmToneAir — a 1 kHz voice-like tone over FM at 2.5 kHz deviation, seconds long.
func fmToneAir(seconds float64) []complex64 {
	n := int(seconds * testRate)
	a := make([]float32, n)
	for i := range a {
		a[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / testRate))
	}
	clean := modem.NewFM(modem.FMConfig{SampleRate: testRate, DeviationHz: 2500}).Modulate(nil, a)
	return toAir(clean, testRate, 30)
}

// ssbAir — the suppressed carrier at testOffset with a tone in the upper sideband at usbHz
// and a tone of the same level in the lower one at lsbHz below the carrier.
func ssbAir(usbHz, lsbHz, seconds float64) []complex64 {
	clean := make([]complex64, int(seconds*testRate))
	for i := range clean {
		t := float64(i) / testRate
		u := complex(math.Cos(2*math.Pi*usbHz*t), math.Sin(2*math.Pi*usbHz*t))
		l := complex(math.Cos(2*math.Pi*lsbHz*t), -math.Sin(2*math.Pi*lsbHz*t))
		clean[i] = complex64(0.5 * (u + l))
	}
	return toAir(clean, testRate, 30)
}

func collectAudio(c *Chain, frames []sdr.IQFrame, s sdr.ChannelSettings) []float32 {
	var out []float32
	for _, f := range frames {
		c.Process(f, s)
		out = append(out, c.Audio()...)
	}
	return out
}

// tonePower — power of the audio at freq (single-bin DFT).
func tonePower(x []float32, freq float64) float64 {
	var re, im float64
	for i, v := range x {
		s, c := math.Sincos(2 * math.Pi * freq * float64(i) / AudioRate)
		re += float64(v) * c
		im -= float64(v) * s
	}
	n := float64(len(x))
	return (re*re + im*im) / (n * n)
}

func voiceSettings(demod string, bw uint32, squelch float64) sdr.ChannelSettings {
	return sdr.ChannelSettings{OffsetHz: testOffset, BandwidthHz: bw, SquelchDBFS: squelch, Demod: demod}
}

func checkAudioLength(t *testing.T, audio []float32, seconds float64) {
	t.Helper()
	if want := seconds * AudioRate; math.Abs(float64(len(audio))-want) > 0.01*want {
		t.Fatalf("audio samples=%d, want about %.0f: the stream must keep real time", len(audio), want)
	}
}

// Broadcast FM is 200 kHz wide. A carrier 40 kHz off the channel center is outside
// a 15 kHz voice window and inside the broadcast window, and the 1 kHz tone is heard.
func TestChain_WideFMHearsOffsetBroadcast(t *testing.T) {
	const seconds = 0.6
	n := int(seconds * testRate)
	a := make([]float32, n)
	for i := range a {
		a[i] = float32(0.5 * math.Sin(2*math.Pi*1000*float64(i)/testRate))
	}
	clean := modem.NewFM(modem.FMConfig{SampleRate: testRate, DeviationHz: wfmDeviationHz}).Modulate(nil, a)
	air := toAir(clean, testRate, 40)

	c := NewChain()
	frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)
	s := voiceSettings("wfm", sdr.WFMBandwidthHz, -90)
	s.OffsetHz = testOffset - 40_000
	audio := collectAudio(c, frames, s)

	checkAudioLength(t, audio, seconds)
	settled := audio[AudioRate/5:]
	tone := tonePower(settled, 1000)
	if other := tonePower(settled, 1700); tone < 100*other {
		t.Errorf("1000 Hz power %.2e vs 1700 Hz %.2e, want the tone", tone, other)
	}
	// 50 µs deemphasis at 1 kHz leaves about 0.47 of a half-deviation tone.
	if amp := 2 * math.Sqrt(tone); amp < 0.3 || amp > 0.65 {
		t.Errorf("tone amplitude %.3f, want about 0.47", amp)
	}
	if st := c.Status(); !st.Supported || st.PassbandHz != float64(sdr.WFMBandwidthHz) {
		t.Errorf("status supported=%v passband=%.0f, want %d", st.Supported, st.PassbandHz, sdr.WFMBandwidthHz)
	}
}

func TestChain_FMVoiceToAudio(t *testing.T) {
	c := NewChain()
	frames := toFrames(fmToneAir(1), testRate, sdr.DefaultBlockSize, 0)
	audio := collectAudio(c, frames, voiceSettings("fm", 15_000, -90))

	checkAudioLength(t, audio, 1)
	settled := audio[AudioRate/5:]
	tone := tonePower(settled, 1000)
	if other := tonePower(settled, 1700); tone < 1000*other {
		t.Errorf("1000 Hz power %.2e vs 1700 Hz %.2e, want the tone 30 dB above", tone, other)
	}
	// Deemphasis 530 µs at 1 kHz: 0.5 of the deviation becomes about 0.144.
	if a := 2 * math.Sqrt(tone); a < 0.12 || a > 0.17 {
		t.Errorf("tone amplitude %.3f, want about 0.144", a)
	}
	if st := c.Status(); !st.Supported || !st.SquelchOpen {
		t.Errorf("status=%+v, want supported with the squelch open", st)
	}
}

// Below the threshold the stream goes on with silence: the player keeps real time.
func TestChain_ClosedSquelchGivesSilence(t *testing.T) {
	c := NewChain()
	frames := toFrames(fmToneAir(0.5), testRate, sdr.DefaultBlockSize, 0)
	audio := collectAudio(c, frames, voiceSettings("fm", 15_000, testLevel+20))

	checkAudioLength(t, audio, 0.5)
	for i, v := range audio {
		if v != 0 {
			t.Fatalf("sample %d = %v, want silence with the squelch closed", i, v)
		}
	}
}

// The channel is the selected band around its offset; the suppressed carrier sits 200 Hz past
// the band edge (upper edge for LSB, lower for USB), so the other sideband does not sound.
func TestChain_SSBSelectsSideband(t *testing.T) {
	for _, tc := range []struct {
		demod        string
		want, reject float64
		centerHz     float64 // band center from the carrier: 200 Hz plus half the width
	}{
		{"usb", 1200, 1700, 1600},
		{"lsb", 1700, 1200, -1600},
	} {
		t.Run(tc.demod, func(t *testing.T) {
			c := NewChain()
			frames := toFrames(ssbAir(1200, 1700, 1), testRate, sdr.DefaultBlockSize, 0)
			s := voiceSettings(tc.demod, 2800, -90)
			s.OffsetHz += int64(tc.centerHz)
			audio := collectAudio(c, frames, s)

			checkAudioLength(t, audio, 1)
			settled := audio[AudioRate/2:]
			want, reject := tonePower(settled, tc.want), tonePower(settled, tc.reject)
			if want < 1000*reject {
				t.Errorf("%.0f Hz power %.2e vs %.0f Hz %.2e, want the other sideband 30 dB down",
					tc.want, want, tc.reject, reject)
			}
			if p := peak(settled); p < 0.3 || p > 0.9 {
				t.Errorf("audio peak %.3f, want the AGC level", p)
			}
			if st := c.Status(); st.PassbandHz != 2800 {
				t.Errorf("passband %.0f Hz, want 2800: the spectrum draws that width", st.PassbandHz)
			}
		})
	}
}

// carrierAir — an unmodulated carrier at testOffset, as a CW beacon with the key down.
func carrierAir(seconds float64) []complex64 {
	clean := make([]complex64, int(seconds*testRate))
	for i := range clean {
		clean[i] = 1
	}
	return toAir(clean, testRate, 30)
}

// A band wider than the audio is narrowed: a carrier in the middle of the selected band
// must sound, not land above the audio filter.
func TestChain_SSBWideBandIsNarrowedToAudio(t *testing.T) {
	c := NewChain()
	frames := toFrames(carrierAir(1), testRate, sdr.DefaultBlockSize, 0)
	audio := collectAudio(c, frames, voiceSettings("lsb", 15_000, -90))

	checkAudioLength(t, audio, 1)
	settled := audio[AudioRate/2:]
	// 4 kHz band: the carrier in its middle is 200 Hz plus half the width from the suppressed one.
	tone := tonePower(settled, 2200)
	if other := tonePower(settled, 1500); tone < 1000*other {
		t.Errorf("2200 Hz power %.2e vs 1500 Hz %.2e, want the carrier heard as a 2200 Hz tone", tone, other)
	}
	if st := c.Status(); st.PassbandHz != 4000 {
		t.Errorf("passband %.0f Hz, want 4000: wider sideband does not fit the audio", st.PassbandHz)
	}
}

// CW: the carrier in the middle of the band sounds as the 700 Hz beat note.
func TestChain_CWCarrierAtCenterGivesTone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bw       uint32
		passband float64
	}{
		{"narrow", 500, 500},
		{"too wide", 15_000, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewChain()
			frames := toFrames(carrierAir(1), testRate, sdr.DefaultBlockSize, 0)
			audio := collectAudio(c, frames, voiceSettings("cw", tc.bw, -90))

			checkAudioLength(t, audio, 1)
			settled := audio[AudioRate/2:]
			tone := tonePower(settled, 700)
			if other := tonePower(settled, 1200); tone < 1000*other {
				t.Errorf("700 Hz power %.2e vs 1200 Hz %.2e, want the beat note", tone, other)
			}
			if p := peak(settled); p < 0.3 || p > 0.9 {
				t.Errorf("audio peak %.3f, want the AGC level", p)
			}
			st := c.Status()
			if !st.Supported {
				t.Error("cw not supported")
			}
			if st.PassbandHz != tc.passband {
				t.Errorf("passband %.0f Hz, want %.0f", st.PassbandHz, tc.passband)
			}
		})
	}
}

// SSTV tones can be heard while the picture is received.
func TestChain_SSTVAlsoGivesAudio(t *testing.T) {
	frames := toFrames(sstvAir(t, sstv.TestPattern(320, 240), sstv.Robot36, 30), sstvRate, 8192, 0)
	s := sdr.ChannelSettings{OffsetHz: 20_000, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodSSTV}
	c := NewChain()
	audio := collectAudio(c, frames[:len(frames)/4], s)

	var samples int
	for _, f := range frames[:len(frames)/4] {
		samples += len(f.Samples)
	}
	checkAudioLength(t, audio, float64(samples)/sstvRate)
	// The VIS leader is 1900 Hz.
	if p := peak(audio); p < 0.1 {
		t.Errorf("audio peak %.3f, want the SSTV tones", p)
	}
}

func TestChain_PacketProfileGivesNoAudio(t *testing.T) {
	c := NewChain()
	frames := toFrames(beaconAir(t, testRate, 1, time.Second), testRate, sdr.DefaultBlockSize, 0)
	if audio := collectAudio(c, frames, afskSettings(testOffset, -90)); len(audio) != 0 {
		t.Errorf("audio samples=%d, want none for a packet profile", len(audio))
	}
}

func peak(x []float32) float64 {
	m := 0.0
	for _, v := range x {
		m = max(m, math.Abs(float64(v)))
	}
	return m
}
