package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/art-injener/satellite-scout/internal/access"
	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sweep"
)

func TestSetupRoutes_LegacyRedirects(t *testing.T) {
	mux := http.NewServeMux()
	setupRoutes(mux, minimalRouteDeps(t))

	cases := []struct {
		path     string
		wantCode int
		wantLoc  string
	}{
		{"/", http.StatusFound, "/tracking"},
		{"/settings", http.StatusFound, "/tracking?settings=open"},
		{"/receiver", http.StatusFound, "/tracking"},
		{"/simulation", http.StatusFound, "/tracking"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if got := rec.Header().Get("Location"); got != tc.wantLoc {
				t.Fatalf("Location = %q, want %q", got, tc.wantLoc)
			}
		})
	}
}

func TestSetupRoutes_APIRegistered(t *testing.T) {
	mux := http.NewServeMux()
	setupRoutes(mux, minimalRouteDeps(t))

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/health"},
		{http.MethodGet, "/api/config"},
		{http.MethodGet, "/api/settings"},
		{http.MethodGet, "/api/exclusions"},
		{http.MethodGet, "/api/sdr/devices"},
		{http.MethodGet, "/api/sdr/sweep"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code == http.StatusNotFound {
				t.Fatalf("route not registered")
			}
		})
	}
}

func TestSetupRoutes_ChangesRequireMaster(t *testing.T) {
	mux := http.NewServeMux()
	setupRoutes(mux, minimalRouteDeps(t))

	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/settings"},
		{http.MethodPost, "/api/sdr/tune"},
		{http.MethodPost, "/api/sdr/receiver"},
		{http.MethodPost, "/api/sdr/channel"},
		{http.MethodPost, "/api/sdr/demod"},
		{http.MethodPost, "/api/sdr/sweep"},
		{http.MethodPost, "/api/access/decide"},
		{http.MethodPost, "/api/access/revoke"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.RemoteAddr = "192.168.1.40:50000"
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("remote viewer: status=%d, want 403", rec.Code)
			}
		})
	}

	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader("{"))
	req.RemoteAddr = "127.0.0.1:50000"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Error("local master must reach settings handler")
	}
}

func TestSetupRoutes_AccessRegistered(t *testing.T) {
	mux := http.NewServeMux()
	setupRoutes(mux, minimalRouteDeps(t))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/access", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/access: status=%d", rec.Code)
	}
	for _, path := range []string{"/api/access/login", "/api/access/request", "/api/access/release"} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{")))
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("POST %s not registered: %d", path, rec.Code)
		}
	}
}

func minimalRouteDeps(t *testing.T) *routeDeps {
	t.Helper()

	cfg := config.DefaultConfig()
	store := config.NewStore(t.TempDir() + "/config.json")
	store.Set(cfg)

	templates := testTemplatesFS()
	static := fstest.MapFS{
		"css/main.css": &fstest.MapFile{Data: []byte("body{}")},
	}

	return &routeDeps{
		Cfg:         cfg,
		ConfigStore: store,
		Templates:   templates,
		Static:      static,
		SSE:         handlers.NewSSEHub(),
		Access:      access.NewManager("test-password"),
		Tracking:    &noopTrackingService{},
		Exclude:     &noopExcludeStore{},
		PassCache:   &noopPassCache{},
		Group:       &noopGroupRefresher{},
		SDR:         sdr.NewService(),
	}
}

type noopTrackingService struct{}

func (noopTrackingService) SetManualSelection(noradID int, clientID string) {}
func (noopTrackingService) ResetManualSelection(clientID string)            {}

type noopExcludeStore struct{}

func (noopExcludeStore) Add(norad int) error    { return nil }
func (noopExcludeStore) Remove(norad int) error { return nil }
func (noopExcludeStore) List() []int            { return nil }

type noopPassCache struct{}

func (noopPassCache) InvalidateCache() {}

type noopGroupRefresher struct{}

func (noopGroupRefresher) ForceGroupUpdate() {}

func testTemplatesFS() fs.FS {
	return fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head><title>{{.Title}}</title></head><body></body></html>`),
		},
		"pages/tracking.html": &fstest.MapFile{
			Data: []byte(`{{define "tracking-content"}}ok{{end}}`),
		},
	}
}

func TestSweepRanges_MHzToHz(t *testing.T) {
	got := sweepRanges(&config.SweepConfig{Ranges: []config.SweepRange{
		{CenterMHz: 145.9},
		{MinMHz: 435.4, MaxMHz: 437.8},
	}})
	want := []sweep.Range{{CenterHz: 145_900_000}, {LoHz: 435_400_000, HiHz: 437_800_000}}
	if !slices.Equal(got, want) {
		t.Errorf("sweepRanges = %+v, want %+v", got, want)
	}
}
