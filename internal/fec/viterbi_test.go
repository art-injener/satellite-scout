package fec

import (
	"bytes"
	"testing"
)

func TestViterbiCCSDS_RoundTrip(t *testing.T) {
	msg := []byte("Viterbi USP hello-12")
	coded := EncodeViterbiCCSDS(msg)
	if len(coded) != (len(msg)*8+6)*2 {
		t.Fatalf("coded len %d", len(coded))
	}
	got := DecodeViterbiCCSDS(coded)
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %q want %q", got, msg)
	}
}

func TestViterbiCCSDS_CorrectsFewFlips(t *testing.T) {
	msg := bytes.Repeat([]byte{0x3C}, 20)
	coded := EncodeViterbiCCSDS(msg)
	coded[10] ^= 1
	coded[40] ^= 1
	coded[100] ^= 1
	got := DecodeViterbiCCSDS(coded)
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %x want %x", got, msg)
	}
}
