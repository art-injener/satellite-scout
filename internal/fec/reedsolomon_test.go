package fec

import (
	"bytes"
	"testing"
)

func TestReedSolomon_EncodeKnownShort(t *testing.T) {
	// Вектор из эталонного энкодера с параметрами libfec CCSDS
	// (mm=8, gfpoly=0x187, fcr=112, prim=11, nroots=32).
	msg := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
	}
	wantParity := mustDecodeHex(t, "c314fd836f019a7de996e5fab240157cd8a187bb2e7b072e55e628a4c00204d7")
	got, err := EncodeRS(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantParity) {
		t.Fatalf("parity\n got %x\nwant %x", got, wantParity)
	}
}

func TestReedSolomon_RoundTrip(t *testing.T) {
	msg := []byte("CSP hello AX100 frame!!") // 23 байта
	parity, err := EncodeRS(msg)
	if err != nil {
		t.Fatal(err)
	}
	block := append(append([]byte(nil), msg...), parity...)
	got, n, err := DecodeRS(block)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("corrected=%d, want 0", n)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %x want %x", got, msg)
	}
}

func TestReedSolomon_CorrectsErrors(t *testing.T) {
	msg := make([]byte, 100)
	for i := range msg {
		msg[i] = byte(i * 3)
	}
	parity, err := EncodeRS(msg)
	if err != nil {
		t.Fatal(err)
	}
	block := append(append([]byte(nil), msg...), parity...)
	block[0] ^= 0xff
	block[50] ^= 0x55
	block[len(block)-1] ^= 0x0f
	got, n, err := DecodeRS(block)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("corrected=%d, want 3", n)
	}
	if !bytes.Equal(got, msg) {
		t.Fatal("payload mismatch after correction")
	}
}

func TestReedSolomon_TooManyErrors(t *testing.T) {
	msg := make([]byte, 40)
	parity, err := EncodeRS(msg)
	if err != nil {
		t.Fatal(err)
	}
	block := append(append([]byte(nil), msg...), parity...)
	for i := 0; i < 20; i++ {
		block[i] ^= 0xff
	}
	if _, _, err := DecodeRS(block); err == nil {
		t.Fatal("expected decode failure")
	}
}

func TestReedSolomon_FullCodeword(t *testing.T) {
	msg := make([]byte, RSDataLen)
	for i := range msg {
		msg[i] = byte(i)
	}
	parity, err := EncodeRS(msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(parity) != RSParityLen {
		t.Fatalf("parity len %d", len(parity))
	}
	block := append(msg, parity...)
	got, _, err := DecodeRS(block)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatal("full codeword mismatch")
	}
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		var b byte
		for _, c := range []byte{s[2*i], s[2*i+1]} {
			b <<= 4
			switch {
			case c >= '0' && c <= '9':
				b |= c - '0'
			case c >= 'a' && c <= 'f':
				b |= c - 'a' + 10
			default:
				t.Fatalf("bad hex %q", s)
			}
		}
		out[i] = b
	}
	return out
}
