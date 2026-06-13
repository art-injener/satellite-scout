package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/handlers"
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
		Tracking:    &noopTrackingService{},
		Exclude:     &noopExcludeStore{},
		PassCache:   &noopPassCache{},
		Group:       &noopGroupRefresher{},
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
