package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewPageHandler(t *testing.T) {
	// Создаём временную директорию для тестов
	tmpDir := t.TempDir()
	layoutsDir := filepath.Join(tmpDir, "layouts")
	pagesDir := filepath.Join(tmpDir, "pages")

	if err := os.MkdirAll(layoutsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Создаём тестовые шаблоны
	baseTemplate := `<!DOCTYPE html><html><body>{{template "content" .}}</body></html>`
	if err := os.WriteFile(filepath.Join(layoutsDir, "base.html"), []byte(baseTemplate), 0o644); err != nil {
		t.Fatal(err)
	}

	pageTemplate := `{{define "content"}}<h1>Test Page</h1>{{end}}`
	if err := os.WriteFile(filepath.Join(pagesDir, "test.html"), []byte(pageTemplate), 0o644); err != nil {
		t.Fatal(err)
	}

	handler, err := NewPageHandler(tmpDir, false)
	if err != nil {
		t.Fatalf("NewPageHandler failed: %v", err)
	}

	if handler == nil {
		t.Fatal("NewPageHandler returned nil")
	}

	if handler.tmplDir != tmpDir {
		t.Errorf("Expected tmplDir %s, got %s", tmpDir, handler.tmplDir)
	}

	if handler.devMode {
		t.Error("Expected devMode to be false")
	}
}

func TestNewPageHandler_InvalidDirectory(t *testing.T) {
	_, err := NewPageHandler("/nonexistent/directory", false)
	if err == nil {
		t.Error("Expected error for nonexistent directory, got nil")
	}
}

func TestPageHandler_Index(t *testing.T) {
	// Создаём минимальный handler для теста редиректа
	tmpDir := t.TempDir()
	layoutsDir := filepath.Join(tmpDir, "layouts")
	if err := os.MkdirAll(layoutsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	baseTemplate := `<!DOCTYPE html><html><body>Test</body></html>`
	if err := os.WriteFile(filepath.Join(layoutsDir, "base.html"), []byte(baseTemplate), 0o644); err != nil {
		t.Fatal(err)
	}

	handler, err := NewPageHandler(tmpDir, false)
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
	tmpDir := setupTestTemplates(t)
	handler, err := NewPageHandler(tmpDir, false)
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
	tmpDir := setupTestTemplates(t)
	handler, err := NewPageHandler(tmpDir, false)
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

func TestPageHandler_Simulation(t *testing.T) {
	tmpDir := setupTestTemplates(t)
	handler, err := NewPageHandler(tmpDir, false)
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
	tmpDir := setupTestTemplates(t)
	handler, err := NewPageHandler(tmpDir, true)
	if err != nil {
		t.Fatalf("NewPageHandler failed: %v", err)
	}

	if !handler.devMode {
		t.Error("Expected devMode to be true")
	}

	// В dev режиме шаблоны должны перезагружаться при каждом запросе
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

// setupTestTemplates создаёт минимальную структуру шаблонов для тестов.
func setupTestTemplates(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	layoutsDir := filepath.Join(tmpDir, "layouts")

	if err := os.MkdirAll(layoutsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	baseTemplate := `<!DOCTYPE html>
<html>
<head><title>{{.Title}}</title></head>
<body>
	<div class="active-tab">{{.ActiveTab}}</div>
	<div class="content">Test Content</div>
</body>
</html>`

	if err := os.WriteFile(filepath.Join(layoutsDir, "base.html"), []byte(baseTemplate), 0o644); err != nil {
		t.Fatal(err)
	}

	return tmpDir
}
