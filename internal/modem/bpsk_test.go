package modem

import (
	"testing"

	"github.com/art-injener/satellite-scout/internal/ax25"
)

func TestBPSK1200_CleanHDLCFrame(t *testing.T) {
	cfg := BPSK1200(48_000)
	frame := ax25.Frame{Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "RS0SIM"}, Info: []byte("bpsk test")}
	b, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	levels := ax25.NRZI(ax25.HDLCBits(b, 64, 2))
	iq := NewBPSK(cfg).Modulate(nil, levels)
	bits := NewBPSKDemod(cfg).Demodulate(nil, iq)
	var def ax25.Deframer
	def.Push(nil, bits)
	if def.Good() < 1 {
		t.Fatalf("good=%d bad=%d bits=%d jitter=%.3f, want ≥1 good frame",
			def.Good(), def.Bad(), len(bits), NewBPSKDemod(cfg).ClockJitter())
	}
}

func TestBPSK9600_CleanHDLCFrame(t *testing.T) {
	cfg := BPSK9600(64_000)
	frame := ax25.Frame{Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "RS0SIM"}, Info: []byte("bpsk 9600")}
	b, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	levels := ax25.NRZI(ax25.HDLCBits(b, 64, 2))
	bits := NewBPSKDemod(cfg).Demodulate(nil, NewBPSK(cfg).Modulate(nil, levels))
	var def ax25.Deframer
	def.Push(nil, bits)
	if def.Good() < 1 {
		t.Fatalf("good=%d bad=%d bits=%d, want ≥1 good frame", def.Good(), def.Bad(), len(bits))
	}
}
