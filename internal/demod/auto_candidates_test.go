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
		Blind:       "fsk9600_g3ruh",
		BlindAlt:    []string{"fsk9600_geoscan"},
		Max:         5,
	})
	// принято → обнаружено → карточка (уже в BlindAlt) → заявлено → запас кадровых
	want := []string{"afsk1200", "fsk9600_g3ruh", "fsk9600_geoscan", "fsk9600_ax100_rs", "fsk4800"}
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

func TestBuildCandidates_SkipsUnimplementedAndVoice(t *testing.T) {
	got := BuildCandidates(CandidateHints{
		CardProfile: "fm",
		Mode:        "FM",
		Baud:        0,
		Blind:       "ook", // линия без newDeframer — в кадровые кандидаты не попадает
		Max:         5,
	})
	want := ImplementedFrameProfiles()
	if len(want) > 5 {
		want = want[:5]
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
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
