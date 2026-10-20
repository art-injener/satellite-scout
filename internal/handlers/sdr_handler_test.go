package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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
	hub.Register(pathID, sdr.PathConfig{}, func() (sdr.Receiver, error) {
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
	if resp.Status.Channel != sdr.DefaultChannelSettings() {
		t.Errorf("channel=%+v, want defaults", resp.Status.Channel)
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

// --- SetReceiver ---

func postReceiver(t *testing.T, h *SDRHandler, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sdr/receiver", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.SetReceiver(w, req)
	return w
}

func getStatus(t *testing.T, h *SDRHandler, pathID string) StatusResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sdr/status?radio_path="+pathID, nil)
	w := httptest.NewRecorder()
	h.GetStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var st StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return st
}

func TestSDRHandler_SetReceiver_StoresAndReportsInStatus(t *testing.T) {
	hub, pathID := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	want := sdr.ChannelSettings{OffsetHz: 2_500, BandwidthHz: 30_000, SquelchDBFS: -72.5, Demod: "fsk9600_g3ruh"}
	body, _ := json.Marshal(map[string]any{
		"radio_path":     pathID,
		"sample_rate_hz": 256_000,
		"gain_db":        32,
		"offset_hz":      want.OffsetHz,
		"bandwidth_hz":   want.BandwidthHz,
		"squelch_dbfs":   want.SquelchDBFS,
		"demod":          want.Demod,
	})
	w := postReceiver(t, h, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp TuneResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK || resp.Status.GainDB != 32 || resp.Status.Channel != want {
		t.Errorf("response=%+v, want ok, gain 32, channel %+v", resp, want)
	}

	st := getStatus(t, h, pathID)
	if st.GainDB != 32 {
		t.Errorf("status gain_db=%v, want 32", st.GainDB)
	}
	if st.Channel != want {
		t.Errorf("status channel=%+v, want %+v", st.Channel, want)
	}
	if st.SampleRate != 256_000 {
		t.Errorf("status sample_rate=%d, want 256000", st.SampleRate)
	}
}

func TestSDRHandler_GetStatus_SampleRateAndChoicesWithoutStream(t *testing.T) {
	hub, pathID := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	st := getStatus(t, h, pathID)
	if st.SampleRate != 2_048_000 {
		t.Errorf("sample_rate=%d without a stream, want the path rate 2048000", st.SampleRate)
	}
	if want := []uint32{2_048_000, 1_024_000, 512_000, 256_000}; !slices.Equal(st.SampleRates, want) {
		t.Errorf("sample_rates=%v, want %v", st.SampleRates, want)
	}
}

func TestSDRHandler_SetReceiver_RateRejections(t *testing.T) {
	for name, tc := range map[string]struct {
		rate   uint32
		offset int64
	}{
		"rate outside choices":         {rate: 300_000},
		"channel outside the new band": {rate: 256_000, offset: 120_000},
	} {
		t.Run(name, func(t *testing.T) {
			hub, pathID := testHubWithImitator(t)
			h := NewSDRHandler(sdr.NewService(), hub)
			body, _ := json.Marshal(map[string]any{
				"radio_path": pathID, "sample_rate_hz": tc.rate, "gain_db": 20,
				"offset_hz": tc.offset, "bandwidth_hz": 30_000, "squelch_dbfs": -90, "demod": "afsk1200",
			})
			if w := postReceiver(t, h, body); w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400; body: %s", w.Code, w.Body.String())
			}
			st := getStatus(t, h, pathID)
			if st.SampleRate != 2_048_000 || st.GainDB != 0 {
				t.Errorf("sample_rate=%d gain=%v after rejected request, want 2048000 and 0", st.SampleRate, st.GainDB)
			}
		})
	}
}

func TestSDRHandler_SetReceiver_InvalidChangesNothing(t *testing.T) {
	hub, pathID := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	body, _ := json.Marshal(map[string]any{
		"radio_path":     pathID,
		"sample_rate_hz": 2_048_000,
		"gain_db":        32,
		"offset_hz":      0,
		"bandwidth_hz":   15_000,
		"squelch_dbfs":   -90,
		"demod":          "lora",
	})
	w := postReceiver(t, h, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body: %s", w.Code, w.Body.String())
	}

	st := getStatus(t, h, pathID)
	if st.GainDB != 0 {
		t.Errorf("gain_db=%v after rejected request, want 0", st.GainDB)
	}
	if st.Channel != sdr.DefaultChannelSettings() {
		t.Errorf("channel=%+v after rejected request, want defaults", st.Channel)
	}
}

func TestSDRHandler_SetReceiver_BadRequests(t *testing.T) {
	hub, pathID := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	for name, body := range map[string]string{
		"unknown field": `{"radio_path":"` + pathID + `","bandwidth_hz":15000,"demod":"afsk1200","volume":3}`,
		"no radio path": `{"bandwidth_hz":15000,"demod":"afsk1200"}`,
		"unknown path":  `{"radio_path":"nope","bandwidth_hz":15000,"demod":"afsk1200"}`,
		"broken json":   `{`,
	} {
		t.Run(name, func(t *testing.T) {
			if w := postReceiver(t, h, []byte(body)); w.Code != http.StatusBadRequest {
				t.Errorf("status=%d, want 400; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSDRHandler_SetReceiver_NoHub(t *testing.T) {
	h := NewSDRHandler(sdr.NewService(), nil)
	if w := postReceiver(t, h, []byte(`{"radio_path":"x"}`)); w.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", w.Code)
	}
}

func TestSDRHandler_GetStatus_IncludesChannel(t *testing.T) {
	hub, pathID := testHubWithImitator(t)
	h := NewSDRHandler(sdr.NewService(), hub)

	if st := getStatus(t, h, pathID); st.Channel != sdr.DefaultChannelSettings() {
		t.Errorf("channel=%+v, want defaults", st.Channel)
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
