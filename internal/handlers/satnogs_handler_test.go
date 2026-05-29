package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-injener/satellite-scout/internal/satnogs"
)

// mockSatNOGSProvider — минимальный мок для тестов хендлера.
type mockSatNOGSProvider struct {
	allByID     map[int][]satnogs.Transmitter
	primaryByID map[int]*satnogs.TransmitterSummary
}

func (m *mockSatNOGSProvider) GetAllTransmitters(noradID int) []satnogs.Transmitter {
	return m.allByID[noradID]
}

func (m *mockSatNOGSProvider) GetPrimaryTransmitter(noradID int) *satnogs.TransmitterSummary {
	return m.primaryByID[noradID]
}

func TestSatNOGSHandler_GetTransmitters_Success(t *testing.T) {
	hz := int64(145_825_000)
	mock := &mockSatNOGSProvider{
		allByID: map[int][]satnogs.Transmitter{
			25544: {
				{UUID: "a", Alive: true, Status: "active", DownlinkLow: &hz, Mode: "FM"},
			},
		},
		primaryByID: map[int]*satnogs.TransmitterSummary{
			25544: {UUID: "a", FreqMHz: "145.825", Mode: "FM", Modulation: "FM"},
		},
	}
	h := NewSatNOGSHandler(mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/satnogs/transmitters/{norad}", h.GetTransmitters)

	req := httptest.NewRequest(http.MethodGet, "/api/satnogs/transmitters/25544", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp SatNOGSTransmittersResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.NoradID != 25544 {
		t.Errorf("NoradID = %d, want 25544", resp.NoradID)
	}
	if resp.Count != 1 {
		t.Errorf("Count = %d, want 1", resp.Count)
	}
	if resp.Primary == nil || resp.Primary.FreqMHz != "145.825" {
		t.Errorf("Primary = %+v, want FreqMHz=145.825", resp.Primary)
	}
}

func TestSatNOGSHandler_GetTransmitters_InvalidID(t *testing.T) {
	h := NewSatNOGSHandler(&mockSatNOGSProvider{})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/satnogs/transmitters/{norad}", h.GetTransmitters)

	cases := []string{"abc", "0", "-1"}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/satnogs/transmitters/"+id, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for id=%q", rec.Code, id)
			}
		})
	}
}

func TestSatNOGSHandler_GetTransmitters_EmptyResult(t *testing.T) {
	mock := &mockSatNOGSProvider{
		allByID:     map[int][]satnogs.Transmitter{},
		primaryByID: map[int]*satnogs.TransmitterSummary{},
	}
	h := NewSatNOGSHandler(mock)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/satnogs/transmitters/{norad}", h.GetTransmitters)

	req := httptest.NewRequest(http.MethodGet, "/api/satnogs/transmitters/99999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp SatNOGSTransmittersResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.NoradID != 99999 {
		t.Errorf("NoradID = %d, want 99999", resp.NoradID)
	}
	if resp.Count != 0 {
		t.Errorf("Count = %d, want 0", resp.Count)
	}
	if resp.Primary != nil {
		t.Errorf("Primary = %+v, want nil", resp.Primary)
	}
	// Поле Transmitters должно быть пустым массивом, а не null — для консистентности UI.
	if resp.Transmitters == nil {
		t.Error("Transmitters = nil, want empty slice")
	}
}
