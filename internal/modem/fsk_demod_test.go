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
