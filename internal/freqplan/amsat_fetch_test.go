package freqplan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFetchAMSAT_DownloadsAndUsesCache(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(amsatSample))
	}))
	defer srv.Close()

	cache := filepath.Join(t.TempDir(), "amsat.csv")
	src, err := FetchAMSAT(context.Background(), srv.Client(), srv.URL, cache, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Transmitters(27607)) != 1 {
		t.Fatalf("after download: %+v", src.Transmitters(27607))
	}
	if hits != 1 {
		t.Fatalf("hits %d, want 1", hits)
	}

	src2, err := FetchAMSAT(context.Background(), srv.Client(), srv.URL, cache, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("fresh cache must not refetch, hits %d", hits)
	}
	if len(src2.Transmitters(27607)) != 1 {
		t.Fatalf("from cache: %+v", src2.Transmitters(27607))
	}
}

func TestFetchAMSAT_NetworkErrorFallsBackToCache(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "amsat.csv")
	if err := os.WriteFile(cache, []byte(amsatSample), 0o644); err != nil {
		t.Fatal(err)
	}
	// Старый кеш, чтобы пойти в сеть.
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(cache, old, old); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	src, err := FetchAMSAT(context.Background(), client, "http://127.0.0.1:1/nope", cache, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Transmitters(27607)) != 1 {
		t.Fatalf("fallback cache: %+v", src.Transmitters(27607))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
