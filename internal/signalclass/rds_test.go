package signalclass

import (
	"math"
	"testing"

	"github.com/art-injener/satellite-scout/internal/modem"
)

func TestRDSSyndrome_KnownOffsetIdentity(t *testing.T) {
	const info = 0x1234
	for i, name := range []string{"A", "B", "C", "D", "C'"} {
		block := makeRDSBlock(info, i)
		s := rdsSyndrome(block)
		if s != rdsOffset[i] {
			t.Errorf("offset %s: syndrome %#x, want %#x (block %#x)", name, s, rdsOffset[i], block)
		}
		if got := (block >> 10) & 0xFFFF; got != info {
			t.Errorf("offset %s: info %#x, want %#x", name, got, info)
		}
	}
}

func TestRDSPSFromBits_AssemblesName(t *testing.T) {
	// Group 0A, four segments → "TEST FM ".
	name := [8]byte{'T', 'E', 'S', 'T', ' ', 'F', 'M', ' '}
	var bits []byte
	for seg := 0; seg < 4; seg++ {
		pi := 0xF201
		// Block B: group type 0A, TP=0, PTY=0, TA=0, M/S=0, DI=0, seg address.
		b := (0 << 12) | (seg & 3)
		c := 0xE0E0 // AF filler
		d := int(name[seg*2])<<8 | int(name[seg*2+1])
		for _, blk := range []struct{ info, off int }{{pi, 0}, {b, 1}, {c, 2}, {d, 3}} {
			bits = append(bits, intToBits26(makeRDSBlock(blk.info, blk.off))...)
		}
	}
	got := rdsPSFromBits(bits)
	if got != "TEST FM" {
		t.Fatalf("PS %q, want TEST FM", got)
	}
}

func intToBits26(block int) []byte {
	out := make([]byte, 26)
	for i := 25; i >= 0; i-- {
		out[25-i] = byte((block >> i) & 1)
	}
	return out
}

func TestRDSBits_FromDirectMPX(t *testing.T) {
	const rate = 256_000.0
	mpx := synthMPX(int(rate*2), rate, "RADIO 1 ")
	got := rdsPSFromBits(rdsBits(mpx, rate, rdsCarrierHz))
	if got != "RADIO 1" {
		t.Fatalf("PS %q from MPX, want RADIO 1 (bits %d)", got, len(rdsBits(mpx, rate, rdsCarrierHz)))
	}
}

func TestDecodeRDSPS_FromSyntheticWFM(t *testing.T) {
	const rate = 256_000.0
	n := int(rate * 2)
	iq := modem.NewFM(modem.FMConfig{SampleRate: rate, DeviationHz: 75_000}).
		Modulate(nil, float32s(synthMPX(n, rate, "RADIO 1 ")))
	got := decodeRDSPS(iq, rate)
	if got != "RADIO 1" {
		t.Fatalf("PS %q, want RADIO 1", got)
	}
}

// Уход частоты отсчётов как у кварца RTL: свободные 57 кГц не держат фазу, 3×пилот — да.
func TestDecodeRDSPS_WithSampleRateError(t *testing.T) {
	const nom = 256_000.0
	const ppm = 60.0
	trueRate := nom * (1 + ppm/1e6)
	n := int(nom * 2)
	iq := modem.NewFM(modem.FMConfig{SampleRate: trueRate, DeviationHz: 75_000}).
		Modulate(nil, float32s(synthMPX(n, trueRate, "EUROPA+ ")))
	got := decodeRDSPS(iq, nom)
	if got != "EUROPA+" {
		t.Fatalf("PS %q at %.0f ppm, want EUROPA+", got, ppm)
	}
}

// Узкий горб обнаружителя (~130 кГц) без запаса срезает RDS; Extract расширяет канал.
func TestExtract_BroadcastHumpKeepsRDS(t *testing.T) {
	const rate = 2_560_000.0
	n := int(rate * 2)
	iq := modem.NewFM(modem.FMConfig{SampleRate: rate, DeviationHz: 75_000}).
		Modulate(nil, float32s(synthMPX(n, rate, "RADIO 1 ")))
	if ExtractBandwidth(130_000) < wfmExtractMinHz {
		t.Fatalf("ExtractBandwidth(130k)=%.0f, want ≥ %.0f", ExtractBandwidth(130_000), wfmExtractMinHz)
	}
	slice, outRate := Extract(iq, rate, 0, 130_000)
	if got := decodeRDSPS(slice, outRate); got != "RADIO 1" {
		t.Fatalf("PS %q after Extract(130k), want RADIO 1 (rate %.0f, n=%d)", got, outRate, len(slice))
	}
}

func float32s(x []float64) []float32 {
	out := make([]float32, len(x))
	for i, v := range x {
		out[i] = float32(v)
	}
	return out
}

// synthMPX — пилот 19 кГц + RDS 57 кГц с PS (после диффкодирования и двухфазы).
func synthMPX(n int, rate float64, ps8 string) []float64 {
	if len(ps8) < 8 {
		ps8 = (ps8 + "        ")[:8]
	}
	data := makeRDSBitstream([]byte(ps8))
	syms := make([]byte, len(data))
	prev := byte(0)
	for i, d := range data {
		syms[i] = prev ^ d
		prev = syms[i]
	}
	sps := rate / rdsBitRate
	out := make([]float64, n)
	for i := range out {
		t := float64(i) / rate
		ph := 2 * math.Pi * rdsPilotHz * t
		pilot := 0.1 * math.Sin(ph)
		si := int(float64(i)/sps) % len(syms)
		phase := math.Mod(float64(i), sps) / sps
		level := 1.0
		if phase >= 0.5 {
			level = -1
		}
		if syms[si] == 0 {
			level = -level
		}
		// Несущая RDS — третья гармоника пилота (как в стереомультиплексе).
		rds := 0.08 * level * math.Cos(3*ph)
		out[i] = 0.2*math.Sin(2*math.Pi*1000*t) + pilot + rds
	}
	return out
}

// makeRDSBitstream — повторяющиеся группы 0A с PS (8 символов).
func makeRDSBitstream(ps []byte) []byte {
	var bits []byte
	for range 8 {
		for seg := 0; seg < 4; seg++ {
			pi := 0xC201
			b := seg & 3
			c := 0xE0E0
			d := int(ps[seg*2])<<8 | int(ps[seg*2+1])
			for _, blk := range []struct{ info, off int }{{pi, 0}, {b, 1}, {c, 2}, {d, 3}} {
				bits = append(bits, intToBits26(makeRDSBlock(blk.info, blk.off))...)
			}
		}
	}
	return bits
}
