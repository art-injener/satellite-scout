package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sdr/imitator"
)

func TestSDRHandler_ListDevices(t *testing.T) {
	h := NewSDRHandler(sdr.NewService(), nil)

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
	h := NewSDRHandler(sdr.NewService(), nil)

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
	h := NewSDRHandler(sdr.NewService(), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/sdr/test", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.Test(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- Tune ---

func testHubWithImitator(t *testing.T) (*sdr.ReceiverHub, string) {
	t.Helper()
	hub := sdr.NewReceiverHub()
	pathID := "test-radio"
	hub.Register(pathID, func() (sdr.Receiver, error) {
		r := imitator.New(imitator.DefaultOptions())
		_ = r.Tune(145_800_000)
		return r, nil
	})
	return hub, pathID
}

func TestSDRHandler_Tune_OK(t *testing.T) {
	hub, pathID := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	gain := float32(28.0)
	body, _ := json.Marshal(TuneRequest{
		RadioPath: pathID,
		CenterHz:  437_365_000,
		GainDB:    &gain,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/sdr/tune", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Tune(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp TuneResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if resp.Status.CenterHz != 437_365_000 {
		t.Errorf("center_hz=%d, want 437365000", resp.Status.CenterHz)
	}
	if resp.Status.GainDB != 28.0 {
		t.Errorf("gain_db=%f, want 28.0", resp.Status.GainDB)
	}
}

func TestSDRHandler_Tune_MissingRadioPath(t *testing.T) {
	hub, _ := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	body, _ := json.Marshal(TuneRequest{CenterHz: 100_000_000})
	req := httptest.NewRequest(http.MethodPost, "/api/sdr/tune", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Tune(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestSDRHandler_Tune_FreqOutOfRange(t *testing.T) {
	hub, pathID := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	body, _ := json.Marshal(TuneRequest{
		RadioPath: pathID,
		CenterHz:  1, // ниже 24 МГц
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sdr/tune", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Tune(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestSDRHandler_Tune_NoHub(t *testing.T) {
	h := NewSDRHandler(sdr.NewService(), nil)

	body, _ := json.Marshal(TuneRequest{RadioPath: "x", CenterHz: 100_000_000})
	req := httptest.NewRequest(http.MethodPost, "/api/sdr/tune", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Tune(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", w.Code)
	}
}

// --- GetStatus ---

func TestSDRHandler_GetStatus_OK(t *testing.T) {
	hub, pathID := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	req := httptest.NewRequest(http.MethodGet, "/api/sdr/status?radio_path="+pathID, nil)
	w := httptest.NewRecorder()
	h.GetStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var st sdr.ReceiverStatus
	if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.CenterHz != 145_800_000 {
		t.Errorf("center_hz=%d, want 145800000", st.CenterHz)
	}
}

func TestSDRHandler_GetStatus_MissingRadioPath(t *testing.T) {
	hub, _ := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	req := httptest.NewRequest(http.MethodGet, "/api/sdr/status", nil)
	w := httptest.NewRecorder()
	h.GetStatus(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestSDRHandler_GetStatus_UnknownPath(t *testing.T) {
	hub, _ := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	req := httptest.NewRequest(http.MethodGet, "/api/sdr/status?radio_path=nope", nil)
	w := httptest.NewRecorder()
	h.GetStatus(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}
