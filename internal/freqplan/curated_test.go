package freqplan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCurated_ReadsTransmitters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "curated.json")
	body := `{
  "transmitters": [
    {"norad_id": 25544, "name": "ISS", "freq_hz": 437800000, "mode": "FM", "kind": "voice"},
    {"norad_id": 44909, "name": "RS-44", "freq_hz": 435605000, "mode": "CW", "kind": "beacon"}
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := LoadCurated(path)
	if err != nil {
		t.Fatalf("LoadCurated: %v", err)
	}
	got := src.Transmitters(25544)
	if len(got) != 1 || got[0].FreqHz != 437_800_000 || got[0].Source != SourceCurated {
		t.Fatalf("ISS = %+v", got)
	}
	if src.Transmitters(1) != nil {
		t.Fatalf("unknown norad should be empty")
	}
}

func TestLoadCurated_MissingFileIsEmptySource(t *testing.T) {
	src, err := LoadCurated(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("LoadCurated: %v", err)
	}
	if src.Transmitters(25544) != nil {
		t.Fatalf("empty curated should return nil")
	}
}
