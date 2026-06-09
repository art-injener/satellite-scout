package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

func TestSDRHandler_ListDevices(t *testing.T) {
	h := NewSDRHandler(sdr.NewService())

	req := httptest.NewRequest(http.MethodGet, "/api/sdr/devices", nil)
	w := httptest.NewRecorder()
	h.ListDevices(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Devices []struct {
			Driver string `json:"driver"`
		} `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Devices) == 0 || body.Devices[0].Driver != "simulated" {
		t.Errorf("devices = %+v, want simulated first", body.Devices)
	}
}

func TestSDRHandler_TestSimulated(t *testing.T) {
	h := NewSDRHandler(sdr.NewService())

	body, _ := json.Marshal(map[string]string{"driver": "simulated"})
	req := httptest.NewRequest(http.MethodPost, "/api/sdr/test", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Test(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ack.OK {
		t.Error("expected ok=true for simulated")
	}
}

func TestSDRHandler_TestMissingDriver(t *testing.T) {
	h := NewSDRHandler(sdr.NewService())

	req := httptest.NewRequest(http.MethodPost, "/api/sdr/test", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.Test(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
