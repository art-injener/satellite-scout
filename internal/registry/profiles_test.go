package registry

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/rfprofile"
)

func TestStore_SchemaV3CreatesRFProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.db")
	r := openTestRegistry(t, path)
	_ = r.Close()

	db, err := sql.Open(sqliteDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'rf_profiles'`).Scan(&name)
	if err != nil || name != "rf_profiles" {
		t.Fatalf("rf_profiles missing: name=%q err=%v", name, err)
	}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("user_version = %d, want 3", version)
	}
}

func TestStore_MigratesV2ToV3AddsRFProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.db")
	db, err := sql.Open(sqliteDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schemaV2); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	r := openTestRegistry(t, path)
	_ = r.Close()

	db, err = sql.Open(sqliteDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("user_version = %d, want 3", version)
	}
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'rf_profiles'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("rf_profiles must exist after v2→v3 migrate")
	}
}

func TestRegistry_PutRFProfile_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.db")
	ctx := context.Background()
	r := openTestRegistry(t, path)

	want := rfprofile.Profile{
		NoradID:   57172,
		BestKey:   "tx-uuid-1",
		UpdatedMs: 1_700_000_000_000,
		Tx: []rfprofile.Transmitter{{
			Key:    "tx-uuid-1",
			UUID:   "tx-uuid-1",
			FreqHz: 401_025_000,
			Detected: rfprofile.Detected{
				Modulation:    "2FSK",
				Baud:          2400,
				BandwidthHz:   12_000,
				BeaconPeriodS: 30,
				OffsetHz:      -250,
				Confidence:    0.8,
				Confirmations: 3,
				LastSeenMs:    1_700_000_000_000,
				Demod:         "fsk2400",
			},
			Accepted: rfprofile.Accepted{
				DemodID:     "fsk2400",
				FramesTotal: 2,
				LastFrameMs: 1_700_000_100_000,
			},
			Stats: rfprofile.Stats{
				VisitsTotal:      5,
				VisitsWithEnergy: 4,
				VisitsWithFrames: 1,
				BestSNRDB:        14.5,
				LastHeardMs:      1_700_000_000_000,
			},
		}},
	}
	if err := r.PutRFProfile(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, ok := r.RFProfile(57172)
	if !ok {
		t.Fatal("RFProfile missing in memory")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("memory got %+v, want %+v", got, want)
	}
	_ = r.Close()

	again := openTestRegistry(t, path)
	got, ok = again.RFProfile(57172)
	if !ok {
		t.Fatal("RFProfile must survive reopen")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after reopen got %+v, want %+v", got, want)
	}
}

func TestRegistry_PutRFProfile_MemoryOnly(t *testing.T) {
	r := New()
	p := rfprofile.Profile{NoradID: 1, UpdatedMs: 10, Tx: []rfprofile.Transmitter{{
		Key: rfprofile.Key("", 145_800_000), FreqHz: 145_800_000,
	}}}
	if err := r.PutRFProfile(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	got, ok := r.RFProfile(1)
	if !ok || !reflect.DeepEqual(got, p) {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestRegistry_RFProfile_Missing(t *testing.T) {
	r := New()
	if _, ok := r.RFProfile(42); ok {
		t.Fatal("empty registry must not report profile")
	}
}

func TestRegistry_PutRFProfile_RejectsZeroNorad(t *testing.T) {
	r := New()
	err := r.PutRFProfile(context.Background(), rfprofile.Profile{NoradID: 0})
	if err == nil {
		t.Fatal("want error for norad 0")
	}
}

func TestRegistry_ApplyRFDwellAndFrame_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.db")
	ctx := context.Background()
	r := openTestRegistry(t, path)

	visits := []freqplan.Visit{{
		NoradID: 57172, UUID: "u1", FreqHz: 401_025_000,
		Energy: true, SNRDB: 11, WindowEndMs: 1000,
	}}
	sights := []freqplan.Sighting{{
		CenterHz: 401_025_000, Modulation: "2FSK", Baud: 2400, Demod: "fsk2400",
	}}
	if err := r.ApplyRFDwell(ctx, visits, sights); err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyRFFrame(ctx, 57172, 401_025_000, "fsk2400", 2000); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()

	again := openTestRegistry(t, path)
	p, ok := again.RFProfile(57172)
	if !ok {
		t.Fatal("profile missing after reopen")
	}
	if len(p.Tx) != 1 {
		t.Fatalf("tx=%d", len(p.Tx))
	}
	tx := p.Tx[0]
	if tx.Detected.Confirmations != 1 || tx.Detected.Modulation != "2FSK" {
		t.Fatalf("detected %+v", tx.Detected)
	}
	if tx.Accepted.DemodID != "fsk2400" || tx.Accepted.FramesTotal != 1 {
		t.Fatalf("accepted %+v", tx.Accepted)
	}
}
