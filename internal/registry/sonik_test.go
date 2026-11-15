package registry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Фрагмент ответа sonik.space /transmitters/data/: NORAD бывает пустой строкой, baud — null.
const sonikTxBody = `{"data": [
	{"sat_id": "XSKZ", "sat_norad": 25544, "sat_name": "ISS", "description": "Mode V/V APRS", "type": "Transceiver",
	 "downlink_low": 145825000, "downlink_mode": "AFSK", "status": "inactive", "uuid": "4Yp5", "baud": 1200},
	{"sat_id": "XSKZ", "sat_norad": 25544, "sat_name": "ISS", "description": "Voice", "type": "Transmitter",
	 "downlink_low": 437800000, "downlink_mode": "FM", "status": "active", "uuid": "aB12", "baud": null},
	{"sat_id": "", "sat_norad": "", "sat_name": "", "description": "Unknown", "type": "Transmitter",
	 "downlink_low": 401250000, "downlink_mode": "GMSK", "status": "active", "uuid": "zz99", "baud": 9600},
	{"sat_id": "NOFR", "sat_norad": 43017, "sat_name": "AO-91", "description": "No freq", "type": "Transmitter",
	 "downlink_low": null, "downlink_mode": "FM", "status": "active", "uuid": "nf01", "baud": null}
]}`

func newFakeSonikTx(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func TestParseSonikTransmitters(t *testing.T) {
	got, err := ParseSonikTransmitters([]byte(sonikTxBody), testAt)
	if err != nil {
		t.Fatalf("ParseSonikTransmitters: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d transmitters, want 3 (empty NORAD skipped): %+v", len(got), got)
	}
	want := Transmitter{
		UUID: "4Yp5", NoradID: 25544, Name: "Mode V/V APRS", DownlinkHz: 145_825_000,
		Mode: "AFSK", Baud: 1200, Status: "inactive", Source: SourceSonik,
	}
	if !got[0].sameRecord(want) || !got[0].FetchedAt.Equal(testAt) {
		t.Fatalf("first = %+v, want %+v", got[0], want)
	}
	if got[1].Baud != 0 || got[1].Alive != nil {
		t.Fatalf("second = %+v, want zero baud and unknown alive", got[1])
	}
	if got[2].DownlinkHz != 0 || got[2].NoradID != 43017 {
		t.Fatalf("third = %+v, want NORAD 43017 without downlink", got[2])
	}
}

func TestSonikTx_LoadFreshCacheSkipsNetwork(t *testing.T) {
	srv, hits := newFakeSonikTx(t, http.StatusOK, sonikTxBody)
	path := filepath.Join(t.TempDir(), "sonik_transmitters.json")
	if err := os.WriteFile(path, []byte(sonikTxBody), 0o600); err != nil {
		t.Fatal(err)
	}

	txs, at, err := NewSonikTxClient(WithSonikTxURL(srv.URL)).Load(context.Background(), path, time.Hour)
	if err != nil || len(txs) != 3 {
		t.Fatalf("got %d, err %v; want 3 from cache", len(txs), err)
	}
	if hits.Load() != 0 {
		t.Fatalf("network hits = %d, want 0 on fresh cache", hits.Load())
	}
	if time.Since(at) > time.Minute {
		t.Fatalf("at = %v, want file modification time", at)
	}
}

func TestSonikTx_LoadStaleCacheDownloadsAndSaves(t *testing.T) {
	srv, hits := newFakeSonikTx(t, http.StatusOK, sonikTxBody)
	path := filepath.Join(t.TempDir(), "sonik_transmitters.json")
	if err := os.WriteFile(path, []byte(`{"data": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	txs, _, err := NewSonikTxClient(WithSonikTxURL(srv.URL)).Load(context.Background(), path, time.Hour)
	if err != nil || len(txs) != 3 || hits.Load() != 1 {
		t.Fatalf("got %d, err %v, hits %d; want 3 from network", len(txs), err, hits.Load())
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != sonikTxBody {
		t.Fatalf("cache file not replaced with raw response: %v", err)
	}
}

func TestSonikTx_LoadNetworkErrorFallsBackToCache(t *testing.T) {
	srv, _ := newFakeSonikTx(t, http.StatusBadGateway, "")
	path := filepath.Join(t.TempDir(), "sonik_transmitters.json")
	if err := os.WriteFile(path, []byte(sonikTxBody), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	txs, at, err := NewSonikTxClient(WithSonikTxURL(srv.URL)).Load(context.Background(), path, time.Hour)
	if !errors.Is(err, ErrSonikTxStatus) {
		t.Fatalf("err = %v, want ErrSonikTxStatus", err)
	}
	if len(txs) != 3 || at.Unix() != old.Unix() {
		t.Fatalf("got %d at %v, want 3 cached from %v", len(txs), at, old)
	}
}

func TestSonikTx_LoadBrokenResponseKeepsCache(t *testing.T) {
	srv, _ := newFakeSonikTx(t, http.StatusOK, `{"data": [`)
	path := filepath.Join(t.TempDir(), "sonik_transmitters.json")
	if err := os.WriteFile(path, []byte(sonikTxBody), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	txs, _, err := NewSonikTxClient(WithSonikTxURL(srv.URL)).Load(context.Background(), path, time.Hour)
	if err == nil || len(txs) != 3 {
		t.Fatalf("got %d, err %v; want error and 3 cached", len(txs), err)
	}
	saved, _ := os.ReadFile(path)
	if string(saved) != sonikTxBody {
		t.Fatal("broken response must not overwrite cache")
	}
}

func TestSonikTx_LoadNoCacheNoNetworkFails(t *testing.T) {
	srv, _ := newFakeSonikTx(t, http.StatusInternalServerError, "")
	path := filepath.Join(t.TempDir(), "sonik_transmitters.json")
	txs, _, err := NewSonikTxClient(WithSonikTxURL(srv.URL)).Load(context.Background(), path, time.Hour)
	if err == nil || len(txs) != 0 {
		t.Fatalf("got %d, err %v; want error and nothing", len(txs), err)
	}
}
