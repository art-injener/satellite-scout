package tracker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	testSTIdentity = "user@example.org"
	testSTPassword = "s3cret-Pa55"
)

// makeTestTLE собирает корректный TLE для NORAD с заданной эпохой (YYDDD.DDDDDDDD).
func makeTestTLE(t *testing.T, norad int, name, epoch string) *TLE {
	t.Helper()
	l1 := makeTLELineTest(fmt.Sprintf("1 %05dU 98067A   %s  .00016717  00000-0  10270-3 0  999", norad, epoch))
	l2 := makeTLELineTest(fmt.Sprintf("2 %05d  51.6400 247.4627 0006703 130.5360 325.0288 15.4981557142340", norad))
	tle, err := ParseTLE([]string{name, l1, l2})
	if err != nil {
		t.Fatalf("ParseTLE(%d): %v", norad, err)
	}
	return tle
}

// fakeSpaceTrack имитирует логин и запрос GP площадки Space-Track.
type fakeSpaceTrack struct {
	mu          sync.Mutex
	elements    map[int]*TLE
	rejectLogin bool
	expireNext  bool // следующий запрос получит 401, сессия сбрасывается
	failStatus  int  // код ответа на запрос GP; 0 — обычный ответ
	session     string
	logins      int
	queries     [][]int

	// SATCAT: NORAD и дата схода ("" — на орбите).
	satcat         map[int]string
	satcatFail     int
	satcatFailFrom int // с какого запроса SATCAT (с 1) отдавать satcatFail; 0 — с первого
	satcatHits     int
	satcatQueries  [][]int
}

