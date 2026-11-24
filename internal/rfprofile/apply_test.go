package rfprofile

import (
	"testing"

	"github.com/art-injener/satellite-scout/internal/freqplan"
)

func TestApplyDwell_IncrementsStatsAndDetected(t *testing.T) {
	p := Profile{NoradID: 57172}
	visits := []freqplan.Visit{{
		NoradID: 57172, UUID: "u1", FreqHz: 401_025_000,
		Energy: true, SNRDB: 12, WindowEndMs: 1000,
	}}
	sights := []freqplan.Sighting{{
		CenterHz: 401_025_100, Modulation: "2FSK", Baud: 2400,
		BandwidthHz: 12_000, BeaconPeriodS: 30, Confidence: 0.7,
		Demod: "fsk2400", SNRDB: 12, LastSeenMs: 1000,
	}}
	got := ApplyDwell(p, visits, sights)
	if len(got.Tx) != 1 {
		t.Fatalf("tx=%d", len(got.Tx))
	}
	tx := got.Tx[0]
	if tx.Key != "u1" || tx.Stats.VisitsTotal != 1 || tx.Stats.VisitsWithEnergy != 1 {
		t.Fatalf("stats %+v key=%q", tx.Stats, tx.Key)
	}
	if tx.Detected.Modulation != "2FSK" || tx.Detected.Baud != 2400 || tx.Detected.Confirmations != 1 {
		t.Fatalf("detected %+v", tx.Detected)
	}
	if got.BestKey != "u1" || got.UpdatedMs != 1000 {
		t.Fatalf("best=%q updated=%d", got.BestKey, got.UpdatedMs)
	}
}

func TestApplyDwell_DopplerShiftedSightingFillsDetected(t *testing.T) {
	p := Profile{NoradID: 69916}
	visits := []freqplan.Visit{{
		NoradID: 69916, FreqHz: 435_400_000, Energy: true, SNRDB: 13, WindowEndMs: 1000,
	}}
	sights := []freqplan.Sighting{{
		CenterHz: 435_389_900, NoradID: 69916, Modulation: "2FSK", Baud: 2400,
	}}
	got := ApplyDwell(p, visits, sights)
	d := got.Tx[0].Detected
	if d.Modulation != "2FSK" || d.Baud != 2400 || d.Confirmations != 1 {
		t.Fatalf("detected %+v, want 2FSK 2400 from Doppler-shifted sighting", d)
	}
}

func TestApplyDwell_SameModeIncrementsConfirmations(t *testing.T) {
	p := Profile{NoradID: 1, Tx: []Transmitter{{
		Key: "u1", UUID: "u1", FreqHz: 401_000_000,
		Detected: Detected{Modulation: "2FSK", Baud: 2400, Confirmations: 2},
	}}}
	visits := []freqplan.Visit{{
		NoradID: 1, UUID: "u1", FreqHz: 401_000_000, Energy: true, WindowEndMs: 2000,
	}}
	sights := []freqplan.Sighting{{
		CenterHz: 401_000_000, Modulation: "2FSK", Baud: 2400,
	}}
	got := ApplyDwell(p, visits, sights)
	if got.Tx[0].Detected.Confirmations != 3 {
		t.Fatalf("confirmations=%d, want 3", got.Tx[0].Detected.Confirmations)
	}
}

func TestApplyDwell_ModeChangeResetsConfirmations(t *testing.T) {
	p := Profile{NoradID: 1, Tx: []Transmitter{{
		Key: "u1", UUID: "u1", FreqHz: 401_000_000,
		Detected: Detected{Modulation: "2FSK", Baud: 2400, Confirmations: 5},
	}}}
	visits := []freqplan.Visit{{
		NoradID: 1, UUID: "u1", FreqHz: 401_000_000, Energy: true, WindowEndMs: 2000,
	}}
	sights := []freqplan.Sighting{{
		CenterHz: 401_000_000, Modulation: "GMSK", Baud: 9600,
	}}
	got := ApplyDwell(p, visits, sights)
	d := got.Tx[0].Detected
	if d.Modulation != "GMSK" || d.Baud != 9600 || d.Confirmations != 1 {
		t.Fatalf("detected %+v", d)
	}
}

func TestApplyDwell_SilentVisitStillCounts(t *testing.T) {
	p := Profile{NoradID: 1}
	visits := []freqplan.Visit{{
		NoradID: 1, FreqHz: 145_800_000, Energy: false, WindowEndMs: 500,
	}}
	got := ApplyDwell(p, visits, nil)
	tx := got.Tx[0]
	if tx.Key != "145800000" || tx.Stats.VisitsTotal != 1 || tx.Stats.VisitsWithEnergy != 0 {
		t.Fatalf("%+v", tx)
	}
	if tx.Detected.Confirmations != 0 {
		t.Fatalf("silent visit must not touch detected")
	}
}

