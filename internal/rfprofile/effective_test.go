package rfprofile

import "testing"

func TestEffective_AcceptedBeatsDetected(t *testing.T) {
	tx := Transmitter{
		Accepted: Accepted{DemodID: "fsk2400"},
		Detected: Detected{Modulation: "2FSK", Baud: 2400, Demod: "fsk9600_g3ruh", Confirmations: 5},
	}
	got := Effective(tx)
	if got.Source != SourceAccepted || got.Demod != "fsk2400" {
		t.Fatalf("%+v", got)
	}
}

func TestEffective_DetectedNeedsConfirmations(t *testing.T) {
	tx := Transmitter{Detected: Detected{Modulation: "2FSK", Baud: 2400, Demod: "fsk2400", Confirmations: 2}}
	if got := Effective(tx); got.Source != "" {
		t.Fatalf("want empty, got %+v", got)
	}
	tx.Detected.Confirmations = 3
	got := Effective(tx)
	if got.Source != SourceDetected || got.Demod != "fsk2400" || got.Baud != 2400 {
		t.Fatalf("%+v", got)
	}
}

func TestFindTx_ByUUIDAndFreq(t *testing.T) {
	p := Profile{Tx: []Transmitter{
		{Key: "u1", UUID: "u1", FreqHz: 401_000_000},
		{Key: "145800000", FreqHz: 145_800_000},
	}}
	if tx, ok := FindTx(p, "u1", 0); !ok || tx.UUID != "u1" {
		t.Fatalf("uuid: %+v ok=%v", tx, ok)
	}
	if tx, ok := FindTx(p, "", 145_802_000); !ok || tx.FreqHz != 145_800_000 {
		t.Fatalf("freq: %+v ok=%v", tx, ok)
	}
}

func TestLastHeardAt(t *testing.T) {
	p := Profile{Tx: []Transmitter{{
		Key: "401000000", FreqHz: 401_000_000, Stats: Stats{LastHeardMs: 99},
	}}}
	if got := LastHeardAt(p, 401_001_000); got != 99 {
		t.Fatalf("got %d", got)
	}
	if got := LastHeardAt(p, 145_800_000); got != 0 {
		t.Fatalf("miss got %d", got)
	}
}