func newFakeSpaceTrack(t *testing.T, tles ...*TLE) (*fakeSpaceTrack, *httptest.Server) {
	t.Helper()
	f := &fakeSpaceTrack{elements: make(map[int]*TLE), satcat: make(map[int]string)}
	for _, tle := range tles {
		f.elements[tle.NoradID] = tle
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ajaxauth/login", f.login)
	mux.HandleFunc("GET /basicspacedata/query/class/gp/NORAD_CAT_ID/{ids}/format/3le", f.query)
	mux.HandleFunc(
		"GET /basicspacedata/query/class/satcat/NORAD_CAT_ID/{ids}/predicates/NORAD_CAT_ID,DECAY/format/json",
		f.querySatcat,
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeSpaceTrack) login(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logins++
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if f.rejectLogin || r.PostForm.Get("identity") != testSTIdentity || r.PostForm.Get("password") != testSTPassword {
		_, _ = w.Write([]byte(`{"Login":"Failed"}`))
		return
	}
	f.session = "session-" + strconv.Itoa(f.logins)
	http.SetCookie(w, &http.Cookie{Name: "chocolatechip", Value: f.session, Path: "/"})
	_, _ = w.Write([]byte(`""`))
}

func (f *fakeSpaceTrack) query(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := r.Cookie("chocolatechip")
	if err != nil || f.session == "" || c.Value != f.session {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.expireNext {
		f.expireNext = false
		f.session = ""
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.failStatus != 0 {
		w.WriteHeader(f.failStatus)
		return
	}
	var ids []int
	for s := range strings.SplitSeq(r.PathValue("ids"), ",") {
		id, convErr := strconv.Atoi(s)
		if convErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	f.queries = append(f.queries, ids)
	var body strings.Builder
	for _, id := range ids {
		if tle, ok := f.elements[id]; ok {
			_, _ = fmt.Fprintf(&body, "0 %s\r\n%s\r\n%s\r\n", tle.Name, tle.Line1, tle.Line2)
		}
	}
	// Как площадка: пустой результат отдаётся кодом 204 без тела.
	if body.Len() == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_, _ = w.Write([]byte(body.String()))
}

// stats возвращает число входов и NORAD каждого запроса GP.
func (f *fakeSpaceTrack) stats() (int, [][]int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins, slices.Clone(f.queries)
}

func newTestSpaceTrackClient(url string, opts ...SpaceTrackOption) *SpaceTrackClient {
	all := append([]SpaceTrackOption{WithSpaceTrackBaseURL(url), WithSpaceTrackRateLimit(0)}, opts...)
	return NewSpaceTrackClient(testSTIdentity, testSTPassword, all...)
}

func TestSpaceTrack_FetchByNoradIDs_LogsInAndParses3LE(t *testing.T) {
	iss := makeTestTLE(t, 25544, "ISS (ZARYA)", "26316.50000000")
	meteor := makeTestTLE(t, 40069, "METEOR-M2", "26316.40000000")
	fake, srv := newFakeSpaceTrack(t, iss, meteor)

	tles, err := newTestSpaceTrackClient(srv.URL).FetchByNoradIDs(context.Background(), []int{25544, 40069})
	if err != nil {
		t.Fatalf("FetchByNoradIDs: %v", err)
	}
	if len(tles) != 2 {
		t.Fatalf("got %d TLEs, want 2", len(tles))
	}
	byID := map[int]*TLE{tles[0].NoradID: tles[0], tles[1].NoradID: tles[1]}
	if got := byID[25544]; got == nil || got.Name != "ISS (ZARYA)" {
		t.Fatalf("ISS name = %+v, want %q without 3le prefix", got, "ISS (ZARYA)")
	}
	if byID[40069] == nil || byID[40069].Line1 != meteor.Line1 {
		t.Fatalf("METEOR-M2 line1 mismatch")
	}
	if logins, _ := fake.stats(); logins != 1 {
		t.Fatalf("logins = %d, want 1", logins)
	}
}

func TestSpaceTrack_FetchByNoradIDs_SplitsIntoBatches(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t,
		makeTestTLE(t, 10001, "A", "26316.50000000"),
		makeTestTLE(t, 10003, "C", "26316.50000000"),
		makeTestTLE(t, 10005, "E", "26316.50000000"),
	)

	ids := []int{10001, 10002, 10003, 10004, 10005}
	tles, err := newTestSpaceTrackClient(srv.URL, WithSpaceTrackBatchSize(2)).
		FetchByNoradIDs(context.Background(), ids)
	if err != nil {
		t.Fatalf("FetchByNoradIDs: %v", err)
	}
	if len(tles) != 3 {
		t.Fatalf("got %d TLEs, want 3", len(tles))
	}
	logins, queries := fake.stats()
	if logins != 1 {
		t.Fatalf("logins = %d, want 1 per pass", logins)
	}
	want := [][]int{{10001, 10002}, {10003, 10004}, {10005}}
	if fmt.Sprint(queries) != fmt.Sprint(want) {
		t.Fatalf("queries = %v, want %v", queries, want)
	}
}

func TestSpaceTrack_FetchByNoradIDsProgress_ReportsEachBatch(t *testing.T) {
	_, srv := newFakeSpaceTrack(t,
		makeTestTLE(t, 10001, "A", "26316.50000000"),
		makeTestTLE(t, 10003, "C", "26316.50000000"),
		makeTestTLE(t, 10005, "E", "26316.50000000"),
	)
	var got [][2]int
	_, err := newTestSpaceTrackClient(srv.URL, WithSpaceTrackBatchSize(2)).
		FetchByNoradIDsProgress(context.Background(), []int{10001, 10002, 10003, 10004, 10005},
			func(done, total int) { got = append(got, [2]int{done, total}) })
	if err != nil {
		t.Fatalf("FetchByNoradIDsProgress: %v", err)
	}
	want := [][2]int{{1, 3}, {2, 3}, {3, 3}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("progress = %v, want %v", got, want)
	}
}

func TestSpaceTrack_FetchByNoradIDs_UnknownBatchIsEmptyNotError(t *testing.T) {
	_, srv := newFakeSpaceTrack(t, makeTestTLE(t, 10001, "A", "26316.50000000"))

	tles, err := newTestSpaceTrackClient(srv.URL, WithSpaceTrackBatchSize(2)).
		FetchByNoradIDs(context.Background(), []int{10001, 10002, 99001, 99002})
	if err != nil {
		t.Fatalf("batch with only unknown NORAD must not fail the pass: %v", err)
	}
	if len(tles) != 1 || tles[0].NoradID != 10001 {
		t.Fatalf("got %v, want only NORAD 10001", tles)
	}
}

func TestSpaceTrack_FetchByNoradIDs_LoginFailedSkipsQuery(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t, makeTestTLE(t, 25544, "ISS", "26316.50000000"))
	fake.rejectLogin = true

	_, err := newTestSpaceTrackClient(srv.URL).FetchByNoradIDs(context.Background(), []int{25544})
	if !errors.Is(err, ErrSpaceTrackLogin) {
		t.Fatalf("err = %v, want ErrSpaceTrackLogin", err)
	}
	if strings.Contains(err.Error(), testSTPassword) {
		t.Fatalf("error text leaks password: %v", err)
	}
	if _, queries := fake.stats(); len(queries) != 0 {
		t.Fatalf("queries after failed login = %v, want none", queries)
	}
}

func TestSpaceTrack_FetchByNoradIDs_ReloginOnExpiredSession(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t, makeTestTLE(t, 25544, "ISS", "26316.50000000"))
	client := newTestSpaceTrackClient(srv.URL)
	if _, err := client.FetchByNoradIDs(context.Background(), []int{25544}); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	fake.mu.Lock()
	fake.expireNext = true
	fake.mu.Unlock()

	tles, err := client.FetchByNoradIDs(context.Background(), []int{25544})
	if err != nil {
		t.Fatalf("fetch after expiry: %v", err)
	}
	if len(tles) != 1 {
		t.Fatalf("got %d TLEs, want 1", len(tles))
	}
	if logins, _ := fake.stats(); logins != 2 {
		t.Fatalf("logins = %d, want 2 (initial + relogin)", logins)
	}
}

func TestSpaceTrack_FetchByNoradIDs_ReusesSession(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t, makeTestTLE(t, 25544, "ISS", "26316.50000000"))
	client := newTestSpaceTrackClient(srv.URL)
	for range 2 {
		if _, err := client.FetchByNoradIDs(context.Background(), []int{25544}); err != nil {
			t.Fatalf("FetchByNoradIDs: %v", err)
		}
	}
	if logins, _ := fake.stats(); logins != 1 {
		t.Fatalf("logins = %d, want 1", logins)
	}
}

func TestSpaceTrack_FetchByNoradIDs_EmptyIDsMakesNoRequests(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t)
	tles, err := newTestSpaceTrackClient(srv.URL).FetchByNoradIDs(context.Background(), nil)
	if err != nil || len(tles) != 0 {
		t.Fatalf("got %d TLEs, err %v; want empty, nil", len(tles), err)
	}
	if logins, _ := fake.stats(); logins != 0 {
		t.Fatalf("logins = %d, want 0", logins)
	}
}

func TestSpaceTrack_FetchByNoradIDs_ServerErrorFailsPass(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t, makeTestTLE(t, 25544, "ISS", "26316.50000000"))
	fake.failStatus = http.StatusInternalServerError

	_, err := newTestSpaceTrackClient(srv.URL).FetchByNoradIDs(context.Background(), []int{25544})
	if !errors.Is(err, ErrSpaceTrackStatus) {
		t.Fatalf("err = %v, want ErrSpaceTrackStatus", err)
	}
}
