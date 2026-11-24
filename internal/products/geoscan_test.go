package products

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGeoscan_AssemblesChunksWithGaps(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.now = fixedClock(time.Date(2026, 11, 22, 18, 0, 0, 0, time.UTC))

	meta := Meta{RadioPath: "radio_path_0", Kind: KindGEOSCAN, Mode: "geoscan", ModeName: "GEOSCAN",
		FrequencyHz: 436_200_000, NoradID: 53385}

	start := []byte{0xFF, 0xD8, 0xFF, 0xDB}
	pad := make([]byte, GeoscanChunkStep-len(start))
	chunk0 := append(start, pad...)

	p := s.PutGeoscanChunk(meta, GeoscanOffsetBase, chunk0)
	if p.ID == "" || p.Done || p.Kind != KindGEOSCAN {
		t.Fatalf("first=%+v", p)
	}
	if p.Lines != GeoscanChunkStep || p.Height < GeoscanChunkStep {
		t.Fatalf("progress lines=%d height=%d", p.Lines, p.Height)
	}

	chunk2 := bytes.Repeat([]byte{0xAA}, GeoscanChunkStep)
	off2 := GeoscanOffsetBase + 2*GeoscanChunkStep
	p = s.PutGeoscanChunk(meta, off2, chunk2)
	// Пропуск одного куска: нули в дыре, принято 2·56 байт.
	if p.Lines != 2*GeoscanChunkStep {
		t.Fatalf("lines after gap=%d, want %d", p.Lines, 2*GeoscanChunkStep)
	}
	if p.Gaps != 1 {
		t.Fatalf("gaps=%d, want 1", p.Gaps)
	}

	data, ct, ok := s.Image(p.ID)
	if !ok || ct != "image/jpeg" {
		t.Fatalf("image ok=%v ct=%q", ok, ct)
	}
	if !bytes.HasPrefix(data, start) {
		t.Fatalf("prefix=% x", data[:min(8, len(data))])
	}
	hole := data[GeoscanChunkStep : 2*GeoscanChunkStep]
	if !bytes.Equal(hole, make([]byte, GeoscanChunkStep)) {
		t.Fatalf("gap not zero-filled")
	}
	if !bytes.Equal(data[2*GeoscanChunkStep:3*GeoscanChunkStep], chunk2) {
		t.Fatal("chunk2 mismatch")
	}
}

func TestGeoscan_FinishOnEOIWritesJPEG(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.now = fixedClock(time.Date(2026, 11, 22, 19, 0, 0, 0, time.UTC))
	meta := Meta{RadioPath: "rp", Kind: KindGEOSCAN, Mode: "geoscan", ModeName: "GEOSCAN"}

	body := []byte{0xFF, 0xD8, 0xFF, 0xD9} // минимальный JPEG
	pad := make([]byte, GeoscanChunkStep-len(body))
	chunk := append(body, pad...)
	p := s.PutGeoscanChunk(meta, GeoscanOffsetBase, chunk)
	if !p.Done {
		t.Fatalf("want done on EOI, got %+v", p)
	}
	file, err := os.ReadFile(filepath.Join(dir, p.ID+".jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(file, []byte{0xFF, 0xD8}) || !bytes.Contains(file, []byte{0xFF, 0xD9}) {
		t.Fatalf("saved jpeg magic missing: % x", file[:min(16, len(file))])
	}
}

func TestGeoscan_NewSessionOnSamePathAfterDone(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.now = fixedClock(time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC))
	meta := Meta{RadioPath: "rp", Kind: KindGEOSCAN, Mode: "geoscan", ModeName: "GEOSCAN"}

	c1 := make([]byte, GeoscanChunkStep)
	c1[0], c1[1], c1[2], c1[3] = 0xFF, 0xD8, 0xFF, 0xD9
	p1 := s.PutGeoscanChunk(meta, GeoscanOffsetBase, c1)
	if !p1.Done {
		t.Fatal("first must finish")
	}
	s.now = fixedClock(time.Date(2026, 11, 22, 20, 0, 1, 0, time.UTC))
	c2 := make([]byte, GeoscanChunkStep)
	c2[0], c2[1] = 0xFF, 0xD8
	p2 := s.PutGeoscanChunk(meta, GeoscanOffsetBase, c2)
	if p2.ID == p1.ID {
		t.Fatalf("want new product after done, got %q", p2.ID)
	}
}
