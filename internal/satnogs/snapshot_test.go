package satnogs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadDiskCache_ReturnsEntriesWithFetchTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "satnogs_transmitters.json")
	at := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	low := int64(437_800_000)
	file := diskCacheFile{Satellites: map[int]diskCacheEntry{
		25544: {
			FetchedAt:    at,
			Transmitters: []Transmitter{{UUID: "u1", DownlinkLow: &low, Status: "active", Alive: true}},
		},
	}}
	if err := writeFileAtomic(path, file); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ReadDiskCache(path)
	if err != nil {
		t.Fatalf("ReadDiskCache: %v", err)
	}
	e, ok := got[25544]
	if !ok || !e.FetchedAt.Equal(at) || len(e.Transmitters) != 1 || e.Transmitters[0].UUID != "u1" {
		t.Fatalf("entry = %+v, want ISS with u1 fetched at %v", e, at)
	}
}

func TestReadDiskCache_MissingFileIsEmpty(t *testing.T) {
	got, err := ReadDiskCache(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, err %v; want empty, nil", got, err)
	}
}

func TestReadDiskCache_BrokenFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte(`{"satellites":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDiskCache(path); err == nil {
		t.Fatal("expected error for broken file")
	}
}
