package ccsds

import (
	"bytes"
	"testing"
)

func TestEncode_RejectsEmptyAndTooLong(t *testing.T) {
	if _, err := Encode(nil, 4, 2); err == nil {
		t.Fatal("empty data: want error")
	}
	long := make([]byte, DataLen+1)
	if _, err := Encode(long, 4, 2); err == nil {
		t.Fatal("too long: want error")
	}
}

func TestEncode_DeframerRoundTrip(t *testing.T) {
	want := []byte("ccsds cadu probe frame for scout")
	levels, err := Encode(want, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	var d Deframer
	got := d.Push(nil, levels)
	if len(got) != 1 {
		t.Fatalf("frames=%d good=%d bad=%d sync=%d, want 1", len(got), d.Good(), d.Bad(), d.SyncWords())
	}
	if !bytes.HasPrefix(got[0], want) {
		t.Fatalf("prefix=%q, want %q", got[0][:len(want)], want)
	}
	if len(got[0]) != DataLen {
		t.Fatalf("len=%d, want %d", len(got[0]), DataLen)
	}
	if d.Good() != 1 || d.SyncWords() != 1 {
		t.Fatalf("good=%d sync=%d, want 1/1", d.Good(), d.SyncWords())
	}
}

func TestDeframer_AcceptsBitInvertedStream(t *testing.T) {
	want := []byte("inverted costas lock")
	levels, err := Encode(want, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := range levels {
		levels[i] ^= 1
	}
	var d Deframer
	got := d.Push(nil, levels)
	if len(got) != 1 || !bytes.HasPrefix(got[0], want) {
		t.Fatalf("frames=%d good=%d, want inverted round-trip", len(got), d.Good())
	}
}

func TestDeframer_RejectsNoiseWithoutASM(t *testing.T) {
	noise := make([]byte, 4096)
	for i := range noise {
		noise[i] = byte(i & 1)
	}
	var d Deframer
	if got := d.Push(nil, noise); len(got) != 0 || d.Good() != 0 {
		t.Fatalf("frames=%d good=%d, want none", len(got), d.Good())
	}
}
