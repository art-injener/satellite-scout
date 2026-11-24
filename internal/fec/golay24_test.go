package fec

import (
	"math/bits"
	"testing"
)

func TestGolay24_EncodeKnown(t *testing.T) {
	// data=0: синдром нулевой, кодовое слово 0.
	if got := EncodeGolay24(0); got != 0 {
		t.Fatalf("EncodeGolay24(0)=%#x, want 0", got)
	}
	// Неноль: старшие 12 бит — чётность, младшие — данные.
	const data = 0x0AB
	cw := EncodeGolay24(data)
	if cw&0xFFF != data {
		t.Fatalf("data bits %#x, want %#x", cw&0xFFF, data)
	}
	if cw>>12 == 0 {
		t.Fatal("parity bits unexpectedly zero")
	}
}

func TestGolay24_RoundTripClean(t *testing.T) {
	for data := uint32(0); data < 64; data++ {
		cw := EncodeGolay24(data)
		got, n, err := DecodeGolay24(cw)
		if err != nil {
			t.Fatalf("data=%#x: %v", data, err)
		}
		if n != 0 || got != data {
			t.Fatalf("data=%#x: got=%#x errs=%d", data, got, n)
		}
	}
}

func TestGolay24_CorrectsUpTo3Errors(t *testing.T) {
	const data uint32 = 0x5A3
	cw := EncodeGolay24(data)
	cases := []uint32{
		1 << 0,
		1 << 7,
		1 << 23,
		(1 << 1) | (1 << 10),
		(1 << 3) | (1 << 11) | (1 << 19),
	}
	for _, e := range cases {
		got, n, err := DecodeGolay24(cw ^ e)
		if err != nil {
			t.Fatalf("e=%#x: %v", e, err)
		}
		wantN := bits.OnesCount32(e)
		if got != data || n != wantN {
			t.Fatalf("e=%#x: got=%#x errs=%d, want data=%#x errs=%d", e, got, n, data, wantN)
		}
	}
}

func TestGolay24_Uncorrectable(t *testing.T) {
	cw := EncodeGolay24(0x111)
	// Четыре ошибки в разных половинах — обычно неисправимы.
	bad := cw ^ 0x00F00F
	if _, _, err := DecodeGolay24(bad); err == nil {
		t.Fatal("expected uncorrectable error")
	}
}

func TestGolay24_PackBytes(t *testing.T) {
	cw := EncodeGolay24(0x0AB)
	b := Golay24ToBytes(cw)
	if len(b) != 3 {
		t.Fatalf("len=%d", len(b))
	}
	got := BytesToGolay24(b)
	if got != cw&0xFFFFFF {
		t.Fatalf("got %#x want %#x", got, cw&0xFFFFFF)
	}
}
