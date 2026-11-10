package signalclass

import (
	"math"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/modem"
)

const wideRate = 2_560_000.0

func TestExtractBandwidthAt_BroadcastForcesRDSWidth(t *testing.T) {
	got := ExtractBandwidthAt(104_100_000, 90_000)
	if got < wfmExtractMinHz {
		t.Fatalf("FM plan bw=%g, want ≥ %g", got, wfmExtractMinHz)
	}
	if ExtractBandwidthAt(145_900_000, 90_000) != 90_000 {
		t.Fatalf("outside FM plan must keep narrow extract")
	}
}

func TestExtract_RateAtLeastTwoAndHalfBandwidths(t *testing.T) {
	for _, bw := range []float64{1000, 12_000, 40_000, 200_000} {
		_, rate := Extract(make([]complex64, 1<<16), wideRate, 0, bw)
		if rate < 2.5*bw || rate < minSliceRate {
			t.Errorf("bandwidth %.0f: rate %.0f, want ≥ %.0f", bw, rate, max(2.5*bw, minSliceRate))
		}
		if rate > 2*max(2.5*bw, minSliceRate) {
			t.Errorf("bandwidth %.0f: rate %.0f, decimated too little", bw, rate)
		}
	}
}

func TestExtract_MovesOffsetToZero(t *testing.T) {
	const off = 300_000.0
	n := int(wideRate / 10)
	ch := modem.NewChannel(modem.ChannelConfig{
		SampleRate: wideRate, OffsetHz: func(time.Duration) float64 { return off },
	})
	ones := make([]complex64, n)
	for i := range ones {
		ones[i] = 1
	}
	out, rate := Extract(ch.Process(nil, ones), wideRate, off, 10_000)
	// After the shift the carrier is DC: phase does not rotate.
	var turn float64
	for i := 1; i < len(out); i++ {
		turn += math.Abs(phaseStep(out[i], out[i-1]))
	}
	if hz := turn / float64(len(out)-1) * rate / (2 * math.Pi); hz > 1 {
		t.Errorf("residual rotation %.1f Hz, want 0", hz)
	}
}

func TestExtractThenClassify_FSKFromWideStream(t *testing.T) {
	const off = -400_000.0
	n := int(wideRate / 2)
	m := modem.NewFSK(modem.FSKConfig{SampleRate: wideRate, Baud: 4800, DeviationHz: 2400, BT: 0.5})
	clean := m.Modulate(nil, prbs(int(float64(n)/wideRate*4800)+8))[:n]
	ch := modem.NewChannel(modem.ChannelConfig{
		SampleRate: wideRate, SNRdB: 20, NoiseBandwidthHz: 12_000, Seed: 21,
		OffsetHz: func(time.Duration) float64 { return off },
	})
	slice, rate := Extract(ch.Process(nil, clean), wideRate, off, 10_000)
	res := Classify(slice, rate)
	if res.Modulation != Mod2FSK || res.Params.Baud != 4800 {
		t.Errorf("got %q %.0f baud, want 2fsk 4800 (why: %s)", res.Modulation, res.Params.Baud, res.Why)
	}
}
