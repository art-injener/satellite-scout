package modem

import (
	"math"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
)

// End-to-end receive chain for FSK 9600 G3RUH on the imitator: PRBS-9 through the G3RUH
// scrambler, GFSK at the SDR rate with offset, Doppler and noise; then Doppler removal,
// two-stage channel filter (64 kHz, no decimation in the fine stage), demodulator,
// descrambler, BER.
const (
	fskChainBits      = 20000
	fskChainBandwidth = 19_200.0 // channel filter cutoff ±9.6 kHz at 64 kHz
)

func runFSKReceiveChain(t *testing.T, snrDB float64) *PRBS9Checker {
	t.Helper()
	var s G3RUHScrambler
	line := s.Scramble(nil, prbs9Bits(fskChainBits))
	ch := NewChannel(ChannelConfig{
		SampleRate:       chainSampleRate,
		OffsetHz:         func(t time.Duration) float64 { return chainOffsetHz + chainDoppler(t) },
		SNRdB:            snrDB,
		NoiseBandwidthHz: fskChainBandwidth,
		Seed:             12,
	})
	air := ch.Process(nil, NewFSK(FSK9600(chainSampleRate)).Modulate(nil, line))
	gain := complex(float32(math.Pow(10, chainLevelDBFS/20)), 0)
	for i := range air {
		air[i] *= gain
	}

	const decim = 16
	const midRate = chainSampleRate / decim
	coarse := dsp.NewXlatingFIR(chainOffsetHz, chainSampleRate, decim,
		dsp.DesignLowpass(0.4*midRate, chainSampleRate, 257, dsp.WindowBlackmanHarris))
	coarse.SetShiftFunc(func(t time.Duration) float64 { return chainOffsetHz + chainDoppler(t) })
	fine := dsp.NewXlatingFIR(0, midRate, 1,
		dsp.DesignLowpass(fskChainBandwidth/2, midRate, 255, dsp.WindowBlackmanHarris))
	demod := NewFSKDemod(FSK9600(midRate))
	var d G3RUHDescrambler

	var bits []byte
	for start := 0; start < len(air); start += chainBlock {
		block := fine.Process(coarse.Process(air[start:min(start+chainBlock, len(air))]))
		bits = d.Descramble(bits, demod.Demodulate(nil, block))
	}
	return checkBits(t, bits)
}

func TestFSKReceiveChain_NoErrorsAtGoodSNR(t *testing.T) {
	c := runFSKReceiveChain(t, 14)
	if c.Errors() != 0 {
		t.Errorf("SNR 14 dB: errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
	if c.Bits() < fskChainBits-2*demodSettleBits {
		t.Errorf("checked %d bits of %d sent", c.Bits(), fskChainBits)
	}
}

// Errors after the descrambler come in threes; BER 1e-2 is the edge of usable reception
// (an AX.25 frame of 300 bytes mostly fails its CRC).
func TestFSKReceiveChain_BERGrowsAsSNRFalls(t *testing.T) {
	var prev float64
	for _, snr := range []float64{12, 10, 8} {
		c := runFSKReceiveChain(t, snr)
		ber := c.BER()
		t.Logf("SNR %v dB: BER %.4f (%d/%d)", snr, ber, c.Errors(), c.Bits())
		if ber <= prev {
			t.Errorf("SNR %v dB: BER %.4f not above %.4f at higher SNR", snr, ber, prev)
		}
		if snr >= 10 && ber > 0.005 {
			t.Errorf("SNR %v dB: BER %.4f above 0.005", snr, ber)
		}
		prev = ber
	}
}
