package demod

import (
	"bytes"
	"testing"
)

func TestBurstDeframer_EmitsPackedBytesOnIdleGap(t *testing.T) {
	d := newBurstDeframer().(*burstDeframer)
	// 32 mark + короткие нули внутри (0xAA), затем пауза.
	data := bytes.Repeat([]byte{1, 0, 1, 0, 1, 0, 1, 0}, 4) // заканчивается на 0 — pending
	data = append(data, 1)                                   // закрыть pending нуль → 33 бита с данными
	levels := append(append([]byte{}, data...), bytes.Repeat([]byte{0}, burstIdleBits)...)
	got := d.Push(nil, levels)
	if len(got) != 1 {
		t.Fatalf("packets=%d, want 1", len(got))
	}
	// 0xAA×4 + старший бит следующего байта = 0x80
	if want := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x80}; !bytes.Equal(got[0], want) {
		t.Fatalf("raw=%x, want %x", got[0], want)
	}
	if d.Good() != 1 {
		t.Fatalf("good=%d, want 1", d.Good())
	}
}

func TestBurstDeframer_IgnoresShortNoise(t *testing.T) {
	d := newBurstDeframer().(*burstDeframer)
	levels := append([]byte{1, 0, 1, 0}, bytes.Repeat([]byte{0}, burstIdleBits)...)
	if got := d.Push(nil, levels); len(got) != 0 {
		t.Fatalf("got %d packets, want 0 for short burst", len(got))
	}
}

func TestBurstDeframer_FlushOnSquelchClose(t *testing.T) {
	d := newBurstDeframer().(*burstDeframer)
	levels := bytes.Repeat([]byte{1}, burstMinBits)
	if got := d.Push(nil, levels); len(got) != 0 {
		t.Fatalf("Push early=%d, want 0", len(got))
	}
	got := d.Flush(nil)
	if len(got) != 1 {
		t.Fatalf("Flush packets=%d, want 1", len(got))
	}
	if d.Good() != 1 {
		t.Fatalf("good=%d, want 1", d.Good())
	}
}

func TestBurstDeframer_MaxLengthFlush(t *testing.T) {
	d := newBurstDeframer().(*burstDeframer)
	levels := bytes.Repeat([]byte{1, 0}, burstMaxBits/2+8)
	got := d.Push(nil, levels)
	if len(got) < 1 {
		t.Fatal("want at least one packet from max-length flush")
	}
}
