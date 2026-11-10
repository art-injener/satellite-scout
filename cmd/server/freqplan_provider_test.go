package main

import (
	"testing"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/services"
)

func TestMergePlanTX_AddsMissingFrequencies(t *testing.T) {
	base := []services.TransmitterInfo{
		{UUID: "s1", FreqHz: 436_795_000, Mode: "FM", Modulation: "FM"},
	}
	plan := []freqplan.Entry{
		{NoradID: 27607, Name: "SO-50", FreqHz: 436_795_000, Mode: "FM", Source: freqplan.SourceSatNOGS},
		{NoradID: 27607, Name: "SO-50", FreqHz: 145_850_000, Mode: "FM", Source: freqplan.SourceCurated},
		{NoradID: 1, Name: "other", FreqHz: 437_000_000, Mode: "FM", Source: freqplan.SourceAMSAT},
	}
	got := mergePlanTX(27607, base, plan)
	if len(got) != 2 {
		t.Fatalf("got %+v, want satnogs + curated VHF", got)
	}
	if got[0].FreqHz != 145_850_000 || got[0].UUID == "" {
		t.Errorf("vhf = %+v", got[0])
	}
	if got[1].UUID != "s1" || got[1].FreqHz != 436_795_000 {
		t.Errorf("uhf = %+v", got[1])
	}
	if got[0].Count != 2 || got[1].Count != 2 {
		t.Errorf("count not updated: %+v", got)
	}
}

func TestMergePlanTX_OnlyPlan(t *testing.T) {
	plan := []freqplan.Entry{
		{NoradID: 25544, Name: "ISS", FreqHz: 437_800_000, Mode: "FM", Source: freqplan.SourceCurated},
	}
	got := mergePlanTX(25544, nil, plan)
	if len(got) != 1 || got[0].FreqHz != 437_800_000 || got[0].Description != "ISS" {
		t.Fatalf("got %+v", got)
	}
}

func TestMergePlanTX_NearFreqKeepsBase(t *testing.T) {
	base := []services.TransmitterInfo{{UUID: "s", FreqHz: 436_795_000, Mode: "FM"}}
	plan := []freqplan.Entry{
		{NoradID: 1, FreqHz: 436_796_000, Mode: "FM", Source: freqplan.SourceAMSAT},
	}
	got := mergePlanTX(1, base, plan)
	if len(got) != 1 || got[0].UUID != "s" {
		t.Fatalf("got %+v, want base only", got)
	}
}

func TestPlanTXUUID(t *testing.T) {
	if got := planTXUUID(freqplan.Entry{NoradID: 1, FreqHz: 100, UUID: "keep"}); got != "keep" {
		t.Errorf("got %q", got)
	}
	if got := planTXUUID(freqplan.Entry{NoradID: 27607, FreqHz: 436_795_000}); got != "plan:27607:436795000" {
		t.Errorf("got %q", got)
	}
}
