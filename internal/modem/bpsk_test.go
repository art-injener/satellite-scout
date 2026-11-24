package modem

import (
	"math"
	"testing"
	"time"

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

func TestBPSK_LowLevelRTLSDR(t *testing.T) {
	// -50 dBFS amplitude (~0.00316) with residual carrier offset so Costas loop must track phase.
	scale := float32(math.Pow(10, -50.0/20.0))
	cases := []struct {
		name   string
		cfg    BPSKConfig
		offset float64
	}{
		{"1200/-50dBFS", BPSK1200(48_000), 350},
		{"9600/-50dBFS", BPSK9600(64_000), 2500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := ax25.Frame{
				Dest: ax25.Address{Call: "CQ"},
				Src:  ax25.Address{Call: "RS0SIM"},
				Info: []byte("bpsk low level"),
			}
			b, err := frame.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			levels := ax25.NRZI(ax25.HDLCBits(b, 96, 4))
			iq := NewBPSK(tc.cfg).Modulate(nil, levels)
			ch := NewChannel(ChannelConfig{
				SampleRate: tc.cfg.SampleRate,
				OffsetHz:   func(time.Duration) float64 { return tc.offset },
			})
			iq = ch.Process(nil, iq)
			for i, s := range iq {
				iq[i] = complex(real(s)*scale, imag(s)*scale)
			}
			bits := NewBPSKDemod(tc.cfg).Demodulate(nil, iq)
			var def ax25.Deframer
			def.Push(nil, bits)
			if def.Good() < 1 {
				t.Fatalf("%s: good=%d bad=%d bits=%d, want >= 1 good frame", tc.name, def.Good(), def.Bad(), len(bits))
			}
		})
	}
}
