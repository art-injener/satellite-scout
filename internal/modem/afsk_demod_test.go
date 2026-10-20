package modem

import (
	"testing"
	"time"
)

// demodSettleBits — bits skipped before BER counting while the symbol clock locks.
const demodSettleBits = 100

func modulatePRBS(cfg AFSKConfig, nbits int) []complex64 {
	return NewAFSK(cfg).Modulate(nil, prbs9Bits(nbits))
}

func checkBits(t *testing.T, bits []byte) *PRBS9Checker {
	t.Helper()
	if len(bits) <= demodSettleBits {
		t.Fatalf("demodulated %d bits, too few", len(bits))
	}
	var c PRBS9Checker
	c.Push(bits[demodSettleBits:])
	if !c.Locked() {
		t.Fatalf("PRBS-9 checker not locked after %d bits", len(bits))
	}
	return &c
}

func TestAFSKDemod_CleanSignalNoErrors(t *testing.T) {
	const nbits = 5000
	cfg := AFSK1200(testSampleRate)
	bits := NewAFSKDemod(cfg).Demodulate(nil, modulatePRBS(cfg, nbits))

	if d := len(bits) - nbits; d < -2 || d > 2 {
		t.Errorf("demodulated %d bits, want %d±2", len(bits), nbits)
	}
	if c := checkBits(t, bits); c.Errors() != 0 {
		t.Errorf("errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
}

func TestAFSKDemod_FractionalSamplesPerSymbol(t *testing.T) {
	// 50000 / 1200 = 41.67 samples per symbol.
	cfg := AFSK1200(50000)
	bits := NewAFSKDemod(cfg).Demodulate(nil, modulatePRBS(cfg, 5000))
	if c := checkBits(t, bits); c.Errors() != 0 {
		t.Errorf("errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
}

func TestAFSKDemod_TracksTransmitterClockOffset(t *testing.T) {
	for _, baud := range []float64{1198.8, 1201.2} { // ±0.1%
		t.Run("", func(t *testing.T) {
			tx := AFSK1200(testSampleRate)
			tx.Baud = baud
			bits := NewAFSKDemod(AFSK1200(testSampleRate)).Demodulate(nil, modulatePRBS(tx, 10000))
			if c := checkBits(t, bits); c.Errors() != 0 {
				t.Errorf("tx %.1f Bd: errors=%d of %d, want 0", baud, c.Errors(), c.Bits())
			}
		})
	}
}

func TestAFSKDemod_ToleratesResidualCarrierOffset(t *testing.T) {
	for _, offset := range []float64{-1000, 1000} {
		t.Run("", func(t *testing.T) {
			cfg := AFSK1200(testSampleRate)
			ch := NewChannel(ChannelConfig{
				SampleRate: testSampleRate,
				OffsetHz:   func(time.Duration) float64 { return offset },
			})
			iq := ch.Process(nil, modulatePRBS(cfg, 5000))
			bits := NewAFSKDemod(cfg).Demodulate(nil, iq)
			if c := checkBits(t, bits); c.Errors() != 0 {
				t.Errorf("offset %+.0f Hz: errors=%d of %d, want 0", offset, c.Errors(), c.Bits())
			}
		})
	}
}

func TestAFSKDemod_StateContinuesAcrossCalls(t *testing.T) {
	cfg := AFSK1200(testSampleRate)
	iq := modulatePRBS(cfg, 1000)

	whole := NewAFSKDemod(cfg).Demodulate(nil, iq)

	d := NewAFSKDemod(cfg)
	parts := d.Demodulate(nil, iq[:12345])
	parts = d.Demodulate(parts, iq[12345:])

	if len(parts) != len(whole) {
		t.Fatalf("len=%d, want %d", len(parts), len(whole))
	}
	for i := range whole {
		if parts[i] != whole[i] {
			t.Fatalf("bit %d differs: %d vs %d", i, parts[i], whole[i])
		}
	}
}

func TestAFSKDemod_SilenceGivesNoLock(t *testing.T) {
	bits := NewAFSKDemod(AFSK1200(testSampleRate)).Demodulate(nil, make([]complex64, 48000))
	var c PRBS9Checker
	c.Push(bits)
	if c.Locked() {
		t.Error("checker locked on demodulated silence")
	}
}