func TestApplyDwell_BestSNRAndLastHeard(t *testing.T) {
	p := Profile{NoradID: 1, Tx: []Transmitter{{
		Key: "u1", UUID: "u1", FreqHz: 400_000_000,
		Stats: Stats{BestSNRDB: 10, LastHeardMs: 100},
	}}}
	visits := []freqplan.Visit{{
		NoradID: 1, UUID: "u1", FreqHz: 400_000_000,
		Energy: true, SNRDB: 14.5, WindowEndMs: 900,
	}}
	got := ApplyDwell(p, visits, nil)
	if got.Tx[0].Stats.BestSNRDB != 14.5 || got.Tx[0].Stats.LastHeardMs != 900 {
		t.Fatalf("%+v", got.Tx[0].Stats)
	}
}

func TestApplyDwell_ClearsAcceptedAfterSilence(t *testing.T) {
	p := Profile{NoradID: 1, Tx: []Transmitter{{
		Key: "u1", UUID: "u1", FreqHz: 400_000_000,
		Accepted: Accepted{DemodID: "fsk2400", FramesTotal: 3, LastFrameMs: 1000},
	}}}
	visits := []freqplan.Visit{{
		NoradID: 1, UUID: "u1", FreqHz: 400_000_000,
		Energy: false, WindowEndMs: 1000 + AcceptedSilenceMs + 1,
	}}
	got := ApplyDwell(p, visits, nil)
	if got.Tx[0].Accepted.DemodID != "" || got.Tx[0].Accepted.FramesTotal != 0 {
		t.Fatalf("accepted must clear after silence: %+v", got.Tx[0].Accepted)
	}
}

func TestApplyDwell_KeepsAcceptedWithinSilenceWindow(t *testing.T) {
	p := Profile{NoradID: 1, Tx: []Transmitter{{
		Key: "u1", UUID: "u1", FreqHz: 400_000_000,
		Accepted: Accepted{DemodID: "fsk2400", FramesTotal: 3, LastFrameMs: 1000},
	}}}
	visits := []freqplan.Visit{{
		NoradID: 1, UUID: "u1", FreqHz: 400_000_000,
		Energy: false, WindowEndMs: 1000 + AcceptedSilenceMs/2,
	}}
	got := ApplyDwell(p, visits, nil)
	if got.Tx[0].Accepted.DemodID != "fsk2400" || got.Tx[0].Accepted.FramesTotal != 3 {
		t.Fatalf("accepted %+v", got.Tx[0].Accepted)
	}
}

func TestApplyDwell_IgnoresOtherNorad(t *testing.T) {
	p := Profile{NoradID: 1}
	got := ApplyDwell(p, []freqplan.Visit{{
		NoradID: 2, FreqHz: 145_800_000, Energy: true, WindowEndMs: 1,
	}}, nil)
	if len(got.Tx) != 0 {
		t.Fatalf("other norad must not mutate profile: %+v", got.Tx)
	}
}

func TestApplyFrame_SetsAccepted(t *testing.T) {
	p := Profile{NoradID: 53385}
	got := ApplyFrame(p, "uuid-g", 436_200_000, "fsk9600_geoscan", 5000)
	if len(got.Tx) != 1 {
		t.Fatalf("tx=%d", len(got.Tx))
	}
	tx := got.Tx[0]
	if tx.Key != "uuid-g" || tx.Accepted.DemodID != "fsk9600_geoscan" {
		t.Fatalf("%+v", tx)
	}
	if tx.Accepted.FramesTotal != 1 || tx.Accepted.LastFrameMs != 5000 {
		t.Fatalf("accepted %+v", tx.Accepted)
	}
	if tx.Stats.VisitsWithFrames != 1 || got.UpdatedMs != 5000 {
		t.Fatalf("stats %+v updated=%d", tx.Stats, got.UpdatedMs)
	}
}

func TestApplyFrame_IncrementsExisting(t *testing.T) {
	p := Profile{NoradID: 1, Tx: []Transmitter{{
		Key: Key("", 145_800_000), FreqHz: 145_800_000,
		Accepted: Accepted{DemodID: "afsk1200", FramesTotal: 2, LastFrameMs: 10},
		Stats:    Stats{VisitsWithFrames: 1},
	}}}
	got := ApplyFrame(p, "", 145_802_000, "afsk1200", 20)
	tx := got.Tx[0]
	if tx.Accepted.FramesTotal != 3 || tx.Stats.VisitsWithFrames != 2 {
		t.Fatalf("%+v / %+v", tx.Accepted, tx.Stats)
	}
}

func TestApplyFrame_RejectsEmpty(t *testing.T) {
	p := Profile{NoradID: 1}
	if got := ApplyFrame(p, "", 0, "x", 1); len(got.Tx) != 0 {
		t.Fatal("freq 0 must no-op")
	}
	if got := ApplyFrame(p, "", 1000, "", 1); len(got.Tx) != 0 {
		t.Fatal("empty demod must no-op")
	}
}
