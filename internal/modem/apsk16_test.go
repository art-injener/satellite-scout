package modem

import (
	"testing"
	"time"
)

func TestAPSK16_CleanPRBSLowBER(t *testing.T) {
	cfg := APSK16Config{SampleRate: 96_000, Baud: DefaultAPSK16Baud}
	nbits := 4000
	nbits -= nbits % 4
	want := prbs9Bits(nbits)
	bits := NewAPSK16Demod(cfg).Demodulate(nil, NewAPSK16(cfg).Modulate(nil, want))
	ber, n := bestAPSKAlignBER(want, bits, 16)
	t.Logf("ber=%.4f n=%d", ber, n)
	if ber > 0.02 {
		t.Errorf("BER %.4f too high for clean 16-APSK", ber)
	}
}

func TestAPSK16_ToleratesResidualCarrier(t *testing.T) {
	cfg := APSK16Config{SampleRate: 96_000, Baud: DefaultAPSK16Baud}
	// Небольшой остаток несущей: DD-ФАПЧ 16-APSK держит единицы градусов на символ.
	ch := NewChannel(ChannelConfig{
		SampleRate: cfg.SampleRate,
		OffsetHz:   func(time.Duration) float64 { return 80 },
	})
	nbits := 8000
	nbits -= nbits % 4
	want := prbs9Bits(nbits)
	iq := ch.Process(nil, NewAPSK16(cfg).Modulate(nil, want))
	bits := NewAPSK16Demod(cfg).Demodulate(nil, iq)
	ber, n := bestAPSKAlignBER(want, bits, 32)
	t.Logf("ber=%.4f n=%d", ber, n)
	if ber > 0.05 {
		t.Errorf("BER %.4f too high with residual carrier", ber)
	}
}

func TestAPSK16_StateContinuesAcrossCalls(t *testing.T) {
	cfg := APSK16Config{SampleRate: 96_000, Baud: DefaultAPSK16Baud}
	levels := prbs9Bits(2000)
	iq := NewAPSK16(cfg).Modulate(nil, levels)
	whole := NewAPSK16Demod(cfg).Demodulate(nil, iq)
	d := NewAPSK16Demod(cfg)
	parts := d.Demodulate(nil, iq[:len(iq)/3])
	parts = d.Demodulate(parts, iq[len(iq)/3:])
	if len(parts) != len(whole) {
		t.Fatalf("chunked %d bits, whole %d", len(parts), len(whole))
	}
	for i := range whole {
		if parts[i] != whole[i] {
			t.Fatalf("bit %d differs", i)
		}
	}
}

func TestAPSK16BaudOrDefault(t *testing.T) {
	if got := APSK16BaudOrDefault(0); got != DefaultAPSK16Baud {
		t.Errorf("got %.0f, want %.0f", got, DefaultAPSK16Baud)
	}
}

func bestAPSKAlignBER(want, got []byte, maxOff int) (ber float64, n int) {
	bestErr := len(got) + 1
	bestN := 0
	settle := 64 // пропуск захвата ФАПЧ
	for delay := 0; delay < maxOff && delay < len(want); delay += 4 {
		err, nn := 0, 0
		for i := settle; i < len(got) && i+delay < len(want); i++ {
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
