package modem

import (
	"math"
	"math/cmplx"
	"testing"
	"time"
)

func ones(n int) []complex64 {
	out := make([]complex64, n)
	for i := range out {
		out[i] = 1
	}
	return out
}

func meanPower(x []complex64) float64 {
	sum := 0.0
	for _, v := range x {
		re, im := float64(real(v)), float64(imag(v))
		sum += re*re + im*im
	}
	return sum / float64(len(x))
}

func TestChannel_StaticOffsetShiftsFrequency(t *testing.T) {
	ch := NewChannel(ChannelConfig{
		SampleRate: testSampleRate,
		OffsetHz:   func(time.Duration) float64 { return 1000 },
	})
	freqs := instFreq(ch.Process(nil, ones(4800)))
	for i, f := range freqs {
		if math.Abs(f-1000) > 0.01 {
			t.Fatalf("freq[%d]=%.3f Hz, want 1000", i, f)
		}
	}
}

func TestChannel_OffsetFollowsTime(t *testing.T) {
	// Линейный уход частоты, как у Доплера вблизи кульминации: 2 кГц за секунду.
	ch := NewChannel(ChannelConfig{
		SampleRate: testSampleRate,
		OffsetHz:   func(t time.Duration) float64 { return 2000 * t.Seconds() },
	})
	freqs := instFreq(ch.Process(nil, ones(int(testSampleRate))))
	for _, n := range []int{100, 24000, 47000} {
		// freqs[n] — разность фаз отсчётов n и n+1, сдвиг взят в момент отсчёта n+1.
		want := 2000 * float64(n+1) / testSampleRate
		if math.Abs(freqs[n]-want) > 0.5 {
			t.Errorf("freq at sample %d=%.2f Hz, want %.2f", n, freqs[n], want)
		}
	}
}

func TestChannel_NoOffsetPassesThrough(t *testing.T) {
	ch := NewChannel(ChannelConfig{SampleRate: testSampleRate})
	in := []complex64{1, 1i, -1, complex(0.5, -0.5)}
	out := ch.Process(nil, in)
	for i := range in {
		if cmplx.Abs(complex128(out[i]-in[i])) > 1e-6 {
			t.Errorf("out[%d]=%v, want %v", i, out[i], in[i])
		}
	}
}

func TestChannel_StateContinuesAcrossCalls(t *testing.T) {
	cfg := ChannelConfig{
		SampleRate: testSampleRate,
		OffsetHz:   func(t time.Duration) float64 { return 500 + 1000*t.Seconds() },
	}
	in := ones(3000)

	whole := NewChannel(cfg).Process(nil, in)

	ch := NewChannel(cfg)
	parts := ch.Process(nil, in[:1234])
	parts = ch.Process(parts, in[1234:])

	for i := range whole {
		if cmplx.Abs(complex128(whole[i]-parts[i])) > 1e-5 {
			t.Fatalf("sample %d differs: %v vs %v", i, parts[i], whole[i])
		}
	}
}

func TestChannel_NoisePowerFromSNR(t *testing.T) {
	cases := []struct {
		name      string
		snrDB     float64
		bandwidth float64
		want      float64 // мощность шума во всей полосе дискретизации
	}{
		// Полоса SNR равна полосе дискретизации: мощность шума = 1 / SNR.
		{"full band 10 dB", 10, testSampleRate, 0.1},
		// Полоса в 10 раз уже: тот же SNR в канале даёт в 10 раз больше шума во всей полосе.
		{"narrow band 10 dB", 10, testSampleRate / 10, 1.0},
		{"full band 0 dB", 0, testSampleRate, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := NewChannel(ChannelConfig{
				SampleRate:       testSampleRate,
				SNRdB:            tc.snrDB,
				NoiseBandwidthHz: tc.bandwidth,
				Seed:             42,
			})
			// На входе тишина: на выходе только шум.
			got := meanPower(ch.Process(nil, make([]complex64, 200_000)))
			if errDB := 10 * math.Log10(got/tc.want); math.Abs(errDB) > 0.2 {
				t.Errorf("noise power=%.4f, want %.4f (error %.2f dB)", got, tc.want, errDB)
			}
		})
	}
}

func TestChannel_ZeroNoiseBandwidthMeansNoNoise(t *testing.T) {
	ch := NewChannel(ChannelConfig{SampleRate: testSampleRate, SNRdB: 10})
	if p := meanPower(ch.Process(nil, make([]complex64, 1000))); p != 0 {
		t.Errorf("power=%g, want 0", p)
	}
}

func TestChannel_SameSeedSameNoise(t *testing.T) {
	cfg := ChannelConfig{
		SampleRate:       testSampleRate,
		SNRdB:            5,
		NoiseBandwidthHz: 12500,
		Seed:             7,
	}
	a := NewChannel(cfg).Process(nil, make([]complex64, 100))
	b := NewChannel(cfg).Process(nil, make([]complex64, 100))
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs: %v vs %v", i, a[i], b[i])
		}
	}
}
