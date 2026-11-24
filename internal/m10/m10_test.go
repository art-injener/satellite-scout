package m10

import (
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func golden(t *testing.T) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	p := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "sonde", "m10", "golden_frame.hex")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// trim newline
	s := string(raw)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != FrameLen {
		t.Fatalf("len=%d", len(b))
	}
	return b
}

func TestFrameOK_Golden(t *testing.T) {
	if !FrameOK(golden(t)) {
		t.Fatal("checksum")
	}
}

func TestParse_Golden(t *testing.T) {
	tel, ok := Parse(golden(t))
	if !ok {
		t.Fatal("parse")
	}
	if tel.ID != "310 2 11329" {
		t.Fatalf("id=%q", tel.ID)
	}
	if !tel.HasPos || math.Abs(tel.LatDeg-46.05) > 1e-4 || math.Abs(tel.LonDeg-16.13) > 1e-4 {
		t.Fatalf("pos=%+v", tel)
	}
	if math.Abs(tel.AltM-5000) > 0.1 {
		t.Fatalf("alt=%v", tel.AltM)
	}
	if !tel.HasTemp || math.Abs(tel.TempC-25) > 0.5 {
		t.Fatalf("temp=%v", tel.TempC)
	}
}

func TestDeframer_Golden(t *testing.T) {
	levels := BytesToLevels(golden(t))
	d := &Deframer{}
	out := d.Push(nil, levels)
	if len(out) != 1 {
		t.Fatalf("frames=%d good=%d bad=%d", len(out), d.Good(), d.Bad())
	}
	tel, ok := Parse(out[0])
	if !ok || tel.ID != "310 2 11329" {
		t.Fatalf("%+v", tel)
	}
}

func TestSealChecksum_RoundTrip(t *testing.T) {
	f := append([]byte(nil), golden(t)...)
	f[posCheck], f[posCheck+1] = 0, 0
	SealChecksum(f)
	if !FrameOK(f) {
		t.Fatal("seal")
	}
}
