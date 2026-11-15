package tracker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

// addAged adds a TLE with epoch shifted back by age from now.
func addAged(store *TLEStore, norad int, now time.Time, age time.Duration) {
	store.AddWithGroup(&TLE{NoradID: norad, Name: "SAT", Epoch: now.Add(-age)}, "amateur")
}

func TestTLEStore_Status_EmptyCatalogIsStale(t *testing.T) {
	store := NewTLEStore(nil)

	st := store.Status(time.Now())

	if st.Level != TLELevelStale {
		t.Fatalf("Level = %q, want %q", st.Level, TLELevelStale)
	}
	if !slices.Contains(st.Reasons, TLEReasonEmpty) {
		t.Fatalf("Reasons = %v, want %q", st.Reasons, TLEReasonEmpty)
	}
}

func TestTLEStore_Status_FreshEpochsAreOK(t *testing.T) {
	now := time.Now()
	store := NewTLEStore(nil)
	for id := 1; id <= 3; id++ {
		addAged(store, id, now, 12*time.Hour)
	}

	st := store.Status(now)

	if st.Level != TLELevelOK {
		t.Fatalf("Level = %q, want ok (reasons %v)", st.Level, st.Reasons)
	}
	if st.Count != 3 {
		t.Fatalf("Count = %d, want 3", st.Count)
	}
	if st.MedianAgeHours < 11.9 || st.MedianAgeHours > 12.1 {
		t.Fatalf("MedianAgeHours = %.2f, want 12", st.MedianAgeHours)
	}
	if st.ISSAgeHours != nil {
		t.Fatalf("ISSAgeHours = %v, want nil without ISS", *st.ISSAgeHours)
	}
}

func TestTLEStore_Status_MedianAgeThresholds(t *testing.T) {
	cases := []struct {
		name string
		age  time.Duration
		want TLELevel
	}{
		{"under warn", 47 * time.Hour, TLELevelOK},
		{"over warn", 3 * 24 * time.Hour, TLELevelWarn},
		{"over max age", 8 * 24 * time.Hour, TLELevelStale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			store := NewTLEStore(nil)
			for id := 1; id <= 3; id++ {
				addAged(store, id, now, tc.age)
			}

			st := store.Status(now)

			if st.Level != tc.want {
				t.Fatalf("Level = %q, want %q (reasons %v)", st.Level, tc.want, st.Reasons)
			}
			if tc.want != TLELevelOK && !slices.Contains(st.Reasons, TLEReasonMedianAge) {
				t.Fatalf("Reasons = %v, want %q", st.Reasons, TLEReasonMedianAge)
			}
		})
	}
}

func TestTLEStore_Status_OldTailDoesNotRaiseLevel(t *testing.T) {
	now := time.Now()
	store := NewTLEStore(nil)
	for id := 1; id <= 4; id++ {
		addAged(store, id, now, 12*time.Hour)
	}
	addAged(store, 99, now, 30*24*time.Hour)

	st := store.Status(now)

	if st.Level != TLELevelOK {
		t.Fatalf("Level = %q, want ok (reasons %v)", st.Level, st.Reasons)
	}
	if st.StaleCount != 1 {
		t.Fatalf("StaleCount = %d, want 1", st.StaleCount)
	}
}

func TestTLEStore_Status_ISSThresholds(t *testing.T) {
	cases := []struct {
		name string
		age  time.Duration
		want TLELevel
	}{
		{"fresh", 12 * time.Hour, TLELevelOK},
		{"over warn", 30 * time.Hour, TLELevelWarn},
		{"over stale", 4 * 24 * time.Hour, TLELevelStale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			store := NewTLEStore(nil)
			for id := 1; id <= 3; id++ {
				addAged(store, id, now, 6*time.Hour)
			}
			addAged(store, issNoradID, now, tc.age)

			st := store.Status(now)

			if st.Level != tc.want {
				t.Fatalf("Level = %q, want %q (reasons %v)", st.Level, tc.want, st.Reasons)
			}
			if st.ISSAgeHours == nil {
				t.Fatal("ISSAgeHours = nil, want value")
			}
			if tc.want != TLELevelOK && !slices.Contains(st.Reasons, TLEReasonISSAge) {
				t.Fatalf("Reasons = %v, want %q", st.Reasons, TLEReasonISSAge)
			}
		})
	}
}

func TestTLEStore_Status_SourceCelestrakAfterFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(testTLEData))
	}))
	t.Cleanup(srv.Close)
	store := newCachedStore(t, srv.URL, 0)

	before := time.Now()
	if err := store.LoadGroup(context.Background(), "amateur"); err != nil {
		t.Fatalf("LoadGroup: %v", err)
	}
	st := store.Status(time.Now())

	if st.Source != TLESourceCelestrak {
		t.Fatalf("Source = %q, want %q", st.Source, TLESourceCelestrak)
	}
	if st.FetchedAt.Before(before) {
		t.Fatalf("FetchedAt = %v, want after %v", st.FetchedAt, before)
	}
	if st.LastError != "" {
		t.Fatalf("LastError = %q, want empty", st.LastError)
	}
	if slices.Contains(st.Reasons, TLEReasonNetwork) {
		t.Fatalf("Reasons = %v, network must be fine after fetch", st.Reasons)
	}
}

