package tracker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	oldEpoch   = "24001.50000000"
	freshEpoch = "26316.50000000"
)

// catalogFixture — хранилище с кешем группы amateur (МКС со старой эпохой)
// и фейковыми Space-Track, sonik и Celestrak.
type catalogFixture struct {
	cfg           *TLEStoreConfig
	opts          []TLEStoreOption
	store         *TLEStore
	st            *fakeSpaceTrack
	celestrakHits *atomic.Int32
}

func compositionIDs(ids ...int) TLEStoreOption {
	cp := append([]int(nil), ids...)
	return WithComposition(func() []int { return append([]int(nil), cp...) })
}

func newCatalogFixture(t *testing.T, sonik *httptest.Server, composition []int, stTLEs ...*TLE) *catalogFixture {
	t.Helper()
	hits := &atomic.Int32{}
	celestrak := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(celestrak.Close)
	st, stSrv := newFakeSpaceTrack(t, stTLEs...)

	cfg := DefaultTLEStoreConfig()
	cfg.CacheDir = t.TempDir()
	cfg.Groups = []string{"amateur"}
	cfg.UpdateInterval = time.Hour
	opts := []TLEStoreOption{
		WithCelestrakClient(NewCelestrakClient(WithBaseURL(celestrak.URL), WithRateLimit(0), WithMaxRetries(0))),
		WithSpaceTrackClient(newTestSpaceTrackClient(stSrv.URL)),
		compositionIDs(composition...),
	}
	if sonik != nil {
		opts = append(opts, WithSonikClient(NewSonikClient(WithSonikURL(sonik.URL))))
	}

	f := &catalogFixture{cfg: cfg, opts: opts, st: st, celestrakHits: hits}
	f.store = NewTLEStore(cfg, opts...)
	if err := f.store.saveGroupToCache("amateur", []*TLE{makeTestTLE(t, 25544, "ISS (ZARYA)", oldEpoch)}); err != nil {
		t.Fatalf("saveGroupToCache: %v", err)
	}
	f.store.loadCachedGroups()
	return f
}

// reopen создаёт новое хранилище поверх того же каталога кеша — как после рестарта сервера.
func (f *catalogFixture) reopen() *TLEStore {
	return NewTLEStore(f.cfg, f.opts...)
}

func catalogFileIDs(t *testing.T, dir string) []int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, catalogCacheName+tleCacheExtension))
	if err != nil {
		t.Fatalf("read catalog cache: %v", err)
	}
	tles, err := ParseTLEBatch(string(data))
	if err != nil {
		t.Fatalf("parse catalog cache: %v", err)
	}
	ids := make([]int, 0, len(tles))
	for _, tle := range tles {
		ids = append(ids, tle.NoradID)
	}
	slices.Sort(ids)
	return ids
}

func TestTLEStore_RefreshCatalog_SpaceTrackBeforeCelestrak(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{25544, "in orbit", ""}, sonikEntry{40069, "in orbit", ""})
	f := newCatalogFixture(t, sonik, []int{25544, 40069},
		makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch),
		makeTestTLE(t, 40069, "METEOR-M2", freshEpoch),
	)

	if err := f.store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}

	if n := f.celestrakHits.Load(); n != 0 {
		t.Fatalf("celestrak hits = %d, want 0 when Space-Track answered", n)
	}
	iss, ok := f.store.Get(25544)
	if !ok || iss.Epoch.Year() != 2026 {
		t.Fatalf("ISS epoch = %v, want fresh element from Space-Track", iss)
	}
	if _, found := f.store.Get(40069); !found {
		t.Fatal("NORAD 40069 from composition should be in catalog")
	}
	if got := f.store.GetByGroup("amateur"); len(got) != 1 || got[0].NoradID != 25544 {
		t.Fatalf("amateur label lost: %v", got)
	}
	ungrouped := f.store.GetUngrouped()
	if len(ungrouped) != 1 || ungrouped[0].NoradID != 40069 {
		t.Fatalf("ungrouped = %v, want only 40069", ungrouped)
	}
}

