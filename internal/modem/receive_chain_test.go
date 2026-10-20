package modem

import (
	"math"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
)

// End-to-end receive chain on the imitator: AFSK 1200 with PRBS-9 at the SDR rate,
// shifted from the tuning point, Doppler with drift and noise with SNR in the channel
// band; then Doppler removal, two-stage channel filter, squelch, demodulator, BER.
const (
	chainSampleRate = 1_024_000.0
	chainOffsetHz   = 50_000.0 // transmitter offset from the receiver tuning
	chainBandwidth  = 15_000.0 // «Полоса канала» by default
	chainBlock      = 1 << 16  // SDR block size fed to the chain
	chainBits       = 2400
	chainLevelDBFS  = -40.0 // signal level at the receiver input, as the imitator at mid-pass
)

// chainDoppler is a pass segment: +8 kHz falling at 200 Hz/s.
func chainDoppler(t time.Duration) float64 {
	return 8000 - 200*t.Seconds()
}

type chainResult struct {
	bits    []byte
	levelDB float32 // channel level of the last block
}

func runReceiveChain(t *testing.T, snrDB, squelchDBFS float64) chainResult {
	t.Helper()
	ch := NewChannel(ChannelConfig{
		SampleRate:       chainSampleRate,
		OffsetHz:         func(t time.Duration) float64 { return chainOffsetHz + chainDoppler(t) },
		SNRdB:            snrDB,
		NoiseBandwidthHz: chainBandwidth,
		Seed:             11,
	})
	air := ch.Process(nil, modulatePRBS(AFSK1200(chainSampleRate), chainBits))
	// Channel SNR is relative to a unit signal: scale signal and noise together.
	gain := complex(float32(math.Pow(10, chainLevelDBFS/20)), 0)
	for i := range air {
		air[i] *= gain
	}

	coarse := dsp.NewXlatingFIR(chainOffsetHz, chainSampleRate, 16,
		dsp.DesignLowpass(20_000, chainSampleRate, 127, dsp.WindowBlackmanHarris))
	coarse.SetShiftFunc(func(t time.Duration) float64 { return chainOffsetHz + chainDoppler(t) })
	const midRate = chainSampleRate / 16
	fine := dsp.NewXlatingFIR(0, midRate, 2, dsp.DesignLowpass(chainBandwidth/2, midRate, 255, dsp.WindowBlackmanHarris))
	sq := dsp.Squelch{ThresholdDBFS: squelchDBFS}
	demod := NewAFSKDemod(AFSK1200(midRate / 2))

	var res chainResult
	for start := 0; start < len(air); start += chainBlock {
		block := fine.Process(coarse.Process(air[start:min(start+chainBlock, len(air))]))
		if sq.Open(block) {
			res.bits = demod.Demodulate(res.bits, block)
		}
	}
	res.levelDB = sq.LevelDBFS()
	return res
}

func TestReceiveChain_NoErrorsAtGoodSNR(t *testing.T) {
	r := runReceiveChain(t, 12, -90)
	c := checkBits(t, r.bits)
	if c.Errors() != 0 {
		t.Errorf("SNR 12 dB: errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
	if c.Bits() < chainBits-2*demodSettleBits {
		t.Errorf("checked %d bits of %d sent", c.Bits(), chainBits)
	}
}

func TestReceiveChain_BERGrowsAsSNRFalls(t *testing.T) {
	var prev float64
	for _, snr := range []float64{8, 6, 4} {
		c := checkBits(t, runReceiveChain(t, snr, -90).bits)
		ber := c.BER()
		t.Logf("SNR %v dB: BER %.4f (%d/%d)", snr, ber, c.Errors(), c.Bits())
		if ber <= prev {
			t.Errorf("SNR %v dB: BER %.4f not above %.4f at higher SNR", snr, ber, prev)
		}
		prev = ber
	}
}

func TestReceiveChain_SquelchGatesDemodulator(t *testing.T) {
	open := runReceiveChain(t, 12, chainLevelDBFS-10)
	if len(open.bits) < chainBits/2 {
		t.Errorf("squelch %v dBFS below signal: demodulated %d bits", chainLevelDBFS-10, len(open.bits))
	}
	// Signal plus noise at 12 dB SNR sits ~0.3 dB above the signal level.
	if d := float64(open.levelDB) - chainLevelDBFS; d < 0 || d > 0.6 {
		t.Errorf("channel level=%.2f dBFS, want %.1f..%.1f", open.levelDB, chainLevelDBFS, chainLevelDBFS+0.6)
	}

	closed := runReceiveChain(t, 12, chainLevelDBFS+10)
	if len(closed.bits) != 0 {
		t.Errorf("squelch %v dBFS above signal: demodulated %d bits, want 0", chainLevelDBFS+10, len(closed.bits))
	}
}
