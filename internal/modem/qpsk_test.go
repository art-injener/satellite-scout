package modem

import (
	"math"
	"testing"
	"time"
)

func TestQPSK_CleanPRBSLowBER(t *testing.T) {
	cfg := QPSKCCSDS(64_000)
	nbits := 4000
	if nbits%2 != 0 {
		nbits++
	}
	want := prbs9Bits(nbits)
	bits := NewQPSKDemod(cfg).Demodulate(nil, NewQPSK(cfg).Modulate(nil, want))
	var bestOff, bestErr int
	bestErr = len(bits)
	for off := 0; off < 8 && off < len(bits); off += 2 {
		err, n := 0, 0
		for i := off; i < len(bits) && i-off < len(want); i++ {
			if bits[i] != want[i-off] {
				err++
			}
			n++
		}
		if n > 500 && err < bestErr {
			bestErr, bestOff = err, off
		}
	}
	n := len(bits) - bestOff
	if n > len(want) {
		n = len(want)
	}
	ber := float64(bestErr) / float64(n)
	t.Logf("ber=%.4f off=%d n=%d", ber, bestOff, n)
	if ber > 0.05 {
		t.Errorf("BER %.4f too high for clean QPSK", ber)
	}
}

func TestQPSK_LowLevelRTLSDR(t *testing.T) {
	cfg := QPSKCCSDS(64_000)
	want := prbs9Bits(4000)
	iq := NewQPSK(cfg).Modulate(nil, want)
	ch := NewChannel(ChannelConfig{
		SampleRate: cfg.SampleRate,
		OffsetHz:   func(time.Duration) float64 { return 120 },
	})
	iq = ch.Process(nil, iq)
	// -50 dBFS amplitude (~0.00316).
	scale := float32(math.Pow(10, -50.0/20.0))
	for i, s := range iq {
		iq[i] = complex(real(s)*scale, imag(s)*scale)
	}
	bits := NewQPSKDemod(cfg).Demodulate(nil, iq)
	// Skip initial Costas acquisition (first 400 bits) and check all 4 QPSK phase ambiguities.
	const skip = 400
	bestBER := 1.0
	for rot := range 4 {
		for off := 0; off < 8 && skip+off < len(bits); off += 2 {
			errs, n := 0, 0
			for i := skip; i+off+1 < len(bits) && i+1 < len(want); i += 2 {
				b0, b1 := bits[i+off], bits[i+off+1]
				switch rot {
				case 1:
					b0, b1 = b1, b0^1
				case 2:
					b0, b1 = b0^1, b1^1
				case 3:
					b0, b1 = b1^1, b0
				}
				if b0 != want[i] {
					errs++
				}
				if b1 != want[i+1] {
					errs++
				}
				n += 2
			}
			if n > 500 {
				if ber := float64(errs) / float64(n); ber < bestBER {
					bestBER = ber
				}
			}
		}
	}
	if bestBER > 0.05 {
		t.Errorf("QPSK at -50 dBFS with +120 Hz offset: best BER %.4f, want <= 0.05", bestBER)
	}
}
