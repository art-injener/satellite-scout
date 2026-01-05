package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-injener/satwatch-go/internal/config"
)

func TestNewAPIHandler(t *testing.T) {
	cfg := &config.Config{
		Port:        "8080",
		ObserverLat: 47.315813,
		ObserverLon: 39.788243,
		ObserverAlt: 70.0,
	}

	handler := NewAPIHandler(cfg)

	if handler == nil {
		t.Fatal("NewAPIHandler returned nil")
	}

	if handler.config != cfg {
		t.Error("NewAPIHandler did not store config correctly")
	}
}

func TestAPIHandler_HealthCheck(t *testing.T) {
	cfg := &config.Config{Port: "8080"}
	handler := NewAPIHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	handler.HealthCheck(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if status, ok := body["status"]; !ok || status != "ok" {
		t.Errorf("Expected status: ok, got %v", body)
	}
}

func TestAPIHandler_GetConfig(t *testing.T) {
	cfg := &config.Config{
		Port:        "8080",
		ObserverLat: 51.5074,
		ObserverLon: -0.1278,
		ObserverAlt: 11.0,
	}
	handler := NewAPIHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	handler.GetConfig(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	observer, ok := body["observer"].(map[string]any)
	if !ok {
		t.Fatal("Expected observer object in response")
	}

	if lat := observer["lat"].(float64); lat != cfg.ObserverLat {
		t.Errorf("Expected lat %f, got %f", cfg.ObserverLat, lat)
	}

	if lon := observer["lon"].(float64); lon != cfg.ObserverLon {
		t.Errorf("Expected lon %f, got %f", cfg.ObserverLon, lon)
	}

	if alt := observer["alt"].(float64); alt != cfg.ObserverAlt {
		t.Errorf("Expected alt %f, got %f", cfg.ObserverAlt, alt)
	}
}

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		data       any
		wantStatus int
	}{
		{
			name:       "success response",
			status:     http.StatusOK,
			data:       map[string]string{"message": "success"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "error response",
			status:     http.StatusBadRequest,
			data:       map[string]string{"error": "bad request"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "nil data",
			status:     http.StatusNoContent,
			data:       nil,
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSON(w, tt.status, tt.data)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			contentType := resp.Header.Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type application/json, got %s", contentType)
			}

			if tt.data != nil {
				var body map[string]string
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Errorf("Failed to decode response: %v", err)
				}
			}
		})
	}
}
