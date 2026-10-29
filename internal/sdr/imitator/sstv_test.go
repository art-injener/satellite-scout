package imitator

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sstv"
)

const sstvTestRate = 32_000

func sstvSignal(period time.Duration) Signal {
	return Signal{Kind: SignalSSTV, FrequencyHz: 436_000_000, SSTVMode: sstv.Robot36, Period: period}
}

// audioTone estimates the audio frequency between t0 and t1 seconds by zero crossings.
func audioTone(audio []float32, rate, t0, t1 float64) float64 {
	first, last, n := -1.0, -1.0, 0
	for i := int(t0*rate) + 1; i < int(t1*rate) && i < len(audio); i++ {
		if audio[i-1] < 0 && audio[i] >= 0 {
			t := float64(i-1) + float64(-audio[i-1]/(audio[i]-audio[i-1]))
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

func TestSSTVGen_TransmitsImageThenPausesUntilPeriod(t *testing.T) {
	d := sstv.Robot36.Duration()
	period := d + 3*time.Second
	g, err := newSSTVGen(sstvSignal(period), sstvTestRate)
	if err != nil {
		t.Fatal(err)
	}
	// Two periods in blocks of an awkward size.
	iq := make([]complex64, int(2*period.Seconds()*sstvTestRate))
	for i := 0; i < len(iq); i += 5_000 {
		g.fillClean(iq[i:min(i+5_000, len(iq))])
	}

	audio := modem.NewFMAudioDemod(modem.FMAudioConfig{
		SampleRate: sstvTestRate, DeviationHz: SSTVDeviationHz, AudioRate: sstvTestRate,
	}).Demodulate(nil, iq)
	for _, start := range []float64{0, period.Seconds()} {
		if f := audioTone(audio, sstvTestRate, start+0.02, start+0.28); math.Abs(f-sstv.LeaderHz) > 10 {
			t.Errorf("transmission at %.1f s: leader %.0f Hz, want %d", start, f, sstv.LeaderHz)
		}
	}
	// The pause carries no signal.
	pause := iq[int((d.Seconds()+1)*sstvTestRate):int((d.Seconds()+2)*sstvTestRate)]
	for i, s := range pause {
		if s != 0 {
			t.Fatalf("pause sample %d = %v, want silence", i, s)
		}
	}
	// The transmission is a constant-envelope FM signal.
	for _, s := range iq[1000:2000] {
		if a := math.Hypot(float64(real(s)), float64(imag(s))); math.Abs(a-1) > 1e-3 {
			t.Fatalf("transmission envelope %.4f, want 1", a)
		}
	}
}

func TestSSTVGen_RequiresMode(t *testing.T) {
	sig := sstvSignal(time.Minute)
	sig.SSTVMode = sstv.Mode{}
	if _, err := newSignalSource(sig, sstvTestRate); !errors.Is(err, ErrInvalidSignal) {
		t.Errorf("err=%v, want ErrInvalidSignal without a mode", err)
	}
}

func TestSSTVGen_TransmitsRecordedAudio(t *testing.T) {
	// A quiet 1 s recording of 1900 Hz at a sound card rate.
	const rate = 11_025
	rec := make([]float32, rate)
	for i := range rec {
		rec[i] = 0.3 * float32(math.Sin(2*math.Pi*1900*float64(i)/rate))
	}
	sig := Signal{Kind: SignalSSTV, FrequencyHz: 436_000_000, Period: 2 * time.Second, Audio: rec, AudioRate: rate}
	g, err := newSignalSource(sig, sstvTestRate)
	if err != nil {
		t.Fatal(err)
	}
	iq := make([]complex64, 4*sstvTestRate)
	for i := 0; i < len(iq); i += 3_000 {
		g.gen.fillClean(iq[i:min(i+3_000, len(iq))])
	}
	audio := modem.NewFMAudioDemod(modem.FMAudioConfig{
		SampleRate: sstvTestRate, DeviationHz: SSTVDeviationHz, AudioRate: sstvTestRate,
	}).Demodulate(nil, iq)
	for _, start := range []float64{0, 2} {
		if f := audioTone(audio, sstvTestRate, start+0.1, start+0.9); math.Abs(f-1900) > 5 {
			t.Errorf("recording at %.0f s: %.0f Hz, want 1900", start, f)
		}
	}
	// The recording is scaled to the full deviation.
	var peak float32
	for _, a := range audio[sstvTestRate/10 : sstvTestRate*9/10] {
		peak = max(peak, a)
	}
	if peak < 0.9 || peak > 1.1 {
		t.Errorf("audio peak %.2f, want the recording at full deviation", peak)
	}
	if s := iq[int(1.5*sstvTestRate)]; s != 0 {
		t.Errorf("pause sample %v, want silence", s)
	}
}
