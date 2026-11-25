package demod

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

func TestEstimateBurstSpectrum_Shifted2FSK(t *testing.T) {
	const midRate = 64_000.0
	for _, shiftHz := range []float64{+4500, -3500} {
		// 2-FSK burst at 9600 Bd shifted by shiftHz inside the 64 kHz coarse band.
		bits := make([]byte, 600)
		rng := rand.New(rand.NewPCG(7, 11))
		for i := range bits {
			bits[i] = byte(rng.IntN(2))
		}
		cfg := modem.FSK9600(midRate)
		iq := modem.NewFSK(cfg).Modulate(nil, bits)
		ch := modem.NewChannel(modem.ChannelConfig{
			SampleRate:       midRate,
			OffsetHz:         func(time.Duration) float64 { return shiftHz },
			SNRdB:            18,
			NoiseBandwidthHz: 15_000,
			Seed:             19,
		})
		noisy := ch.Process(nil, iq)
		centerHz, widthHz, ok := estimateBurstSpectrum(noisy, midRate)
		if !ok {
			t.Fatalf("shift %.0f Hz: estimateBurstSpectrum returned ok=false", shiftHz)
		}
		if math.Abs(centerHz-shiftHz) > 600 {
			t.Errorf("shift %.0f Hz: center=%.0f Hz, want within ±600 Hz", shiftHz, centerHz)
		}
		if widthHz < 5_000 || widthHz > 20_000 {
			t.Errorf("shift %.0f Hz: width=%.0f Hz, want in [5000, 20000]", shiftHz, widthHz)
		}
	}
}

func TestAutoPicker_BurstBufferDecodesOffCenterFSK9600Geoscan(t *testing.T) {
	// Beacon sits at testOffset; tuning the channel 8.5 kHz lower puts the burst at +8.5 kHz.
	// With 18 kHz fine passband (±9 kHz) the upper FSK tone (+13.3 kHz) is cut off so the
	// streaming path fails, while the 64 kHz coarse burst buffer preserves both tones,
	// recenters to 0 Hz and decodes GEOSCAN with CRC.
	air := beaconAirOf(t, testRate, 1, 1500*time.Millisecond, "fsk9600_geoscan")
	full := append(noiseAir(t, testRate, 300*time.Millisecond), air...)

	picker := NewAutoPicker()
	picker.ConfigurePlan("geoscan-off", CandidatePlan{
		Initial: []string{"fsk9600_geoscan", "fsk9600_g3ruh"},
	})
	cs := sdr.ChannelSettings{
		OffsetHz:     testOffset - 8_500,
		BandwidthHz:  sdr.MaxChannelBandwidthHz,
		SquelchDBFS:  -90,
		SquelchAddDB: sdr.DefaultSquelchAddDB,
		Demod:        DemodAuto,
	}
	var got []taggedPacket
	var carrierHz float64
	for _, f := range toFrames(full, testRate, sdr.DefaultBlockSize, 0) {
		pkts, st := picker.Process(f, cs, 0)
		if len(pkts) > 0 && st.Measured {
			carrierHz = st.CarrierHz
		}
		got = append(got, pkts...)
	}
	if len(got) == 0 {
		t.Fatal("expected decoded GEOSCAN packet from recentered burst buffer at +8.5 kHz offset")
	}
	if got[0].Profile != "fsk9600_geoscan" {
		t.Errorf("profile = %q, want fsk9600_geoscan", got[0].Profile)
	}
	if picker.Locked() != "fsk9600_geoscan" {
		t.Errorf("Locked() = %q, want fsk9600_geoscan", picker.Locked())
	}
	if math.Abs(carrierHz-8_500) > 1_000 {
		t.Errorf("reported carrierHz = %.0f, want near +8500 (proving burst-buffer path decoded it)", carrierHz)
	}
}

