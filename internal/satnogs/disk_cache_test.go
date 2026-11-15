package satnogs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func issTransmitters() []Transmitter {
	return []Transmitter{
		{UUID: "iss-fm", Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
	}
}

// fillDiskCache fetches transmitters through a service with disk cache and waits for the file.
func fillDiskCache(t *testing.T, path string) {
	t.Helper()
	mock := newMockFetcher()
	mock.results[25544] = issTransmitters()
	svc := NewService(mock).WithDiskCache(path)
	stop := runService(svc)
	defer stop()

	svc.RequestFetch([]int{25544})
	waitFor(t, time.Second, func() bool {
		data, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(data), "iss-fm")
	})
}

func TestService_DiskCache_SavedAfterFetch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "satnogs.json")

	fillDiskCache(t, path)
}

func TestService_DiskCache_FreshEntryServedWithoutNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "satnogs.json")
	fillDiskCache(t, path)

	mock := newMockFetcher()
	svc := NewService(mock).WithDiskCache(path)
	stop := runService(svc)
	defer stop()

	got := svc.GetPrimaryTransmitter(25544)
	if got == nil || got.UUID != "iss-fm" {
		t.Fatalf("GetPrimaryTransmitter = %+v, want iss-fm from disk", got)
	}
	if n := len(svc.GetAllTransmitters(25544)); n != 1 {
		t.Fatalf("GetAllTransmitters len = %d, want 1", n)
	}
	time.Sleep(50 * time.Millisecond)
	if n := mock.callCount(); n != 0 {
		t.Fatalf("network calls = %d, want 0 for fresh disk entry", n)
	}
}

func TestService_DiskCache_StaleEntryServedAndRefetched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "satnogs.json")
	fillDiskCache(t, path)

	mock := newMockFetcher()
	mock.errors[25544] = errors.New("stalled")
	svc := NewService(mock).WithCacheTTL(time.Nanosecond).WithDiskCache(path)
	stop := runService(svc)
	defer stop()

	if got := svc.GetPrimaryTransmitter(25544); got == nil {
		t.Fatal("GetPrimaryTransmitter = nil, want stale entry from disk")
	}
	svc.RequestFetch([]int{25544})
	waitFor(t, time.Second, func() bool { return mock.callCount() == 1 })
	time.Sleep(20 * time.Millisecond)
	if got := svc.GetPrimaryTransmitter(25544); got == nil {
		t.Fatal("entry lost after failed refetch, want stale data kept")
	}
}

func TestService_DiskCache_FailedFetchNotSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "satnogs.json")
	mock := newMockFetcher()
	mock.errors[1] = errors.New("stalled")
	svc := NewService(mock).WithDiskCache(path)
	stop := runService(svc)
	defer stop()

	svc.RequestFetch([]int{1})
	waitFor(t, time.Second, func() bool { return mock.callCount() == 1 })
	time.Sleep(20 * time.Millisecond)

	if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), `"1"`) {
		t.Fatalf("failed fetch was saved: %s", data)
	}
}

func TestService_DiskCache_MissingOrBrokenFileIgnored(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing.json"), broken} {
		svc := NewService(newMockFetcher()).WithDiskCache(path)
		if n := svc.CacheSize(); n != 0 {
			t.Fatalf("%s: CacheSize = %d, want 0", path, n)
		}
	}
}
