package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sweep"
)

func testSweep(t *testing.T) (*sweep.Service, *sdr.ReceiverHub, string) {
	t.Helper()
	hub, pathID := testHubWithImitator(t)
	svc := sweep.NewService(hub, func(string, []byte) {})
	svc.Configure(pathID, []sweep.Range{{CenterHz: 145_900_000}, {CenterHz: 437_000_000}}, time.Minute)
	t.Cleanup(func() {
		svc.Stop(pathID)
		_ = hub.CloseAll()
	})
	return svc, hub, pathID
}

func decodeSweepState(t *testing.T, w *httptest.ResponseRecorder) sweep.State {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var st sweep.State
	if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return st
}

func postSweep(h *SweepHandler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/sdr/sweep", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	h.Set(w, req)
	return w
}

func TestSweepHandler_GetListsWindows(t *testing.T) {
	svc, _, pathID := testSweep(t)
	h := NewSweepHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/sdr/sweep?radio_path="+pathID, nil)
	w := httptest.NewRecorder()
	h.Get(w, req)

	st := decodeSweepState(t, w)
	if st.Running || len(st.Windows) != 2 || st.Windows[0].CenterHz != 145_900_000 {
		t.Errorf("state %+v, want stopped sweep with two windows", st)
	}
}

func TestSweepHandler_GetNotConfigured(t *testing.T) {
	svc, _, _ := testSweep(t)
	h := NewSweepHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/sdr/sweep?radio_path=other", nil)
	w := httptest.NewRecorder()
	h.Get(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404 for a path without sweep", w.Code)
	}
}

func TestSweepHandler_StartAndStop(t *testing.T) {
	svc, hub, pathID := testSweep(t)
	h := NewSweepHandler(svc)

	st := decodeSweepState(t, postSweep(h, `{"radio_path":"`+pathID+`","running":true}`))
	if !st.Running || st.Index != 0 {
		t.Errorf("after start %+v", st)
	}
	receiver, _ := hub.Get(pathID)
	if got := receiver.Status().CenterHz; got != 145_900_000 {
		t.Errorf("receiver at %d, want first window 145900000", got)
	}

	st = decodeSweepState(t, postSweep(h, `{"radio_path":"`+pathID+`","running":false}`))
	if st.Running {
		t.Errorf("after stop %+v", st)
	}
}

func TestSweepHandler_SetBadRequests(t *testing.T) {
	svc, _, _ := testSweep(t)
	h := NewSweepHandler(svc)

	for name, tc := range map[string]struct {
		body string
		code int
	}{
		"invalid json":   {`{`, http.StatusBadRequest},
		"no radio path":  {`{"running":true}`, http.StatusBadRequest},
		"unknown field":  {`{"radio_path":"x","run":true}`, http.StatusBadRequest},
		"not configured": {`{"radio_path":"other","running":true}`, http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			if w := postSweep(h, tc.body); w.Code != tc.code {
				t.Errorf("status=%d, want %d; body: %s", w.Code, tc.code, w.Body.String())
			}
		})
	}
}

func TestSDRHandler_Tune_StopsSweep(t *testing.T) {
	svc, hub, pathID := testSweep(t)
	if err := svc.Start(pathID); err != nil {
		t.Fatalf("start: %v", err)
	}
	h := NewSDRHandler(sdr.NewService(), hub).WithSweep(svc)

	body, _ := json.Marshal(TuneRequest{RadioPath: pathID, CenterHz: 436_500_000})
	w := httptest.NewRecorder()
	h.Tune(w, httptest.NewRequest(http.MethodPost, "/api/sdr/tune", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d; body: %s", w.Code, w.Body.String())
	}
	if st, _ := svc.State(pathID); st.Running {
		t.Error("manual tune must stop the sweep")
	}
	receiver, _ := hub.Get(pathID)
	if got := receiver.Status().CenterHz; got != 436_500_000 {
		t.Errorf("receiver at %d, want manual 436500000", got)
	}
}

func TestSDRHandler_SetReceiver_StopsSweep(t *testing.T) {
	svc, hub, pathID := testSweep(t)
	if err := svc.Start(pathID); err != nil {
		t.Fatalf("start: %v", err)
	}
	h := NewSDRHandler(sdr.NewService(), hub).WithSweep(svc)
	rate, _ := hub.SampleRate(pathID)

	body, _ := json.Marshal(ReceiverRequest{RadioPath: pathID, CenterHz: 436_500_000, SampleRateHz: rate, GainDB: 20})
	w := httptest.NewRecorder()
	h.SetReceiver(w, httptest.NewRequest(http.MethodPost, "/api/sdr/receiver", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d; body: %s", w.Code, w.Body.String())
	}
	if st, _ := svc.State(pathID); st.Running {
		t.Error("receiver settings must stop the sweep")
	}
}

func TestSDRHandler_SetReceiver_RejectedKeepsSweep(t *testing.T) {
	svc, hub, pathID := testSweep(t)
	if err := svc.Start(pathID); err != nil {
		t.Fatalf("start: %v", err)
	}
	h := NewSDRHandler(sdr.NewService(), hub).WithSweep(svc)

	body, _ := json.Marshal(ReceiverRequest{RadioPath: pathID, CenterHz: 436_500_000, SampleRateHz: 1, GainDB: 20})
	w := httptest.NewRecorder()
	h.SetReceiver(w, httptest.NewRequest(http.MethodPost, "/api/sdr/receiver", bytes.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
	if st, _ := svc.State(pathID); !st.Running {
		t.Error("rejected request must not stop the sweep")
	}
}
