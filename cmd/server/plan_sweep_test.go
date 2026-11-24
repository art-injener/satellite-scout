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

func TestPlanSweepTargets_KeepsNames(t *testing.T) {
	a := &app{
		bands:        registry.Bands{{MinHz: 24_000_000, MaxHz: 1_700_000_000}},
		antennaBands: registry.Bands{{MinHz: 144_000_000, MaxHz: 440_000_000}},
		freqPlan: []freqplan.Entry{
			{NoradID: 25544, Name: "ISS", FreqHz: 437_800_000, AOS: 1, LOS: 100},
			{NoradID: 27607, Name: "SO-50", FreqHz: 436_795_000, AOS: 1, LOS: 100},
		},
	}
	got := a.planSweepTargets("radio_path_1", time.UnixMilli(10), time.UnixMilli(50))
	if len(got) != 2 || got[0].FreqHz != 436_795_000 || got[0].Label != "SO-50" {
		t.Fatalf("got %+v", got)
	}
	if got[1].FreqHz != 437_800_000 || got[1].Label != "ISS" {
		t.Fatalf("got %+v", got)
	}
}

func TestFilterTargetsByNorad_UsedForGroupSync(t *testing.T) {
	// planSweepTargets режет ActiveTargets через FilterTargetsByNorad по GroupNoradIDs.
	all := []freqplan.Target{
		{FreqHz: 435_100_000, Name: "AO-7", NoradID: 7530},
		{FreqHz: 437_800_000, Name: "ISS", NoradID: 25544},
	}
	got := freqplan.FilterTargetsByNorad(all, map[int]struct{}{25544: {}})
	if len(got) != 1 || got[0].NoradID != 25544 {
		t.Fatalf("got %+v, want only ISS from group", got)
	}
}
