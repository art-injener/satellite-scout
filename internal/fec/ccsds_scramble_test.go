package fec

import (
	"bytes"
	"testing"
)

func TestCCSDSScramble_KnownSequence(t *testing.T) {
	// Вектор Blue Book / gr-satellites randomizer.c на нулевых данных.
	want := []byte{0xff, 0x48, 0x0e, 0xc0, 0x9a, 0x0d, 0x70, 0xbc, 0x8e}
	got := make([]byte, len(want))
	CCSDSScramble(got)
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x, want %x", got, want)
	}
}

func TestCCSDSScramble_Involution(t *testing.T) {
	in := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	buf := append([]byte(nil), in...)
	CCSDSScramble(buf)
	if bytes.Equal(buf, in) {
		t.Fatal("scramble left buffer unchanged")
	}
	CCSDSScramble(buf)
	if !bytes.Equal(buf, in) {
		t.Fatalf("second pass: got %x, want %x", buf, in)
	}
}
