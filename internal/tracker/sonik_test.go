package tracker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sonikEntry — одна запись каталога sonik в тестовом ответе.
type sonikEntry struct {
	norad    int
	status   string
	launched string // ГГГГ-ММ-ДД; пусто — поля launched в JSON нет
}

// newFakeSonik отдаёт каталог в формате sonik.space /satellites/data/.
func newFakeSonik(t *testing.T, entries ...sonikEntry) *httptest.Server {
	t.Helper()
	rows := make([]string, 0, len(entries))
	for _, e := range entries {
		launched := ""
		if e.launched != "" {
			launched = fmt.Sprintf(`, "launched": %q`, e.launched)
		}
		rows = append(rows, fmt.Sprintf(
			`{"sat_id": "XXXX-%d", "name": "SAT %d", "norad_cat_id": %d, "status": %q, "tx_count": 1%s}`,
			e.norad, e.norad, e.norad, e.status, launched))
	}
	body := `{"data": [` + strings.Join(rows, ", ") + `]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSonikFreshLaunchIDs_OnlyRecentInOrbitOrFuture(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	sats := []SonikSat{
		{NoradID: 100, Status: sonikStatusInOrbit, Launched: "2026-10-01"},  // 45 суток
		{NoradID: 101, Status: sonikStatusInOrbit, Launched: "2026-08-01"},  // старше 60
		{NoradID: 102, Status: sonikStatusFuture, Launched: "2026-12-01"},   // будущий
		{NoradID: 103, Status: sonikStatusInOrbit, Launched: ""},            // без даты
		{NoradID: 104, Status: "re-entered", Launched: "2026-10-01"},
		{NoradID: 105, Status: sonikStatusInOrbit, Launched: "not-a-date"},
		{NoradID: 0, Status: sonikStatusInOrbit, Launched: "2026-10-01"},
	}
	got := SonikFreshLaunchIDs(sats, now)
	want := []int{100, 102}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("SonikFreshLaunchIDs = %v, want %v", got, want)
	}
}

func TestSonik_FetchNoradIDs_KeepsInOrbitAndFuture(t *testing.T) {
	srv := newFakeSonik(t,
		sonikEntry{norad: 43017, status: "in orbit"},
		sonikEntry{norad: 25544, status: "in orbit"},
		sonikEntry{norad: 99681, status: "future"},
		sonikEntry{norad: 2012, status: "re-entered"},
		sonikEntry{norad: 0, status: "in orbit"},
		sonikEntry{norad: 25544, status: "in orbit"},
	)

	ids, err := NewSonikClient(WithSonikURL(srv.URL)).FetchNoradIDs(context.Background())
	if err != nil {
		t.Fatalf("FetchNoradIDs: %v", err)
	}
	want := []int{25544, 43017, 99681}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}

func TestSonik_FetchNoradIDs_HTTPErrorFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	_, err := NewSonikClient(WithSonikURL(srv.URL)).FetchNoradIDs(context.Background())
	if !errors.Is(err, ErrSonikStatus) {
		t.Fatalf("err = %v, want ErrSonikStatus", err)
	}
}

func TestSonik_FetchNoradIDs_BrokenJSONFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data": [`))
	}))
	t.Cleanup(srv.Close)

	if _, err := NewSonikClient(WithSonikURL(srv.URL)).FetchNoradIDs(context.Background()); err == nil {
		t.Fatal("expected error for truncated JSON")
	}
}

func TestSonik_FetchCatalog_KeepsAllStatusesAndCardFields(t *testing.T) {
	body := `{"data": [
		{"sat_id": "XSKZ", "name": "ISS", "norad_cat_id": 25544, "status": "in orbit", "operator": "NASA",
		 "network": "", "tx_count": 53, "call_sign": "RS0ISS", "total_obs": 7260, "launched": "1998-11-20", "countries": ["ru", "us"]},
		{"sat_id": "HUET", "name": "OPS 1184", "norad_cat_id": 2012, "status": "re-entered", "operator": "",
		 "network": "", "tx_count": 0, "call_sign": "", "total_obs": 0, "launched": "", "countries": ["us"]},
		{"sat_id": "ZERO", "name": "NO NORAD", "norad_cat_id": 0, "status": "in orbit"}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	sats, err := NewSonikClient(WithSonikURL(srv.URL)).FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	want := []SonikSat{
		{NoradID: 2012, Name: "OPS 1184", Status: "re-entered"},
		{NoradID: 25544, Name: "ISS", Status: "in orbit", CallSign: "RS0ISS", Operator: "NASA", Launched: "1998-11-20"},
	}
	if fmt.Sprint(sats) != fmt.Sprint(want) {
		t.Fatalf("sats = %+v, want %+v", sats, want)
	}
}

func TestSonik_FetchNoradIDs_NoActiveSatellitesFails(t *testing.T) {
	srv := newFakeSonik(t, sonikEntry{norad: 2012, status: "re-entered"})

	_, err := NewSonikClient(WithSonikURL(srv.URL)).FetchNoradIDs(context.Background())
	if !errors.Is(err, ErrSonikEmpty) {
		t.Fatalf("err = %v, want ErrSonikEmpty", err)
	}
}
