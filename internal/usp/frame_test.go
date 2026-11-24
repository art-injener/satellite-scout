package usp

import (
	"bytes"
	"testing"

	"github.com/art-injener/satellite-scout/internal/ax25"
)

func TestUSP_RoundTripShort(t *testing.T) {
	frame := ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "RS32S"},
		Info: []byte("USP short beacon"),
	}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	levels, err := Encode(raw, false, 32, 4)
	if err != nil {
		t.Fatal(err)
	}
	var d Deframer
	got := d.Push(nil, levels)
	if len(got) != 1 {
		t.Fatalf("frames=%d good=%d bad=%d", len(got), d.Good(), d.Bad())
	}
	if !bytes.Equal(got[0], raw) {
		t.Fatalf("got %x want %x", got[0], raw)
	}
}

func TestUSP_RoundTripLong(t *testing.T) {
	info := bytes.Repeat([]byte("TM"), 80)
	frame := ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "RS15S"},
		Info: info,
	}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	levels, err := Encode(raw, true, 32, 4)
	if err != nil {
		t.Fatal(err)
	}
	var d Deframer
	got := d.Push(nil, levels)
	if len(got) != 1 || !bytes.Equal(got[0], raw) {
		t.Fatalf("n=%d good=%d bad=%d", len(got), d.Good(), d.Bad())
	}
}

func TestUSP_PLSComputeDistinct(t *testing.T) {
	if bytes.Equal(pls0, pls1) {
		t.Fatal("PLS0 == PLS1")
	}
	if dl, ok := dataLenForPLS(pls0); !ok || dl != DataLenShort {
		t.Fatalf("pls0 → %d ok=%v", dl, ok)
	}
	if dl, ok := dataLenForPLS(pls1); !ok || dl != DataLenLong {
		t.Fatalf("pls1 → %d ok=%v", dl, ok)
	}
}

func TestUSP_InvertedPolarity(t *testing.T) {
	frame := ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "RS32S"},
		Info: []byte("USP inverted beacon"),
	}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	levels, err := Encode(raw, false, 32, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i := range levels {
		levels[i] ^= 1
	}
	var d Deframer
	got := d.Push(nil, levels)
	if len(got) != 1 || !bytes.Equal(got[0], raw) {
		t.Fatalf("inverted USP: frames=%d good=%d bad=%d, want 1 matching frame", len(got), d.Good(), d.Bad())
	}
}
