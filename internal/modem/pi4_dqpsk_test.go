package modem

import (
	"testing"
	"time"
)

func TestPI4DQPSK_CleanPRBSLowBER(t *testing.T) {
	cfg := PI4DQPSKConfig{SampleRate: 64_000, Baud: DefaultPI4DQPSKBaud}
	nbits := 4000
	want := prbs9Bits(nbits)
	bits := NewPI4DQPSKDemod(cfg).Demodulate(nil, NewPI4DQPSK(cfg).Modulate(nil, want))
	ber, n := bestBitAlignBER(want, bits, 8)
	t.Logf("ber=%.4f n=%d", ber, n)
	if ber > 0.02 {
		t.Errorf("BER %.4f too high for clean π/4-DQPSK", ber)
	}
}

func TestPI4DQPSK_ToleratesResidualCarrierAndDrift(t *testing.T) {
	cfg := PI4DQPSKConfig{SampleRate: 64_000, Baud: DefaultPI4DQPSKBaud}
	ch := NewChannel(ChannelConfig{
		SampleRate: cfg.SampleRate,
		OffsetHz: func(t time.Duration) float64 {
			return 800 - 50*t.Seconds()
		},
	})
	want := prbs9Bits(4000)
	iq := ch.Process(nil, NewPI4DQPSK(cfg).Modulate(nil, want))
	bits := NewPI4DQPSKDemod(cfg).Demodulate(nil, iq)
	ber, n := bestBitAlignBER(want, bits, 8)
	t.Logf("ber=%.4f n=%d", ber, n)
	if ber > 0.05 {
		t.Errorf("BER %.4f too high with residual carrier/drift", ber)
	}
}

func TestPI4DQPSK_StateContinuesAcrossCalls(t *testing.T) {
	cfg := PI4DQPSKConfig{SampleRate: 64_000, Baud: DefaultPI4DQPSKBaud}
	levels := prbs9Bits(2000)
	iq := NewPI4DQPSK(cfg).Modulate(nil, levels)
	whole := NewPI4DQPSKDemod(cfg).Demodulate(nil, iq)
	d := NewPI4DQPSKDemod(cfg)
	parts := d.Demodulate(nil, iq[:len(iq)/3])
	parts = d.Demodulate(parts, iq[len(iq)/3:])
	if len(parts) != len(whole) {
		t.Fatalf("chunked %d bits, whole %d", len(parts), len(whole))
	}
	for i := range whole {
		if parts[i] != whole[i] {
			t.Fatalf("bit %d: chunked %d, whole %d", i, parts[i], whole[i])
		}
	}
}

func TestPI4DQPSKBaudOrDefault(t *testing.T) {
	if got := PI4DQPSKBaudOrDefault(0); got != DefaultPI4DQPSKBaud {
		t.Errorf("got %.0f, want %.0f", got, DefaultPI4DQPSKBaud)
	}
	if got := PI4DQPSKBaudOrDefault(4800); got != 4800 {
		t.Errorf("got %.0f, want 4800", got)
	}
}

// bestBitAlignBER ищет задержку got относительно want (шаг 2 бита) с наименьшей BER.
// Дифф. детектор отдаёт дибит со второго символа, поэтому типичная задержка — 2 бита.
func bestBitAlignBER(want, got []byte, maxOff int) (ber float64, n int) {
	bestErr := len(got) + 1
	bestN := 0
	for delay := 0; delay < maxOff && delay < len(want); delay += 2 {
		err, nn := 0, 0
		for i := 0; i < len(got) && i+delay < len(want); i++ {
			if got[i] != want[i+delay] {
				err++
			}
			nn++
		}
		if nn > 200 && err < bestErr {
			bestErr, bestN = err, nn
		}
	}
	if bestN == 0 {
		return 1, 0
	}
	return float64(bestErr) / float64(bestN), bestN
}
