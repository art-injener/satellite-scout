package modem

import (
	"math/cmplx"
	"testing"
	"time"
)

// fskTestRate — channel rate for FSK 9600 after the channel filter: 6.67 samples per symbol.
const fskTestRate = 64_000

func modulatePRBSFSK(cfg FSKConfig, nbits int) []complex64 {
	return NewFSK(cfg).Modulate(nil, prbs9Bits(nbits))
}

func TestFSKDemod_CleanSignalNoErrors(t *testing.T) {
	const nbits = 20000
	cfg := FSK9600(fskTestRate)
	bits := NewFSKDemod(cfg).Demodulate(nil, modulatePRBSFSK(cfg, nbits))

	if d := len(bits) - nbits; d < -3 || d > 3 {
		t.Errorf("demodulated %d bits, want %d±3", len(bits), nbits)
	}
	if c := checkBits(t, bits); c.Errors() != 0 {
		t.Errorf("errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
}

func TestFSKDemod_MSK9600CleanSignalNoErrors(t *testing.T) {
	const nbits = 20000
	cfg := MSK9600(fskTestRate)
	bits := NewFSKDemod(cfg).Demodulate(nil, modulatePRBSFSK(cfg, nbits))
	if d := len(bits) - nbits; d < -3 || d > 3 {
		t.Errorf("demodulated %d bits, want %d±3", len(bits), nbits)
	}
	if c := checkBits(t, bits); c.Errors() != 0 {
		t.Errorf("errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
}

func TestFSKDemod_FractionalSamplesPerSymbol(t *testing.T) {
	// 50000 / 9600 = 5.21 samples per symbol.
	cfg := FSK9600(50_000)
	bits := NewFSKDemod(cfg).Demodulate(nil, modulatePRBSFSK(cfg, 20000))
	if c := checkBits(t, bits); c.Errors() != 0 {
		t.Errorf("errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
}

func TestFSKDemod_TracksTransmitterClockOffset(t *testing.T) {
	for _, baud := range []float64{9590.4, 9609.6} { // ±0.1%
		t.Run("", func(t *testing.T) {
			tx := FSK9600(fskTestRate)
			tx.Baud = baud
			bits := NewFSKDemod(FSK9600(fskTestRate)).Demodulate(nil, modulatePRBSFSK(tx, 40000))
			if c := checkBits(t, bits); c.Errors() != 0 {
				t.Errorf("tx %.1f Bd: errors=%d of %d, want 0", baud, c.Errors(), c.Bits())
			}
		})
	}
}

// A carrier offset shifts the discriminator output by offset/deviation: half the deviation
// here, so without tracking the decision threshold the eye is closed on one side.
func TestFSKDemod_TracksResidualCarrierOffset(t *testing.T) {
	for _, offset := range []float64{-1500, 1500} {
		t.Run("", func(t *testing.T) {
			cfg := FSK9600(fskTestRate)
			ch := NewChannel(ChannelConfig{
				SampleRate: fskTestRate,
				OffsetHz:   func(time.Duration) float64 { return offset },
			})
			iq := ch.Process(nil, modulatePRBSFSK(cfg, 20000))
			bits := NewFSKDemod(cfg).Demodulate(nil, iq)
			if c := checkBits(t, bits); c.Errors() != 0 {
				t.Errorf("offset %+.0f Hz: errors=%d of %d, want 0", offset, c.Errors(), c.Bits())
			}
		})
	}
}

// A burst far outside the deviation (filter start-up, interference) drives the decision
// levels off the eye: the demodulator must come back once the signal appears.
func TestFSKDemod_RecoversAfterOffDeviationBurst(t *testing.T) {
	const burstSymbols, settleBits = 100, 300
	for _, burstHz := range []float64{-12000, 12000} { // ±4 deviations
		t.Run("", func(t *testing.T) {
			cfg := FSK9600(fskTestRate)
			n := int(burstSymbols * fskTestRate / cfg.Baud)
			iq := make([]complex64, n)
			for i := range iq {
				iq[i] = complex64(cmplx.Rect(1, twoPi*burstHz*float64(i)/fskTestRate))
			}
			iq = append(iq, modulatePRBSFSK(cfg, 5000)...)
			bits := NewFSKDemod(cfg).Demodulate(nil, iq)
			if c := checkBits(t, bits[burstSymbols+settleBits:]); c.Errors() != 0 {
				t.Errorf("burst %+.0f Hz: errors=%d of %d, want 0", burstHz, c.Errors(), c.Bits())
			}
		})
	}
}

func TestFSKDemod_StateContinuesAcrossCalls(t *testing.T) {
	cfg := FSK9600(fskTestRate)
	iq := modulatePRBSFSK(cfg, 3000)

	whole := NewFSKDemod(cfg).Demodulate(nil, iq)

	d := NewFSKDemod(cfg)
	parts := d.Demodulate(nil, iq[:7777])
	parts = d.Demodulate(parts, iq[7777:])

	if len(parts) != len(whole) {
		t.Fatalf("len=%d, want %d", len(parts), len(whole))
	}
	for i := range whole {
		if parts[i] != whole[i] {
			t.Fatalf("bit %d differs: %d vs %d", i, parts[i], whole[i])
		}
	}
}

func TestFSKDemod_SilenceGivesNoLock(t *testing.T) {
	bits := NewFSKDemod(FSK9600(fskTestRate)).Demodulate(nil, make([]complex64, fskTestRate))
	var c PRBS9Checker
	c.Push(bits)
	if c.Locked() {
		t.Error("checker locked on demodulated silence")
	}
}

func TestFSKDemod_ShortPacketAfterNoisyFMClicks(t *testing.T) {
	// Strong off-deviation FM clicks before a short 9600 FSK packet inflate unclamped high/low
	// and shift threshold so the short 24-bit preamble is burned on recovery.
	cfg := FSK9600(fskTestRate)
	noiseSamples := int(0.1 * fskTestRate)
	pktBits := 960 // 100 ms at 9600 baud
	pktIQ := modulatePRBSFSK(cfg, pktBits)

	ch := NewChannel(ChannelConfig{
		SampleRate: fskTestRate, SNRdB: 16, NoiseBandwidthHz: 18_000, Seed: 101,
	})
	iq := make([]complex64, noiseSamples+len(pktIQ))
	// Simulated FM clicks (+24 kHz = +8 deviations) right before packet start.
	for i := noiseSamples - 200; i < noiseSamples; i++ {
		iq[i] = complex64(cmplx.Rect(0.6, twoPi*24_000*float64(i)/fskTestRate))
	}
	copy(iq[noiseSamples:], pktIQ)
	noisy := ch.Process(nil, iq)

	bits := NewFSKDemod(cfg).Demodulate(nil, noisy)
	pktStart := int(float64(noiseSamples) * cfg.Baud / fskTestRate)
	const preambleAllowance = 24
	if len(bits) <= pktStart+preambleAllowance+200 {
		t.Fatalf("too few bits %d for packet starting at %d", len(bits), pktStart)
	}
	tail := bits[pktStart+preambleAllowance:]
	var c PRBS9Checker
	c.Push(tail)
	if !c.Locked() || c.Errors() != 0 {
		t.Errorf("short packet after noisy FM clicks: locked=%v bits=%d errors=%d, want 0 after 24-bit preamble",
			c.Locked(), c.Bits(), c.Errors())
	}
}

func TestFSKDemod_TracksLargeCarrierOffset(t *testing.T) {
	cases := []struct {
		name   string
		cfg    FSKConfig
		offset float64
	}{
		{"9600/+4kHz", FSK9600(fskTestRate), 4000},
		{"9600/-4kHz", FSK9600(fskTestRate), -4000},
		{"4800/+4kHz", FSK4800(fskTestRate), 4000},
		{"4800/-4kHz", FSK4800(fskTestRate), -4000},
		{"2400/+4kHz", GMSK2400(fskTestRate), 4000},
		{"2400/-4kHz", GMSK2400(fskTestRate), -4000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Short burst (~250 ms) with residual carrier offset right after channel noise.
			noiseSamples := int(0.2 * fskTestRate)
			pktBits := max(int(0.25*tc.cfg.Baud), 600)
			pktIQ := modulatePRBSFSK(tc.cfg, pktBits)
			ch := NewChannel(ChannelConfig{
				SampleRate: fskTestRate, SNRdB: 18, NoiseBandwidthHz: 18_000, Seed: 107,
				OffsetHz: func(time.Duration) float64 { return tc.offset },
			})
			iq := make([]complex64, noiseSamples+len(pktIQ))
			copy(iq[noiseSamples:], pktIQ)
			noisy := ch.Process(nil, iq)

			bits := NewFSKDemod(tc.cfg).Demodulate(nil, noisy)
			pktStart := int(float64(noiseSamples) * tc.cfg.Baud / fskTestRate)
			const preambleAllowance = 24
			if len(bits) <= pktStart+preambleAllowance+150 {
				t.Fatalf("too few bits %d", len(bits))
			}
			tail := bits[pktStart+preambleAllowance:]
			var c PRBS9Checker
			c.Push(tail)
			if !c.Locked() || c.Errors() != 0 {
				t.Errorf("%s: locked=%v bits=%d errors=%d, want 0 errors immediately after 24-bit preamble",
					tc.name, c.Locked(), c.Bits(), c.Errors())
			}
		})
	}
}
