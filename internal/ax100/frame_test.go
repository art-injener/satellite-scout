package ax100

import (
	"bytes"
	"testing"
)

func TestEncodeDecode_RoundTrip(t *testing.T) {
	// Короткий CSP-подобный кадр (4 байта заголовка + данные).
	payload := []byte{0x00, 0x2A, 0x10, 0x00, 'T', 'M', 'I', '1', '2', '3'}
	levels, err := Encode(payload, 32, 8)
	if err != nil {
		t.Fatal(err)
	}
	var d Deframer
	got := d.Push(nil, levels)
	if len(got) != 1 {
		t.Fatalf("frames=%d, good=%d bad=%d", len(got), d.Good(), d.Bad())
	}
	if !bytes.Equal(got[0], payload) {
		t.Fatalf("got %x want %x", got[0], payload)
	}
	if d.Good() != 1 || d.Bad() != 0 {
		t.Fatalf("good=%d bad=%d", d.Good(), d.Bad())
	}
}

func TestDeframer_BitErrorsInSync(t *testing.T) {
	payload := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	levels, err := Encode(payload, 16, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Инвертируем 2 бита внутри синхрослова (байты preamble*8 .. preamble*8+31).
	const preamble = 16
	levels[(preamble*8)+3] ^= 1
	levels[(preamble*8)+17] ^= 1

	var d Deframer
	got := d.Push(nil, levels)
	if len(got) != 1 || !bytes.Equal(got[0], payload) {
		t.Fatalf("got %v (n=%d), good=%d bad=%d", got, len(got), d.Good(), d.Bad())
	}
}

func TestDeframer_RejectsNoise(t *testing.T) {
	noise := make([]byte, 4096)
	for i := range noise {
		noise[i] = byte(i & 1)
	}
	var d Deframer
	got := d.Push(nil, noise)
	if len(got) != 0 || d.Good() != 0 {
		t.Fatalf("false frames=%d good=%d", len(got), d.Good())
	}
}

func TestDeframer_ReportsFECCorrections(t *testing.T) {
	payload := []byte{0x00, 0x2A, 0x10, 0x00, 'F', 'E', 'C'}
	levels, err := Encode(payload, 16, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Три байта полезной нагрузки после Golay (3 байта) — внутри RS-блока.
	const preamble = 16
	base := preamble*8 + 32 + 3*8 // sync 32 бит + Golay 3 байта
	levels[base+8] ^= 1
	levels[base+40] ^= 1
	levels[base+80] ^= 1

	var d Deframer
	got := d.Push(nil, levels)
	if len(got) != 1 || !bytes.Equal(got[0], payload) {
		t.Fatalf("n=%d good=%d bad=%d", len(got), d.Good(), d.Bad())
	}
	if d.FECCorrected() < 1 {
		t.Fatalf("fecCorr=%d, want ≥1", d.FECCorrected())
	}
}