func TestTLEStore_RefreshCatalog_KeepsOldElementsForMissingNorad(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{40069, "in orbit", ""})
	f := newCatalogFixture(t, sonik, []int{25544, 40069}, makeTestTLE(t, 40069, "METEOR-M2", freshEpoch))

	if err := f.store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}

	iss, ok := f.store.Get(25544)
	if !ok || iss.Epoch.Year() != 2024 {
		t.Fatalf("ISS = %v, want old cached element kept", iss)
	}
	if got := catalogFileIDs(t, f.cfg.CacheDir); fmt.Sprint(got) != "[25544 40069]" {
		t.Fatalf("catalog cache ids = %v, want old ISS kept alongside 40069", got)
	}
}

func TestTLEStore_RefreshCatalog_ProgressReportsGPThenClears(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{norad: 25544, status: "in orbit"})
	_, stSrv := newFakeSpaceTrack(t, makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch))
	cfg := DefaultTLEStoreConfig()
	cfg.CacheDir = t.TempDir()
	cfg.Groups = []string{"amateur"}
	cfg.UpdateInterval = time.Hour
	store := NewTLEStore(cfg,
		WithSpaceTrackClient(newTestSpaceTrackClient(stSrv.URL, WithSpaceTrackBatchSize(1))),
		WithSonikClient(NewSonikClient(WithSonikURL(sonik.URL))),
		compositionIDs(25544),
	)
	var snaps []TLEStatus
	store.SetOnProgress(func() {
		snaps = append(snaps, store.Status(time.Now()))
	})

	if err := store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}
	if len(snaps) < 2 {
		t.Fatalf("progress snaps = %d, want at least begin and GP", len(snaps))
	}
	var sawGP, sawSatcat bool
	for _, st := range snaps {
		if st.Loading && st.LoadPhase == TLELoadPhaseGP {
			sawGP = true
		}
		if st.Loading && st.LoadPhase == TLELoadPhaseSatcat {
			sawSatcat = true
		}
	}
	if !sawGP || !sawSatcat {
		t.Fatalf("phases in snaps = %+v, want gp and satcat", snaps)
	}
	final := store.Status(time.Now())
	if final.Loading || final.LoadTotal != 0 {
		t.Fatalf("final status still loading: %+v", final)
	}
}

func TestTLEStore_RefreshCatalog_PurgesOutsideComposition(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{99999, "in orbit", "2010-01-01"})
	f := newCatalogFixture(t, sonik, []int{25544}, makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch))
	f.store.Add(makeTestTLE(t, 99999, "OLD SONIK", oldEpoch))

	if err := f.store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}

	if _, ok := f.store.Get(99999); ok {
		t.Fatal("NORAD outside composition must be removed from memory")
	}
	if got := catalogFileIDs(t, f.cfg.CacheDir); fmt.Sprint(got) != "[25544]" {
		t.Fatalf("catalog cache ids = %v, want only composition", got)
	}
	_, queries := f.st.stats()
	if fmt.Sprint(queries) != "[[25544]]" {
		t.Fatalf("queries = %v, want only composition [[25544]]", queries)
	}
}

func TestTLEStore_RefreshCatalog_FreshSonikLaunchIncluded(t *testing.T) {
	launched := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
	sonik := newFakeSonik(t,
		sonikEntry{25544, "in orbit", "1998-11-20"},
		sonikEntry{99100, "in orbit", launched},
	)
	f := newCatalogFixture(t, sonik, []int{25544},
		makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch),
		makeTestTLE(t, 99100, "FRESH CUBESAT", freshEpoch),
	)

	if err := f.store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}

	_, queries := f.st.stats()
	if fmt.Sprint(queries) != "[[25544 99100]]" {
		t.Fatalf("queries = %v, want composition plus fresh sonik", queries)
	}
	if _, ok := f.store.Get(99100); !ok {
		t.Fatal("fresh sonik launch must stay in catalog")
	}
}

func TestTLEStore_RefreshCatalog_SonikDownUsesComposition(t *testing.T) {
	sonik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(sonik.Close)
	f := newCatalogFixture(t, sonik, []int{25544}, makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch))

	if err := f.store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}

	if _, queries := f.st.stats(); fmt.Sprint(queries) != "[[25544]]" {
		t.Fatalf("queries = %v, want composition [[25544]]", queries)
	}
	if iss, _ := f.store.Get(25544); iss.Epoch.Year() != 2026 {
		t.Fatalf("ISS epoch year = %d, want 2026", iss.Epoch.Year())
	}
}

