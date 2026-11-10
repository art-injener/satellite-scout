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
		Energy: true, Origin: "satellite", Emission: "pulsed", Received: true, Source: SourceCurated,
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