func TestTLEStore_Status_FetchErrorFallsBackToCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	store := newCachedStore(t, srv.URL, -30*time.Minute)

	if err := store.LoadGroup(context.Background(), "amateur"); err != nil {
		t.Fatalf("LoadGroup: %v", err)
	}
	st := store.Status(time.Now())

	if st.Source != TLESourceCache {
		t.Fatalf("Source = %q, want %q", st.Source, TLESourceCache)
	}
	if st.LastError == "" {
		t.Fatal("LastError is empty, want network error")
	}
	if st.FetchedAt.IsZero() {
		t.Fatal("FetchedAt is zero, want time of cached download")
	}
}

func TestTLEStore_Status_OldDownloadWarnsNetwork(t *testing.T) {
	// UpdateInterval в newCachedStore — 1 ч; кеш скачан 3 ч назад, это больше двух интервалов.
	store := newCachedStore(t, "http://127.0.0.1:0", -3*time.Hour)
	store.loadCachedGroups()

	st := store.Status(time.Now())

	if st.Source != TLESourceCache {
		t.Fatalf("Source = %q, want %q", st.Source, TLESourceCache)
	}
	if !slices.Contains(st.Reasons, TLEReasonNetwork) {
		t.Fatalf("Reasons = %v, want %q", st.Reasons, TLEReasonNetwork)
	}
	if st.Level == TLELevelOK {
		t.Fatal("Level = ok, want at least warn")
	}
}

func TestTLEStore_Status_RecentCacheDoesNotWarnNetwork(t *testing.T) {
	store := newCachedStore(t, "http://127.0.0.1:0", -30*time.Minute)
	store.loadCachedGroups()

	st := store.Status(time.Now())

	if slices.Contains(st.Reasons, TLEReasonNetwork) {
		t.Fatalf("Reasons = %v, cache is within update interval", st.Reasons)
	}
}

// freshStore holds three satellites with fresh epochs, so only the pass check can lower the level.
func freshStore(now time.Time) *TLEStore {
	store := NewTLEStore(nil)
	for id := 1; id <= 3; id++ {
		addAged(store, id, now, 6*time.Hour)
	}
	return store
}

func TestTLEStore_Status_PassDriftThresholds(t *testing.T) {
	cases := []struct {
		name    string
		matched int
		median  float64
		want    TLELevel
	}{
		{"within a minute", 5, 30, TLELevelOK},
		{"over a minute", 5, 90, TLELevelWarn},
		{"over five minutes", 5, 400, TLELevelStale},
		{"too few pairs", 2, 400, TLELevelOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			store := freshStore(now)
			store.SetPassCheck(&PassCheck{Source: "heavens-above", Matched: tc.matched, MedianDriftS: tc.median})

			st := store.Status(now)

			if st.Level != tc.want {
				t.Fatalf("Level = %q, want %q (reasons %v)", st.Level, tc.want, st.Reasons)
			}
			if got := slices.Contains(st.Reasons, TLEReasonPassDrift); got != (tc.want != TLELevelOK) {
				t.Fatalf("Reasons = %v, pass_drift expected = %v", st.Reasons, tc.want != TLELevelOK)
			}
			if st.PassCheck == nil || st.PassCheck.Matched != tc.matched {
				t.Fatalf("PassCheck = %+v, want it in status", st.PassCheck)
			}
		})
	}
}

func TestTLEStore_Status_ISSPassDriftWarnsAlone(t *testing.T) {
	now := time.Now()
	store := freshStore(now)
	iss := 90.0
	store.SetPassCheck(&PassCheck{Matched: 1, MedianDriftS: 90, ISSDriftS: &iss})

	st := store.Status(now)

	if st.Level != TLELevelWarn || !slices.Contains(st.Reasons, TLEReasonPassDrift) {
		t.Fatalf("Level = %q, Reasons = %v; want warn with pass_drift", st.Level, st.Reasons)
	}
}

func TestTLEStore_Status_NoPassCheckOffline(t *testing.T) {
	now := time.Now()
	store := freshStore(now)
	store.SetPassCheck(&PassCheck{Matched: 5, MedianDriftS: 400})
	// Без сети и свежего кеша Heavens-Above сверять не с чем.
	store.SetPassCheck(nil)

	st := store.Status(now)

	if st.Level != TLELevelOK || st.PassCheck != nil {
		t.Fatalf("Level = %q, PassCheck = %+v; want ok without check", st.Level, st.PassCheck)
	}
}

func TestTLEStore_Start_StaleTLEDoesNotFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	store := newCachedStore(t, srv.URL, -48*time.Hour)
	refreshed := make(chan struct{}, 1)
	store.SetOnRefresh(func() {
		select {
		case refreshed <- struct{}{}:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		store.Stop()
	})

	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st := store.Status(time.Now()); st.Level != TLELevelStale {
		t.Fatalf("Level = %q, want stale for 2024 epochs", st.Level)
	}

	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh hook was not called after background load")
	}
}
