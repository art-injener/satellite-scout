package main

import (
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/registry"
)

func TestPlanSweepFreqs_FiltersByReceiverAndAntenna(t *testing.T) {
	a := &app{
		bands:        registry.Bands{{MinHz: 24_000_000, MaxHz: 1_700_000_000}},
		antennaBands: registry.Bands{{MinHz: 144_000_000, MaxHz: 440_000_000}},
		freqPlan: []freqplan.Entry{
			{FreqHz: 401_000_000, AOS: 1, LOS: 100},
			{FreqHz: 145_900_000, AOS: 1, LOS: 100},
			{FreqHz: 2_400_000_000, AOS: 1, LOS: 100}, // вне приёмника
			{FreqHz: 50_000_000, AOS: 1, LOS: 100},     // вне антенны
		},
	}
	got := a.planSweepFreqs("radio_path_1", time.UnixMilli(10), time.UnixMilli(50))
	want := []uint64{145_900_000, 401_000_000}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}
