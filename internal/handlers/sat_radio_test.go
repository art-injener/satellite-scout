package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/registry"
	"github.com/art-injener/satellite-scout/internal/rfprofile"
)

func TestSatRadioHandler_GetEmpty(t *testing.T) {
	h := NewSatRadioHandler(registry.New())
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/sat-radio", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp SatRadioResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Satellites == nil || len(resp.Satellites) != 0 {
		t.Fatalf("%+v", resp)
	}
}

func TestSatRadioHandler_GetAndFilter(t *testing.T) {
	reg := registry.New()
	ctx := context.Background()
	_ = reg.PutRFProfile(ctx, rfprofile.Profile{
		NoradID: 57172, BestKey: "u1", UpdatedMs: 9,
		Tx: []rfprofile.Transmitter{{
			Key: "u1", UUID: "u1", FreqHz: 401_025_000,
			Detected: rfprofile.Detected{
				Modulation: "2FSK", Baud: 2400, Demod: "fsk2400", Confirmations: 3,
			},
			Accepted: rfprofile.Accepted{DemodID: "fsk2400", FramesTotal: 2},
			Stats:    rfprofile.Stats{VisitsWithEnergy: 4, LastHeardMs: 9},
		}},
	})
	h := NewSatRadioHandler(reg)

	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/sat-radio", nil))
	var all SatRadioResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &all)
	if len(all.Satellites) != 1 || !all.Satellites[0].Heard || all.Satellites[0].FramesTotal != 2 {
		t.Fatalf("%+v", all)
	}
	if all.Satellites[0].BestDemod != "fsk2400" || all.Satellites[0].Tx[0].EffectiveSource != rfprofile.SourceAccepted {
		t.Fatalf("tx %+v", all.Satellites[0])
	}

	rec = httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/sat-radio?norad=1", nil))
	var miss SatRadioResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &miss)
	if len(miss.Satellites) != 0 {
		t.Fatalf("miss %+v", miss)
	}

	rec = httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/sat-radio?norad=57172", nil))
	var one SatRadioResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &one)
	if len(one.Satellites) != 1 || one.Satellites[0].NoradID != 57172 {
		t.Fatalf("%+v", one)
	}
}

func TestBuildSatRadioSat_DeclaredDiffers(t *testing.T) {
	reg := registry.New()
	ctx := context.Background()
	_ = reg.ReplaceTransmitters(ctx, registry.SourceCurated, []registry.Transmitter{{
		NoradID: 1, UUID: "u1", DownlinkHz: 401_000_000, Mode: "AFSK", Baud: 1200,
	}}, time.Now())
	p := rfprofile.Profile{NoradID: 1, Tx: []rfprofile.Transmitter{{
		Key: "u1", UUID: "u1", FreqHz: 401_000_000,
		Detected: rfprofile.Detected{Modulation: "2FSK", Baud: 2400, Demod: "fsk2400", Confirmations: 3},
	}}}
	sat := BuildSatRadioSat(reg, p)
	if len(sat.Tx) != 1 || !sat.Tx[0].DeclaredDiffers || sat.Tx[0].DeclaredMode != "AFSK" {
		t.Fatalf("%+v", sat.Tx)
	}
}