func TestTLEStore_RefreshCatalog_EmptyCompositionKeepsCache(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{99999, "in orbit", "2010-01-01"})
	f := newCatalogFixture(t, sonik, nil, makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch))
	f.store.Add(makeTestTLE(t, 25544, "ISS (ZARYA)", oldEpoch))

	if err := f.store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}
	if _, queries := f.st.stats(); len(queries) != 0 {
		t.Fatalf("queries = %v, want none when composition empty", queries)
	}
	if _, ok := f.store.Get(25544); !ok {
		t.Fatal("cached ISS must remain when composition is empty")
	}
}

func TestTLEStore_Refresh_FallsBackToCelestrakOnSpaceTrackError(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{25544, "in orbit", ""})
	f := newCatalogFixture(t, sonik, []int{25544}, makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch))
	f.st.failStatus = http.StatusServiceUnavailable

	f.store.refresh(context.Background(), f.cfg.Groups)

	if n := f.celestrakHits.Load(); n == 0 {
		t.Fatal("expected Celestrak fallback after Space-Track error")
	}
	st := f.store.Status(time.Now())
	if st.Source != TLESourceCache {
		t.Fatalf("status source = %q, want %q", st.Source, TLESourceCache)
	}
	if !strings.Contains(st.LastError, "space-track") {
		t.Fatalf("status last_error = %q, want Space-Track error", st.LastError)
	}
}

func TestTLEStore_Start_FreshCatalogSkipsNetwork(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{25544, "in orbit", ""}, sonikEntry{40069, "in orbit", ""})
	f := newCatalogFixture(t, sonik, []int{25544, 40069},
		makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch),
		makeTestTLE(t, 40069, "METEOR-M2", freshEpoch),
	)
	if err := f.store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}

	store := f.reopen()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		store.Stop()
	})
	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.Count() != 2 {
		t.Fatalf("Count = %d, want 2 from catalog cache", store.Count())
	}
	if iss, _ := store.Get(25544); iss.Epoch.Year() != 2026 {
		t.Fatalf("ISS epoch year = %d, want catalog element over group cache", iss.Epoch.Year())
	}

	time.Sleep(200 * time.Millisecond)
	if _, queries := f.st.stats(); len(queries) != 1 {
		t.Fatalf("queries after Start = %v, want only the first refresh", queries)
	}
}

func TestTLEStore_Start_MissingCatalogRefreshesFromSpaceTrack(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{25544, "in orbit", ""}, sonikEntry{40069, "in orbit", ""})
	f := newCatalogFixture(t, sonik, []int{25544, 40069},
		makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch),
		makeTestTLE(t, 40069, "METEOR-M2", freshEpoch),
	)

	store := f.reopen()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		store.Stop()
	})
	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for store.Count() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if store.Count() != 2 {
		t.Fatalf("Count = %d, want 2 after background Space-Track pass", store.Count())
	}
	if n := f.celestrakHits.Load(); n != 0 {
		t.Fatalf("celestrak hits = %d, want 0", n)
	}
}

func TestTLEStore_Start_StaleMetaWithoutCatalogFileRefreshes(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{norad: 25544, status: "in orbit"})
	f := newCatalogFixture(t, sonik, []int{25544}, makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch))
	if err := f.store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}
	if err := os.Remove(filepath.Join(f.cfg.CacheDir, catalogCacheName+tleCacheExtension)); err != nil {
		t.Fatalf("remove catalog.tle: %v", err)
	}

	store := f.reopen()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		store.Stop()
	})
	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, queries := f.st.stats()
		if len(queries) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queries = %v, want Start to refresh when catalog.tle is gone but cache_meta is fresh", queries)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.CacheDir, catalogCacheName+tleCacheExtension)); err != nil {
		t.Fatalf("catalog.tle after Start: %v", err)
	}
}

