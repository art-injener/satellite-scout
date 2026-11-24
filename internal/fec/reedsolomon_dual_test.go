package fec

import (
	"bytes"
	"testing"
)

func TestReedSolomonDual_RoundTrip(t *testing.T) {
	msg := []byte("USP dual-basis RS payload!!")
	parity, err := EncodeRSDual(msg)
	if err != nil {
		t.Fatal(err)
	}
	block := append(append([]byte(nil), msg...), parity...)
	got, n, err := DecodeRSDual(block)
	if err != nil || n != 0 {
		t.Fatalf("err=%v n=%d", err, n)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %x", got)
	}
}

func TestReedSolomonDual_DiffersFromOrdinary(t *testing.T) {
	msg := bytes.Repeat([]byte{0xA5}, 40)
	p1, _ := EncodeRS(msg)
	p2, _ := EncodeRSDual(msg)
	if bytes.Equal(p1, p2) {
		t.Fatal("dual parity unexpectedly equals ordinary")
	}
}
