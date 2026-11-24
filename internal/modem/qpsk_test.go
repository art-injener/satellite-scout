package modem

import "testing"

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
