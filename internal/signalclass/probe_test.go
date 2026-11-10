package signalclass

import (
	"math"
	"math/cmplx"
	"math/rand/v2"
	"testing"

	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/signals"
)

func sweepIQ(rate, seconds float64) ([]complex64, *modem.Sweep) {
	sw := modem.NewSweep(rate, 125_000, 7)
	iq := make([]complex64, int(rate*seconds))
	for i := range iq {
		iq[i] = sw.Next()
	}
	return iq, sw
}

func fcchIQ(rate float64, at ...float64) []complex64 {
	out := make([]complex64, int(rate*0.2))
	w := 2 * math.Pi * fcchHz / rate
	for _, t0 := range at {
		start := int(t0 * rate)
		dur := int(0.00055 * rate)
		var phase float64
		for i := 0; i < dur && start+i < len(out); i++ {
			out[start+i] = complex64(cmplx.Rect(0.5, phase))
			phase += w
		}
	}
	return out
}

func TestProbeLoRa_NeedsUpAndDown(t *testing.T) {
	const rate = 500_000.0
	iq, sw := sweepIQ(rate, 0.15)
	if !ProbeLoRa(iq, rate, sw.BandwidthHz(), sw.SlopeHzS()) {
		t.Fatal("preamble did not match")
	}
	if ProbeLoRa(iq[:sw.SymbolSamples()], rate, sw.BandwidthHz(), sw.SlopeHzS()) {
		t.Fatal("one chirp matched")
	}
	noise := make([]complex64, len(iq))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range noise {
		noise[i] = complex(float32(rng.NormFloat64()), float32(rng.NormFloat64()))
	}
	if ProbeLoRa(noise, rate, sw.BandwidthHz(), sw.SlopeHzS()) {
		t.Fatal("noise matched")
	}
}

func TestProbeGSM_NeedsTwoTones(t *testing.T) {
	const rate = 500_000.0
	if !ProbeGSM(fcchIQ(rate, 0.02, 0.02+fcchGapS), rate) {
		t.Fatal("two FCCH tones did not match")
	}
	if ProbeGSM(fcchIQ(rate, 0.02), rate) {
		t.Fatal("one tone matched")
	}
	if ProbeGSM(fcchIQ(rate, 0.02, 0.03), rate) {
		t.Fatal("wrong gap matched")
	}
}

func TestResolve_LoRaAndGSM(t *testing.T) {
	const rate = 500_000.0
	iq, sw := sweepIQ(rate, 0.15)
	lora := Resolve(Result{
		Modulation: ModLFM,
		Params:     Params{OccupiedHz: sw.BandwidthHz(), ChirpHzS: sw.SlopeHzS()},
	}, signals.Input{CenterHz: 433_920_000}, iq, rate)
	if lora.Service != signals.IDLoRa || lora.ServiceBasis != BasisProbe || lora.ServiceLabel != "LoRa" {
		t.Fatalf("probe %+v", lora)
	}
	draft := Resolve(Result{
		Modulation: ModLFM,
		Params:     Params{OccupiedHz: 125_000, ChirpHzS: sw.SlopeHzS()},
	}, signals.Input{CenterHz: 868_100_000}, nil, 0)
	if draft.Service != signals.IDLoRa || draft.ServiceBasis != BasisSignature {
		t.Fatalf("draft %+v", draft)
	}

	tones := fcchIQ(rate, 0.02, 0.02+fcchGapS)
	gsm := Resolve(Result{Modulation: Mod2FSK, Params: Params{OccupiedHz: 200_000}},
		signals.Input{CenterHz: 940_000_000}, tones, rate)
	if gsm.Service != signals.IDGSM || gsm.ServiceBasis != BasisProbe {
		t.Fatalf("gsm %+v", gsm)
	}
	if miss := Resolve(Result{Modulation: Mod2FSK, Params: Params{OccupiedHz: 200_000}},
		signals.Input{CenterHz: 940_000_000}, fcchIQ(rate, 0.02), rate); miss.Service != "" {
		t.Fatalf("one tone set %q", miss.Service)
	}
	if out := Resolve(Result{Modulation: Mod2FSK, Params: Params{OccupiedHz: 200_000}},
		signals.Input{CenterHz: 437_000_000}, tones, rate); out.Service != "" {
		t.Fatalf("GSM probe outside the downlink set %q", out.Service)
	}
}
