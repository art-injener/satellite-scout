package modem

import (
	"testing"
	"time"
)

// ookDemodRate — частота канала после фильтра: у 480 бод около 133 отсчёта на символ.
const ookDemodRate = 64_000

func modulatePRBSOOK(cfg OOKConfig, nbits int) []complex64 {
	return NewOOK(cfg).Modulate(nil, prbs9Bits(nbits))
}

func TestOOKDemod_CleanSignalNoErrors(t *testing.T) {
	const nbits = 5000
	cfg := OOKConfig{SampleRate: ookDemodRate, Baud: DefaultOOKBaud}
	bits := NewOOKDemod(cfg).Demodulate(nil, modulatePRBSOOK(cfg, nbits))
	if d := len(bits) - nbits; d < -2 || d > 2 {
		t.Errorf("demodulated %d bits, want %d±2", len(bits), nbits)
	}
	if c := checkBits(t, bits); c.Errors() != 0 {
		t.Errorf("errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
}

func TestOOKDemod_SampleBaud422(t *testing.T) {
	// Образец метеозонда: OOK около 422 бод.
	cfg := OOKConfig{SampleRate: ookDemodRate, Baud: 422}
	bits := NewOOKDemod(cfg).Demodulate(nil, modulatePRBSOOK(cfg, 4000))
	if c := checkBits(t, bits); c.Errors() != 0 {
		t.Errorf("errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
}

func TestOOKDemod_TracksTransmitterClockOffset(t *testing.T) {
	for _, baud := range []float64{478.5, 481.5} { // ±0.3 %
		t.Run("", func(t *testing.T) {
			tx := OOKConfig{SampleRate: ookDemodRate, Baud: baud}
			rx := OOKConfig{SampleRate: ookDemodRate, Baud: DefaultOOKBaud}
			bits := NewOOKDemod(rx).Demodulate(nil, modulatePRBSOOK(tx, 8000))
			if c := checkBits(t, bits); c.Errors() != 0 {
				t.Errorf("tx %.1f Bd: errors=%d of %d, want 0", baud, c.Errors(), c.Bits())
			}
		})
	}
}

func TestOOKDemod_ToleratesResidualCarrierAndDrift(t *testing.T) {
	cfg := OOKConfig{SampleRate: ookDemodRate, Baud: 422}
	// Остаток несущей и линейный дрейф как у образца (−124 Гц/с): огибающая к фазе безразлична.
	ch := NewChannel(ChannelConfig{
		SampleRate: ookDemodRate,
		OffsetHz: func(t time.Duration) float64 {
			return 1500 - 124*t.Seconds()
		},
	})
	iq := ch.Process(nil, modulatePRBSOOK(cfg, 5000))
	bits := NewOOKDemod(cfg).Demodulate(nil, iq)
	if c := checkBits(t, bits); c.Errors() != 0 {
		t.Errorf("errors=%d of %d, want 0", c.Errors(), c.Bits())
	}
}

func TestOOKDemod_StateContinuesAcrossCalls(t *testing.T) {
	cfg := OOKConfig{SampleRate: ookDemodRate, Baud: DefaultOOKBaud}
	iq := modulatePRBSOOK(cfg, 3000)
	whole := NewOOKDemod(cfg).Demodulate(nil, iq)
	d := NewOOKDemod(cfg)
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

func TestOOKBaudOrDefault(t *testing.T) {
	if got := OOKBaudOrDefault(0); got != DefaultOOKBaud {
		t.Errorf("OOKBaudOrDefault(0)=%.0f, want %.0f", got, DefaultOOKBaud)
	}
	if got := OOKBaudOrDefault(422); got != 422 {
		t.Errorf("OOKBaudOrDefault(422)=%.0f, want 422", got)
	}
}
