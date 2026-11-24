package ax100

import (
	"bytes"
	"testing"
)

func TestEncodeRSMode_RoundTrip(t *testing.T) {
	payload := PackCSP(CSPHeader{
		Priority: 1, Source: 2, Destination: 5, SrcPort: 10, DstPort: 9,
	}, []byte("RS mode TM"))
	levels, err := EncodeRSMode(payload, 64, 8)
	if err != nil {
		t.Fatal(err)
	}
	var d RSDeframer
	got := d.Push(nil, levels)
	if len(got) != 1 {
		t.Fatalf("frames=%d good=%d bad=%d", len(got), d.Good(), d.Bad())
	}
	if !bytes.Equal(got[0], payload) {
		t.Fatalf("got %x want %x", got[0], payload)
	}
}

func TestRSDeframer_SyncBitErrors(t *testing.T) {
	payload := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0x01, 0x02, 0x03, 0x04}
	levels, err := EncodeRSMode(payload, 48, 4)
	if err != nil {
		t.Fatal(err)
	}
	// После G3RUH биты синхрослова размазаны — ломаем несколько бит преамбулы/начала,
	// оставляя запас по порогу sync на дескремблированном слове.
	if len(levels) > 100 {
		levels[80] ^= 1
		levels[95] ^= 1
	}
	var d RSDeframer
	got := d.Push(nil, levels)
	if len(got) != 1 || !bytes.Equal(got[0], payload) {
		t.Fatalf("got %v n=%d good=%d bad=%d", got, len(got), d.Good(), d.Bad())
	}
}

func TestRSDeframer_RejectsNoise(t *testing.T) {
	noise := make([]byte, 8192)
	for i := range noise {
		noise[i] = byte((i * 3) & 1)
	}
	var d RSDeframer
	if got := d.Push(nil, noise); len(got) != 0 || d.Good() != 0 {
		t.Fatalf("false frames=%d good=%d", len(got), d.Good())
	}
}
