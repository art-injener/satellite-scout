package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestNewPageHandler(t *testing.T) {
	fsys := setupTestFS()

	handler, err := NewPageHandler(fsys, false, "default")
	if err != nil {
		t.Fatalf("NewPageHandler failed: %v", err)
	}

	if handler == nil {
		t.Fatal("NewPageHandler returned nil")
	}

	if handler.devMode {
		t.Error("Expected devMode to be false")
	}
}

func TestNewPageHandler_InvalidFS(t *testing.T) {
	// Пустая FS без шаблонов — должна вернуть ошибку.
	fsys := fstest.MapFS{}

	_, err := NewPageHandler(fsys, false, "default")
	if err == nil {
		t.Error("Expected error for empty FS, got nil")
	}
}

func TestPageHandler_Index(t *testing.T) {
	fsys := setupTestFS()

	handler, err := NewPageHandler(fsys, false, "default")
	if err != nil {
		t.Fatalf("NewPageHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.Index(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("Expected status 302, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location != "/tracking" {
		t.Errorf("Expected redirect to /tracking, got %s", location)
	}
}

func TestPageHandler_Tracking(t *testing.T) {
	fsys := setupTestFS()
	handler, err := NewPageHandler(fsys, false, "default")
	if err != nil {
		t.Fatalf("NewPageHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/tracking", nil)
	w := httptest.NewRecorder()

	handler.Tracking(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("Expected Content-Type text/html; charset=utf-8, got %s", contentType)
	}
}

func TestPageHandler_Receiver(t *testing.T) {
	fsys := setupTestFS()
	handler, err := NewPageHandler(fsys, false, "default")
	if err != nil {
		t.Fatalf("NewPageHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/receiver", nil)
	w := httptest.NewRecorder()

	handler.Receiver(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestPageHandler_Passes(t *testing.T) {
	fsys := setupTestFS()
	handler, err := NewPageHandler(fsys, false, "default")
	if err != nil {
		t.Fatalf("NewPageHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/passes", nil)
	w := httptest.NewRecorder()

	handler.Passes(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestPageHandler_Simulation(t *testing.T) {
	fsys := setupTestFS()
	handler, err := NewPageHandler(fsys, false, "default")
	if err != nil {
		t.Fatalf("NewPageHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/simulation", nil)
	w := httptest.NewRecorder()

	handler.Simulation(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestPageHandler_DevMode(t *testing.T) {
	fsys := setupTestFS()
	handler, err := NewPageHandler(fsys, true, "default")
	if err != nil {
		t.Fatalf("NewPageHandler failed: %v", err)
	}

	if !handler.devMode {
		t.Error("Expected devMode to be true")
	}

	req := httptest.NewRequest(http.MethodGet, "/tracking", nil)
	w := httptest.NewRecorder()

	handler.Tracking(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestPageData(t *testing.T) {
	data := PageData{
		Title:     "Test Title",
		ActiveTab: "tracking",
	}

	if data.Title != "Test Title" {
		t.Errorf("Expected Title 'Test Title', got %s", data.Title)
	}

	if data.ActiveTab != "tracking" {
		t.Errorf("Expected ActiveTab 'tracking', got %s", data.ActiveTab)
	}
}

// setupTestFS создаёт in-memory файловую систему с минимальными шаблонами.
func setupTestFS() fstest.MapFS {
	return fstest.MapFS{
		"layouts/base.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html>
<html>
<head><title>{{.Title}}</title></head>
<body>
	<div class="active-tab">{{.ActiveTab}}</div>
	<div class="content">Test Content</div>
</body>
</html>`),
		},
		"pages/tracking.html":   &fstest.MapFile{Data: []byte(`{{define "tracking"}}tracking{{end}}`)},
		"pages/receiver.html":   &fstest.MapFile{Data: []byte(`{{define "receiver"}}receiver{{end}}`)},
		"pages/passes.html":     &fstest.MapFile{Data: []byte(`{{define "passes"}}passes{{end}}`)},
		"pages/simulation.html": &fstest.MapFile{Data: []byte(`{{define "simulation"}}simulation{{end}}`)},
	}
}
