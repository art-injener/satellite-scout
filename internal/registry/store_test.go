package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

func openTestRegistry(t *testing.T, path string) *Registry {
	t.Helper()
	r, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestStore_CreatesOnlyRegistryTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "scout.db")
	r := openTestRegistry(t, path)
	_ = r.Close()

	db, err := sql.Open(sqliteDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var n string
		if scanErr := rows.Scan(&n); scanErr != nil {
			t.Fatal(scanErr)
		}
		names = append(names, n)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"orbits", "rf_profiles", "rx_visits", "satellites", "source_loads", "transmitters"}
	if !slices.Equal(names, want) {
		t.Fatalf("tables = %v, want %v", names, want)
	}
}

func TestStore_ReopenRestoresEverything(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.db")
	ctx := context.Background()
	decay := time.Date(2024, 5, 12, 0, 0, 0, 0, time.UTC)
	iss := testTLE(t, "ISS (ZARYA)", "26316.50000000")

	r := openTestRegistry(t, path)
	if err := r.IngestCatalogPass(ctx, tracker.CatalogPass{
		At:   testAt,
		TLEs: []*tracker.TLE{iss},
		Sonik: []tracker.SonikSat{
			{NoradID: 25544, Name: "ISS", Status: StatusInOrbit, Operator: "NASA", Launched: "1998-11-20"},
		},
		Satcat: []tracker.SatcatRecord{{NoradID: 25544}, {NoradID: 2012, Decay: decay}},
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if err := r.ReplaceTransmitters(ctx, SourceSonik, []Transmitter{
		{
			NoradID:    25544,
			UUID:       "u1",
			Name:       "APRS",
			DownlinkHz: 145_825_000,
			Mode:       "AFSK",
			Baud:       1200,
			Status:     "active",
			FetchedAt:  testAt,
		},
	}, testAt); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceTransmittersByNorad(ctx, SourceSatNOGS, map[int][]Transmitter{
		25544: {
			{
				NoradID:    25544,
				UUID:       "u9",
				DownlinkHz: 437_800_000,
				UplinkHz:   145_990_000,
				Status:     "active",
				Alive:      new(true),
				FetchedAt:  testAt,
			},
		},
	}, testAt); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkLoad(ctx, SourceSonikTx, testAt, errors.New("sonik: timeout")); err != nil {
		t.Fatal(err)
	}
	wantSat, _ := r.Satellite(25544)
	wantTx := r.Transmitters(25544)
	_ = r.Close()

	again := openTestRegistry(t, path)
	gotSat, ok := again.Satellite(25544)
	if !ok || fmt.Sprint(gotSat) != fmt.Sprint(wantSat) {
		t.Fatalf("satellite after reopen = %+v, want %+v", gotSat, wantSat)
	}
	if got := again.Transmitters(25544); fmt.Sprint(got) != fmt.Sprint(wantTx) {
		t.Fatalf("transmitters after reopen = %+v, want %+v", got, wantTx)
	}
	o, ok := again.Orbit(25544)
	if !ok || o.Line1 != iss.Line1 || o.Line2 != iss.Line2 || !o.Epoch.Equal(iss.Epoch) ||
		o.Source != SourceSpaceTrack {
		t.Fatalf("orbit after reopen = %+v", o)
	}
	if !again.Decayed(2012) {
		t.Fatal("decay must survive reopen")
	}
	if l := again.Loads()[SourceSonikTx]; !l.LastFail.Equal(testAt) || l.Err != "sonik: timeout" {
		t.Fatalf("load after reopen = %+v", l)
	}
	if l := again.Loads()[SourceSpaceTrack]; !l.LastOK.Equal(testAt) {
		t.Fatalf("space-track load after reopen = %+v", l)
	}
}

func TestStore_ReplaceSnapshotRemovesOldRowsOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.db")
	ctx := context.Background()
	r := openTestRegistry(t, path)
	_ = r.ReplaceTransmitters(ctx, SourceAMSAT, []Transmitter{{NoradID: 1, DownlinkHz: 145_000_000}}, testAt)
	_ = r.ReplaceTransmitters(ctx, SourceAMSAT, []Transmitter{{NoradID: 2, DownlinkHz: 437_000_000}}, testAt)
	_ = r.Close()

	again := openTestRegistry(t, path)
	if len(again.Transmitters(1)) != 0 || len(again.Transmitters(2)) != 1 {
		t.Fatalf("after reopen: 1=%v 2=%v, want only NORAD 2", again.Transmitters(1), again.Transmitters(2))
	}
}
