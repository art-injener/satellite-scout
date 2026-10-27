package sigmf

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

func TestOpen_CF32ReadsAsCFile(t *testing.T) {
	ds, err := Open(filepath.Join("testdata", "tone_cf32.sigmf-meta"))
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()

	buf := make([]byte, 8*8)
	n, err := io.ReadFull(ds, buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 64 {
		t.Fatalf("n=%d", n)
	}
	re0 := math.Float32frombits(binary.LittleEndian.Uint32(buf[0:4]))
	im0 := math.Float32frombits(binary.LittleEndian.Uint32(buf[4:8]))
	if re0 != 1 || im0 != 0 {
		t.Fatalf("first sample = %v+%vi, want 1+0i", re0, im0)
	}
	if _, err := ds.Read(make([]byte, 8)); err != io.EOF {
		t.Fatalf("after end: err=%v, want EOF", err)
	}
}

func TestOpen_CI16ConvertsToCF32(t *testing.T) {
	ds, err := Open(filepath.Join("testdata", "tone_ci16.sigmf-meta"))
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()

	buf := make([]byte, 16)
	if _, err := io.ReadFull(ds, buf); err != nil {
		t.Fatal(err)
	}
	re := math.Float32frombits(binary.LittleEndian.Uint32(buf[0:4]))
	im := math.Float32frombits(binary.LittleEndian.Uint32(buf[4:8]))
	if math.Abs(float64(re)-1) > 1e-4 || im != 0 {
		t.Fatalf("got %v+%vi, want ~1+0i", re, im)
	}
	re2 := math.Float32frombits(binary.LittleEndian.Uint32(buf[8:12]))
	im2 := math.Float32frombits(binary.LittleEndian.Uint32(buf[12:16]))
	if re2 != 0 || math.Abs(float64(im2)-1) > 1e-4 {
		t.Fatalf("second %v+%vi, want ~0+1i", re2, im2)
	}
}

func TestOpen_MissingDataFile(t *testing.T) {
	dir := t.TempDir()
	meta := filepath.Join(dir, "x.sigmf-meta")
	if err := writeFile(meta, `{"global":{"core:datatype":"cf32_le","core:sample_rate":1},"captures":[{"core:frequency":1e8}]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(meta); err == nil {
		t.Fatal("want error when .sigmf-data is missing")
	}
}
