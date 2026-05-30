package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-injener/satellite-scout/internal/config"
)

func TestNewAPIHandler(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: "8080"},
		Station: config.StationConfig{
			Observer: config.ObserverConfig{Lat: 47.315813, Lon: 39.788243, AltM: 70.0},
		},
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
	cfg := &config.Config{Server: config.ServerConfig{Port: "8080"}}
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

	var body HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Status != "ok" {
		t.Errorf("Expected status: ok, got %s", body.Status)
	}
}

func TestAPIHandler_GetConfig(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: "8080"},
		Station: config.StationConfig{
			Observer: config.ObserverConfig{Lat: 51.5074, Lon: -0.1278, AltM: 11.0},
		},
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

	var body ConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body.Observer.Lat != cfg.Station.Observer.Lat {
		t.Errorf("Expected lat %f, got %f", cfg.Station.Observer.Lat, body.Observer.Lat)
	}

	if body.Observer.Lon != cfg.Station.Observer.Lon {
		t.Errorf("Expected lon %f, got %f", cfg.Station.Observer.Lon, body.Observer.Lon)
	}

	if body.Observer.Alt != cfg.Station.Observer.AltM {
		t.Errorf("Expected alt %f, got %f", cfg.Station.Observer.AltM, body.Observer.Alt)
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
			data:       HealthResponse{Status: "ok"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "error response",
			status:     http.StatusBadRequest,
			data:       ErrorResponse{Error: "bad request"},
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
				var body map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Errorf("Failed to decode response: %v", err)
				}
			}
		})
	}
}
