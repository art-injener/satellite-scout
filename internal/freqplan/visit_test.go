package freqplan

import (
	"reflect"
	"testing"
)

func TestJudge_MarksEnergyWhenSightingNearPlanFreq(t *testing.T) {
	plan := []Entry{{
		NoradID: 27607, Name: "SO-50", UUID: "u", FreqHz: 436_795_000,
		Mode: "FM", AOS: 0, LOS: 120_000, Source: SourceCurated,
	}}
	sights := []Sighting{{
		CenterHz: 436_796_000, NoradID: 27607, Origin: "satellite", Emission: "pulsed", SNRDB: 12,
	}}
	got := Judge("rp1", Window{CenterHz: 436_500_000, SpanHz: 1_600_000}, 10_000, 70_000, plan, sights)
	want := []Visit{{
		RadioPath: "rp1", CenterHz: 436_500_000, SpanHz: 1_600_000,
		WindowStartMs: 10_000, WindowEndMs: 70_000,
		NoradID: 27607, Name: "SO-50", UUID: "u", FreqHz: 436_795_000,
		Energy: true, SNRDB: 12, Origin: "satellite", Emission: "pulsed", Received: true, Source: SourceCurated,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestJudge_NoEnergyWhenSilent(t *testing.T) {
	plan := []Entry{{
		NoradID: 27607, Name: "SO-50", FreqHz: 436_795_000,
		AOS: 0, LOS: 120_000, Source: SourceCurated,
	}}
	got := Judge("rp1", Window{CenterHz: 436_500_000, SpanHz: 1_600_000}, 10_000, 70_000, plan, nil)
	if len(got) != 1 || got[0].Energy || got[0].Received {
		t.Fatalf("got %+v, want silent visit", got)
	}
}

func TestJudge_SkipsPlanOutsideWindowOrPass(t *testing.T) {
	plan := []Entry{
		{NoradID: 1, Name: "VHF", FreqHz: 145_900_000, AOS: 0, LOS: 120_000, Source: SourceCurated},
		{NoradID: 2, Name: "later", FreqHz: 436_795_000, AOS: 200_000, LOS: 300_000, Source: SourceCurated},
	}
	got := Judge("rp1", Window{CenterHz: 436_500_000, SpanHz: 1_600_000}, 10_000, 70_000, plan, nil)
	if got != nil {
		t.Fatalf("got %+v, want none", got)
	}
}

func TestJudge_DopplerShiftedSameNoradHasEnergy(t *testing.T) {
	plan := []Entry{{NoradID: 69916, Name: "KOYO", FreqHz: 435_400_000, AOS: 0, LOS: 120_000, Source: SourceCurated}}
	sights := []Sighting{{CenterHz: 435_389_900, NoradID: 69916, SNRDB: 13, Origin: "satellite", Emission: "pulsed"}}
	got := Judge("rp", Window{CenterHz: 435_400_000, SpanHz: 2_048_000}, 0, 50, plan, sights)
	if len(got) != 1 || !got[0].Energy || got[0].SNRDB != 13 {
		t.Fatalf("got %+v, want energy from Doppler-shifted sighting of the same satellite", got)
	}
}

func TestMatchSighting(t *testing.T) {
	const nominal int64 = 435_400_000
	bound := Sighting{CenterHz: nominal - 10_000, NoradID: 69916, SNRDB: 13}
	near := Sighting{CenterHz: nominal + 2_000, SNRDB: 8}
	cases := []struct {
		name    string
		sights  []Sighting
		norad   int
		wantHz  int64
		wantHit bool
	}{
		{"same norad beats nominal neighbour", []Sighting{bound, near}, 69916, bound.CenterHz, true},
		{"unknown norad falls back to nominal", []Sighting{bound, near}, 0, near.CenterHz, true},
		{"other norad beyond NearHz is ignored", []Sighting{bound}, 12345, 0, false},
		{"same norad beyond doppler limit is ignored",
			[]Sighting{{CenterHz: nominal - DopplerMatchHz - 1, NoradID: 69916}}, 69916, 0, false},
	}
	for _, tc := range cases {
		s, ok := MatchSighting(tc.sights, tc.norad, nominal)
		if ok != tc.wantHit || (ok && s.CenterHz != tc.wantHz) {
			t.Errorf("%s: got %d/%v, want %d/%v", tc.name, s.CenterHz, ok, tc.wantHz, tc.wantHit)
		}
	}
}

func TestJudge_PrefersClosestSighting(t *testing.T) {
	plan := []Entry{{NoradID: 1, FreqHz: 436_000_000, AOS: 0, LOS: 100, Source: SourceCurated}}
	sights := []Sighting{
		{CenterHz: 436_020_000, Origin: "terrestrial", Emission: "continuous"},
		{CenterHz: 436_001_000, Origin: "satellite", Emission: "pulsed"},
	}
	got := Judge("rp", Window{CenterHz: 436_000_000, SpanHz: 1_000_000}, 0, 50, plan, sights)
	if len(got) != 1 || got[0].Origin != "satellite" || got[0].Emission != "pulsed" {
		t.Fatalf("got %+v, want closest satellite sighting", got)
	}
}
