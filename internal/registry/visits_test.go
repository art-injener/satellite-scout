package registry

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/art-injener/satellite-scout/internal/freqplan"
)

func TestStore_CreatesRxVisitsTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.db")
	r := openTestRegistry(t, path)
	_ = r.Close()

	db, err := sql.Open(sqliteDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'rx_visits'`).Scan(&name)
	if err != nil || name != "rx_visits" {
		t.Fatalf("rx_visits missing: name=%q err=%v", name, err)
	}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("user_version = %d, want 3", version)
	}
}

func TestStore_MigratesV1ToV2AddsRxVisits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.db")
	db, err := sql.Open(sqliteDriver, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`PRAGMA user_version = 1`); err != nil {
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
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'rx_visits'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("rx_visits must exist after v1→v2 migrate")
	}
}

func TestRegistry_AppendVisits_RoundTripAndHeard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scout.db")
	ctx := context.Background()
	r := openTestRegistry(t, path)

	energy := []freqplan.Visit{{
		RadioPath: "rp1", CenterHz: 436_500_000, SpanHz: 1_600_000,
		WindowStartMs: 10, WindowEndMs: 70,
		NoradID: 27607, Name: "SO-50", UUID: "u", FreqHz: 436_795_000,
		Energy: true, SNRDB: 12.5, Origin: "satellite", Emission: "pulsed",
		Received: true, Source: freqplan.SourceCurated,
	}}
	silent := []freqplan.Visit{{
		RadioPath: "rp1", CenterHz: 145_900_000, SpanHz: 1_600_000,
		WindowStartMs: 70, WindowEndMs: 130,
		NoradID: 43017, Name: "AO-91", FreqHz: 145_960_000,
		Energy: false, Received: false, Source: freqplan.SourceCurated,
	}}
	if err := r.AppendVisits(ctx, energy); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendVisits(ctx, silent); err != nil {
		t.Fatal(err)
	}
	if !r.HasLabel(27607, LabelHeard, rtlBands) {
		t.Fatal("energy visit must mark NORAD as heard")
	}
	if r.HasLabel(43017, LabelHeard, rtlBands) {
		t.Fatal("silent visit must not mark NORAD as heard")
	}

	got, err := r.ListVisits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]freqplan.Visit{}, energy...), silent...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	_ = r.Close()

	again := openTestRegistry(t, path)
	if !again.HasLabel(27607, LabelHeard, rtlBands) {
		t.Fatal("heard must survive reopen")
	}
	got, err = again.ListVisits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after reopen got %+v, want %+v", got, want)
	}
}

func TestRegistry_AppendVisits_EmptyIsNoop(t *testing.T) {
	r := openTestRegistry(t, filepath.Join(t.TempDir(), "scout.db"))
	if err := r.AppendVisits(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListVisits(context.Background())
	if err != nil || got != nil {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestRegistry_Heard_MemoryAppendMarksHeard(t *testing.T) {
	r := New()
	if r.HasLabel(1, LabelHeard, rtlBands) {
		t.Fatal("without visits heard must be false")
	}
	_ = r.AppendVisits(context.Background(), []freqplan.Visit{{
		NoradID: 1, Energy: true, FreqHz: 145_800_000,
	}})
	// Без базы факт живёт только в памяти процесса (как остальные снимки реестра).
	if !r.HasLabel(1, LabelHeard, rtlBands) {
		t.Fatal("AppendVisits must update heard in memory even without store")
	}
}
