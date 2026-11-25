package demod

import (
	"slices"
	"testing"
)

func TestBuildCandidates_Priority(t *testing.T) {
	got := BuildCandidates(CandidateHints{
		Accepted:    "afsk1200",
		CardProfile: "fsk9600_geoscan",
		Mode:        "FSK",
		Baud:        9600,
		Blind:       "bpsk9600",
		BlindAlt:    []string{"bpsk1200"},
		Max:         6,
	})
	// принято → карточка → заявлено → обнаружено → запас кадровых
	want := []string{"afsk1200", "fsk9600_geoscan", "fsk9600_g3ruh", "fsk9600_ax100_rs", "bpsk9600", "bpsk1200"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildCandidates_LocalModeOnly(t *testing.T) {
	got := BuildCandidates(CandidateHints{Mode: "AFSK", Baud: 1200, Max: 3})
	want := []string{"afsk1200", "fsk9600_g3ruh", "fsk9600_geoscan"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildCandidates_VoiceHintsExcludedInAuto(t *testing.T) {
	got := BuildCandidates(CandidateHints{
		CardProfile: "fm",
		Mode:        "FM",
		Baud:        0,
		Blind:       "am",
		BlindAlt:    []string{"usb", "lsb", "wfm", "ook"},
		AirMode:     "fm",
		Max:         5,
	})
	for _, id := range []string{"fm", "am", "usb", "lsb", "wfm", "ook"} {
		if slices.Contains(got, id) {
			t.Fatalf("got %v: %s must not be an Auto candidate", got, id)
		}
	}
	want := ImplementedFrameProfiles()[:5]
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildCandidates_VoiceModesExcluded(t *testing.T) {
	for _, mode := range []string{"FM", "AM", "SSB", "USB", "LSB", "WFM"} {
		got := BuildCandidates(CandidateHints{Mode: mode})
		for _, id := range []string{"fm", "am", "usb", "lsb", "wfm"} {
			if slices.Contains(got, id) {
				t.Errorf("mode %s: %s must not be an Auto candidate, got %v", mode, id, got)
			}
		}
	}
}

func TestBuildCandidates_EmptyFallsBackToFrameProfiles(t *testing.T) {
	got := BuildCandidates(CandidateHints{Max: 2})
	want := ImplementedFrameProfiles()
	if len(want) > 2 {
		want = want[:2]
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMatchTXModeBaud(t *testing.T) {
	txs := []ExpectedTransmitter{
		{NoradID: 1, FreqHz: 145_800_000, Mode: "FM"},
		{NoradID: 1, FreqHz: 145_825_000, Mode: "AFSK", Baud: 1200},
		{NoradID: 2, FreqHz: 436_200_000, Mode: "FSK", Baud: 9600},
	}
	mode, baud, ok := MatchTXModeBaud(txs, 1, 145_825_000)
	if !ok || mode != "AFSK" || baud != 1200 {
		t.Fatalf("got %s %.0f ok=%v", mode, baud, ok)
	}
	mode, baud, ok = MatchTXModeBaud(txs, 2, 436_205_000)
	if !ok || mode != "FSK" || baud != 9600 {
		t.Fatalf("near: %s %.0f ok=%v", mode, baud, ok)
	}
	if _, _, ok := MatchTXModeBaud(txs, 9, 145_800_000); ok {
		t.Fatal("unknown norad")
	}
}

func TestMatchTXModeBauds_CollectsAllModesOnSameFrequency(t *testing.T) {
	txs := []ExpectedTransmitter{
		{NoradID: 57172, FreqHz: 437_625_000, Mode: "SSTV"},
		{NoradID: 57172, FreqHz: 437_625_000, Mode: "GMSK USP", Baud: 2400},
		{NoradID: 57172, FreqHz: 437_625_000, Mode: "GMSK USP", Baud: 4800},
		{NoradID: 57172, FreqHz: 437_625_000, Mode: "GMSK USP", Baud: 9600},
		{NoradID: 57172, FreqHz: 437_625_000, Mode: "CW"},
		{NoradID: 57172, FreqHz: 435_825_000, Mode: "FSK", Baud: 4800},
	}
	got := MatchTXModeBauds(txs, 57172, 437_626_000)
	if len(got) != 5 {
		t.Fatalf("got %d hints %+v, want 5", len(got), got)
	}
	cands := BuildCandidates(CandidateHints{TXHints: got})
	for _, want := range []string{"gmsk2400_usp", "gmsk4800_usp", "gmsk9600_usp"} {
		if !slices.Contains(cands, want) {
			t.Errorf("missing %q in %v", want, cands)
		}
	}
}

func TestBuildCandidates_DefaultIncludesAllFrameProfiles(t *testing.T) {
	got := BuildCandidates(CandidateHints{})
	want := ImplementedFrameProfiles()
	if len(got) != len(want) {
		t.Fatalf("got %d candidates, want all %d frame profiles", len(got), len(want))
	}
	for _, id := range []string{"gmsk9600_usp", "gmsk2400_usp", "afsk1200", "bpsk1200", "qpsk_ccsds"} {
		if !slices.Contains(got, id) {
			t.Errorf("missing %q in default candidates", id)
		}
	}
}

func TestBuildCandidates_IncludesSSTVAndCWOnlyWithHints(t *testing.T) {
	noHints := BuildCandidates(CandidateHints{})
	if slices.Contains(noHints, DemodSSTV) || slices.Contains(noHints, "cw") {
		t.Fatalf("default candidates without hints must not include sstv/cw: %v", noHints)
	}

	withTX := BuildCandidates(CandidateHints{
		TXHints: []TXHint{
			{Mode: "SSTV"},
			{Mode: "GMSK USP", Baud: 9600},
			{Mode: "CW"},
		},
	})
	for _, want := range []string{"gmsk9600_usp", DemodSSTV, "cw"} {
		if !slices.Contains(withTX, want) {
			t.Errorf("missing %q in hinted candidates %v", want, withTX)
		}
	}

	withAirCW := BuildCandidates(CandidateHints{AirMode: "cw"})
	if !slices.Contains(withAirCW, "cw") {
		t.Errorf("expected cw when AirMode=cw, got %v", withAirCW)
	}

	withBlindSSTV := BuildCandidates(CandidateHints{Blind: DemodSSTV})
	if !slices.Contains(withBlindSSTV, DemodSSTV) {
		t.Errorf("expected sstv when Blind=sstv, got %v", withBlindSSTV)
	}
}

func TestBuildCandidatePlan_KnownSatStartsNarrowAndExpandsSameFamily(t *testing.T) {
	// Known satellite with GMSK USP 9600 starts only with its declared GMSK 9600 profiles.
	plan := BuildCandidatePlan(CandidateHints{
		KnownSat: true,
		TXHints:  []TXHint{{Mode: "GMSK USP", Baud: 9600}},
	})
	if len(plan.Initial) == 0 || plan.Initial[0] != "gmsk9600_usp" {
		t.Fatalf("Initial = %v, want gmsk9600_usp first", plan.Initial)
	}
	for _, forbidden := range []string{"bpsk1200", "bpsk9600", "afsk1200", "qpsk_ccsds", "rs41", "m10", "fsk1200"} {
		if slices.Contains(plan.Initial, forbidden) {
			t.Errorf("known sat Initial must not contain %q, got %v", forbidden, plan.Initial)
		}
	}
	// Expansion adds the FSK/GFSK/GMSK family, still excluding BPSK/AFSK/QPSK/RS41/M10.
	if len(plan.Expanded) <= len(plan.Initial) {
		t.Fatalf("Expanded = %v, want wider than Initial %v", plan.Expanded, plan.Initial)
	}
	for _, want := range []string{"gmsk9600_usp", "fsk9600_g3ruh", "fsk4800", "gmsk4800_usp"} {
		if !slices.Contains(plan.Expanded, want) {
			t.Errorf("Expanded missing FSK-family profile %q in %v", want, plan.Expanded)
		}
	}
	for _, forbidden := range []string{"bpsk1200", "bpsk9600", "afsk1200", "qpsk_ccsds", "rs41", "m10"} {
		if slices.Contains(plan.Expanded, forbidden) {
			t.Errorf("Expanded must stay within FSK family, found %q in %v", forbidden, plan.Expanded)
		}
	}

	// BPSK satellite expands only to BPSK profiles.
	bpskPlan := BuildCandidatePlan(CandidateHints{
		KnownSat: true,
		TXHints:  []TXHint{{Mode: "BPSK", Baud: 1200}},
	})
	if !slices.Equal(bpskPlan.Initial, []string{"bpsk1200"}) {
		t.Fatalf("BPSK Initial = %v, want [bpsk1200]", bpskPlan.Initial)
	}
	if !slices.Equal(bpskPlan.Expanded, []string{"bpsk1200", "bpsk9600"}) {
		t.Fatalf("BPSK Expanded = %v, want [bpsk1200 bpsk9600]", bpskPlan.Expanded)
	}

	// Unknown air (KnownSat=false) runs full scan from the start.
	airPlan := BuildCandidatePlan(CandidateHints{KnownSat: false, AirMode: "2fsk", AirBaud: 9600})
	allFrames := len(ImplementedFrameProfiles())
	if len(airPlan.Initial) != allFrames {
		t.Fatalf("unknown air Initial len=%d, want all %d frame profiles", len(airPlan.Initial), allFrames)
	}
	if len(airPlan.Expanded) != 0 {
		t.Fatalf("unknown air Expanded=%v, want empty", airPlan.Expanded)
	}

	// Known satellite with no metadata falls back to full scan.
	emptySat := BuildCandidatePlan(CandidateHints{KnownSat: true})
	if len(emptySat.Initial) != allFrames {
		t.Fatalf("known sat without hints Initial len=%d, want all %d frame profiles", len(emptySat.Initial), allFrames)
	}
}