func sinkStore(f *catalogFixture, passes *[]CatalogPass) *TLEStore {
	opts := append(slices.Clone(f.opts), WithCatalogSink(func(_ context.Context, p CatalogPass) {
		*passes = append(*passes, p)
	}))
	store := NewTLEStore(f.cfg, opts...)
	store.loadCachedGroups()
	return store
}

func TestTLEStore_RefreshCatalog_SinkGetsGPSonikAndSatcatInOnePass(t *testing.T) {
	sonik := newFakeSonik(
		t,
		sonikEntry{25544, "in orbit", ""},
		sonikEntry{40069, "in orbit", ""},
		sonikEntry{2012, "re-entered", ""},
	)
	f := newCatalogFixture(t, sonik, []int{25544, 40069},
		makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch),
		makeTestTLE(t, 40069, "METEOR-M2", freshEpoch),
	)
	f.st.satcat[25544] = ""
	f.st.satcat[40069] = "2026-01-02"
	var passes []CatalogPass
	store := sinkStore(f, &passes)

	if err := store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("refreshCatalog: %v", err)
	}

	if len(passes) != 1 {
		t.Fatalf("sink calls = %d, want 1", len(passes))
	}
	p := passes[0]
	if p.Err != nil || p.SonikErr != nil || p.SatcatErr != nil {
		t.Fatalf("errors = %v / %v / %v, want none", p.Err, p.SonikErr, p.SatcatErr)
	}
	if len(p.TLEs) != 2 {
		t.Fatalf("TLEs = %d, want 2 from GP", len(p.TLEs))
	}
	if len(p.Sonik) != 3 {
		t.Fatalf("Sonik = %d, want full sonik snapshot including re-entered", len(p.Sonik))
	}
	if len(p.Satcat) != 2 || p.SatcatBytes == 0 {
		t.Fatalf("Satcat = %d records / %d bytes, want 2 with body", len(p.Satcat), p.SatcatBytes)
	}
}

func TestTLEStore_RefreshCatalog_SatcatErrorKeepsPass(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{25544, "in orbit", ""})
	f := newCatalogFixture(t, sonik, []int{25544}, makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch))
	f.st.satcatFail = http.StatusInternalServerError
	var passes []CatalogPass
	store := sinkStore(f, &passes)

	if err := store.refreshCatalog(context.Background()); err != nil {
		t.Fatalf("SATCAT failure must not fail the pass: %v", err)
	}
	if iss, _ := store.Get(25544); iss.Epoch.Year() != 2026 {
		t.Fatalf("ISS epoch year = %d, want 2026", iss.Epoch.Year())
	}
	if len(passes) != 1 || passes[0].SatcatErr == nil || len(passes[0].TLEs) != 1 {
		t.Fatalf("passes = %+v, want one pass with TLEs and SatcatErr", passes)
	}
}

func TestTLEStore_RefreshCatalog_GPErrorReachesSinkWithoutSatcat(t *testing.T) {
	sonik := newFakeSonik(t, sonikEntry{25544, "in orbit", ""})
	f := newCatalogFixture(t, sonik, []int{25544}, makeTestTLE(t, 25544, "ISS (ZARYA)", freshEpoch))
	f.st.failStatus = http.StatusServiceUnavailable
	var passes []CatalogPass
	store := sinkStore(f, &passes)

	if err := store.refreshCatalog(context.Background()); err == nil {
		t.Fatal("expected GP error")
	}
	if len(passes) != 1 || passes[0].Err == nil || len(passes[0].Sonik) != 1 {
		t.Fatalf("passes = %+v, want one pass with Err and sonik records", passes)
	}
	if q := f.st.satcatStats(); len(q) != 0 {
		t.Fatalf("satcat queries = %v, want none after GP failure", q)
	}
}

func TestTLEStore_GetUngrouped(t *testing.T) {
	store := NewTLEStore(nil)
	store.AddWithGroup(&TLE{NoradID: 1, Name: "A"}, "amateur")
	store.Add(&TLE{NoradID: 2, Name: "B"})

	got := store.GetUngrouped()
	if len(got) != 1 || got[0].NoradID != 2 {
		t.Fatalf("GetUngrouped = %v, want only NORAD 2", got)
	}
}