func TestAutoPicker_BurstBufferDecodesOffCenterBPSK1200(t *testing.T) {
	// BPSK 1200 shifted by -3 kHz: narrow Costas loop misses it in streaming path.
	// First 2 bursts lock bpsk1200 via burst buffer, 3rd burst proves pinned mode also uses burst buffer.
	air := beaconAirOf(t, testRate, 3, 1500*time.Millisecond, "bpsk1200")
	full := append(noiseAir(t, testRate, 300*time.Millisecond), air...)

	picker := NewAutoPicker()
	picker.ConfigurePlan("bpsk-off", CandidatePlan{
		Initial: []string{"bpsk1200"},
	})
	cs := sdr.ChannelSettings{
		OffsetHz:     testOffset + 3_000,
		BandwidthHz:  15_000,
		SquelchDBFS:  -90,
		SquelchAddDB: sdr.DefaultSquelchAddDB,
		Demod:        DemodAuto,
	}
	var got []taggedPacket
	for _, f := range toFrames(full, testRate, sdr.DefaultBlockSize, 0) {
		pkts, _ := picker.Process(f, cs, 0)
		got = append(got, pkts...)
	}
	if len(got) < 3 {
		t.Fatalf("got %d BPSK1200 packets at -3 kHz offset, want 3 (2 during search + 1 after lock)", len(got))
	}
	if picker.Locked() != "bpsk1200" {
		t.Errorf("Locked() = %q, want bpsk1200", picker.Locked())
	}
}

func TestAutoPicker_BurstBufferNoDuplicateWhenStreamDecoded(t *testing.T) {
	air := beaconAirOf(t, testRate, 1, 1500*time.Millisecond, "fsk9600_geoscan")
	full := append(noiseAir(t, testRate, 300*time.Millisecond), air...)

	picker := NewAutoPicker()
	picker.ConfigurePlan("geoscan-center", CandidatePlan{
		Initial: []string{"fsk9600_geoscan"},
	})
	cs := sdr.ChannelSettings{
		OffsetHz:    testOffset,
		BandwidthHz: sdr.MaxChannelBandwidthHz,
		SquelchDBFS: -90,
		Demod:       DemodAuto,
	}
	var got []taggedPacket
	for _, f := range toFrames(full, testRate, sdr.DefaultBlockSize, 0) {
		pkts, _ := picker.Process(f, cs, 0)
		got = append(got, pkts...)
	}
	if len(got) != 1 {
		t.Fatalf("got %d packets on single centered burst, want exactly 1 (no duplicates)", len(got))
	}
}

func TestAutoPicker_BurstBufferCapsAtThreeSeconds(t *testing.T) {
	picker := NewAutoPicker()
	picker.ConfigurePlan("long-tone", CandidatePlan{
		Initial: []string{"fsk9600_geoscan"},
	})
	cs := voiceSettings(DemodAuto, sdr.MaxChannelBandwidthHz, -90)
	air := fmToneAir(4) // 4 seconds continuous signal with no valid frames
	maxSamples := int(burstMaxDuration.Seconds()*midRateHz) + 4096
	for _, f := range toFrames(air, testRate, sdr.DefaultBlockSize, 0) {
		picker.Process(f, cs, 0)
		if len(picker.burst.samples) > maxSamples {
			t.Fatalf("burst buffer grew to %d samples, exceeds 3 s cap (%d)", len(picker.burst.samples), maxSamples)
		}
	}
}

func noiseAir(t *testing.T, rate float64, dur time.Duration) []complex64 {
	t.Helper()
	clean := make([]complex64, int(dur.Seconds()*rate))
	ch := modem.NewChannel(modem.ChannelConfig{
		SampleRate:       rate,
		OffsetHz:         func(time.Duration) float64 { return testOffset },
		SNRdB:            testSNR,
		NoiseBandwidthHz: 15_000,
		Seed:             17,
	})
	air := ch.Process(nil, clean)
	gain := complex(float32(math.Pow(10, testLevel/20)), 0)
	for i := range air {
		air[i] *= gain
	}
	return air
}
