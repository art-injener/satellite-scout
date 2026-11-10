package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func openPanorama(t *testing.T, h *StreamHandler, pathID string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.Panorama))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?radio_path="+pathID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET panorama: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestPanorama_StreamsCurrentWindow(t *testing.T) {
	svc, hub, pathID := testSweep(t)
	if err := svc.Start(pathID); err != nil {
		t.Fatalf("start: %v", err)
	}
	resp := openPanorama(t, NewStreamHandler(hub).WithSweep(svc), pathID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	f := readSpectrumFrame(t, resp.Body)
	if f.centerHz != 145_900_000 || len(f.dbfs) == 0 {
		t.Errorf("frame center %d with %d bins, want first window 145900000", f.centerHz, len(f.dbfs))
	}
	if f.spanHz == 0 {
		t.Error("frame must carry the window span")
	}
}

func TestPanorama_SendsStoredSnapshotsOnConnect(t *testing.T) {
	svc, hub, pathID := testSweep(t)
	_, updates, cancel, err := svc.Watch(pathID)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if err := svc.Start(pathID); err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("no snapshot from the imitator")
	}
	cancel()
	svc.Stop(pathID)

	resp := openPanorama(t, NewStreamHandler(hub).WithSweep(svc), pathID)
	if f := readSpectrumFrame(t, resp.Body); f.centerHz != 145_900_000 {
		t.Errorf("stored frame center %d, want 145900000", f.centerHz)
	}
}

func TestPanorama_NotConfigured(t *testing.T) {
	svc, hub, _ := testSweep(t)
	resp := openPanorama(t, NewStreamHandler(hub).WithSweep(svc), "other")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d, want 404", resp.StatusCode)
	}
}
