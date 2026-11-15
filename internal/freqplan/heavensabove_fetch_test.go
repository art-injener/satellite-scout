package freqplan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHeavensAboveURL_UsesObserverAndEnglish(t *testing.T) {
	u := HeavensAboveURL(47.2357, 39.7015)

	for _, part := range []string{"AmateurSats.aspx", "lat=47.2357", "lng=39.7015", "cul=en"} {
		if !strings.Contains(u, part) {
			t.Fatalf("url %q has no %q", u, part)
		}
	}
}

func TestFetchHeavensAbove_DownloadsAndUsesFreshCache(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(haSample))
	}))
	defer srv.Close()
	cache := filepath.Join(t.TempDir(), "ha.html")

	for range 2 {
		src, err := FetchHeavensAbove(context.Background(), srv.Client(), srv.URL, cache, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if len(src.Transmitters(25544)) != 1 {
			t.Fatalf("ISS = %+v", src.Transmitters(25544))
		}
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (second call from fresh cache)", hits)
	}
}

func TestFetchHeavensAbove_NetworkErrorFallsBackToCache(t *testing.T) {
	cache := writeStaleHACache(t)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}

	src, err := FetchHeavensAbove(context.Background(), client, "http://127.0.0.1:1/nope", cache, time.Hour)

	if err != nil {
		t.Fatal(err)
	}
	if len(src.Transmitters(25544)) != 1 {
		t.Fatalf("fallback cache: %+v", src.Transmitters(25544))
	}
}

func TestFetchHeavensAbove_BrokenPageKeepsOldCache(t *testing.T) {
	cache := writeStaleHACache(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>new layout</html>"))
	}))
	defer srv.Close()

	src, err := FetchHeavensAbove(context.Background(), srv.Client(), srv.URL, cache, time.Hour)

	if err != nil {
		t.Fatal(err)
	}
	if len(src.Transmitters(25544)) != 1 {
		t.Fatalf("old cache lost: %+v", src.Transmitters(25544))
	}
	data, _ := os.ReadFile(cache)
	if !strings.Contains(string(data), "standardTable") {
		t.Fatal("broken page overwrote the cache")
	}
}

func TestFetchHeavensAbove_NoCacheNoNetworkIsEmpty(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "ha.html")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}

	src, err := FetchHeavensAbove(context.Background(), client, "http://127.0.0.1:1/nope", cache, time.Hour)

	if err == nil {
		t.Fatal("want error when neither network nor cache is available")
	}
	if src == nil || len(src.Transmitters(25544)) != 0 {
		t.Fatalf("src = %+v, want empty source", src)
	}
}

func TestFetchHeavensAbove_OfflineCacheDatesFromFileTime(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "ha.html")
	if err := os.WriteFile(cache, []byte(haSample), 0o600); err != nil {
		t.Fatal(err)
	}
	downloaded := time.Date(2025, time.November, 12, 9, 0, 0, 0, time.UTC)
	if err := os.Chtimes(cache, downloaded, downloaded); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}

	src, err := FetchHeavensAbove(context.Background(), client, "http://127.0.0.1:1/nope", cache, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	passes := src.Passes()
	if len(passes) == 0 || passes[0].Start.Year() != 2025 {
		t.Fatalf("passes = %+v, want year 2025 from cache file time", passes)
	}
}

// writeStaleHACache writes the sample page as a cache older than any TTL in tests.
func writeStaleHACache(t *testing.T) string {
	t.Helper()
	cache := filepath.Join(t.TempDir(), "ha.html")
	if err := os.WriteFile(cache, []byte(haSample), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(cache, old, old); err != nil {
		t.Fatal(err)
	}
	return cache
}
