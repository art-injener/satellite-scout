package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// querySatcat имитирует class/satcat с predicates NORAD_CAT_ID,DECAY: NORAD строкой, DECAY null или дата.
func (f *fakeSpaceTrack) querySatcat(w http.ResponseWriter, r *http.Request) {
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
	f.satcatHits++
	if f.satcatFail != 0 && (f.satcatFailFrom == 0 || f.satcatHits >= f.satcatFailFrom) {
		w.WriteHeader(f.satcatFail)
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
	f.satcatQueries = append(f.satcatQueries, ids)
	type row struct {
		NoradCatID string  `json:"NORAD_CAT_ID"`
		Decay      *string `json:"DECAY"`
	}
	var rows []row
	for _, id := range ids {
		decay, ok := f.satcat[id]
		if !ok {
			continue
		}
		r := row{NoradCatID: strconv.Itoa(id)}
		if decay != "" {
			r.Decay = &decay
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}

func (f *fakeSpaceTrack) satcatStats() [][]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.satcatQueries)
}

func TestSpaceTrack_FetchSatcat_ParsesDecay(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t)
	fake.satcat[25544] = ""
	fake.satcat[2012] = "2024-05-12"

	recs, n, err := newTestSpaceTrackClient(srv.URL).FetchSatcat(context.Background(), []int{2012, 25544, 99001})
	if err != nil {
		t.Fatalf("FetchSatcat: %v", err)
	}
	if n <= 0 {
		t.Fatalf("response bytes = %d, want > 0", n)
	}
	byID := make(map[int]SatcatRecord, len(recs))
	for _, r := range recs {
		byID[r.NoradID] = r
	}
	if len(byID) != 2 {
		t.Fatalf("records = %v, want 2 (unknown NORAD is absent)", recs)
	}
	if !byID[25544].Decay.IsZero() {
		t.Fatalf("ISS decay = %v, want zero", byID[25544].Decay)
	}
	want := time.Date(2024, 5, 12, 0, 0, 0, 0, time.UTC)
	if !byID[2012].Decay.Equal(want) {
		t.Fatalf("2012 decay = %v, want %v", byID[2012].Decay, want)
	}
}

func TestSpaceTrack_FetchSatcat_SharesSessionWithGP(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t, makeTestTLE(t, 25544, "ISS", "26316.50000000"))
	fake.satcat[25544] = ""
	client := newTestSpaceTrackClient(srv.URL, WithSpaceTrackBatchSize(2))

	ids := []int{25544, 10002, 10003}
	if _, err := client.FetchByNoradIDs(context.Background(), ids); err != nil {
		t.Fatalf("FetchByNoradIDs: %v", err)
	}
	if _, _, err := client.FetchSatcat(context.Background(), ids); err != nil {
		t.Fatalf("FetchSatcat: %v", err)
	}
	if logins, _ := fake.stats(); logins != 1 {
		t.Fatalf("logins = %d, want 1 for GP and SATCAT in one pass", logins)
	}
	want := [][]int{{25544, 10002}, {10003}}
	if got := fake.satcatStats(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("satcat queries = %v, want %v", got, want)
	}
}

func TestSpaceTrack_FetchSatcat_UnknownBatchIsEmptyNotError(t *testing.T) {
	_, srv := newFakeSpaceTrack(t)
	recs, _, err := newTestSpaceTrackClient(srv.URL).FetchSatcat(context.Background(), []int{99001, 99002})
	if err != nil || len(recs) != 0 {
		t.Fatalf("got %v, err %v; want empty, nil", recs, err)
	}
}

func TestSpaceTrack_FetchSatcat_ReloginOnExpiredSession(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t)
	fake.satcat[2012] = "2024-05-12"
	client := newTestSpaceTrackClient(srv.URL)
	if _, _, err := client.FetchSatcat(context.Background(), []int{2012}); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	fake.mu.Lock()
	fake.expireNext = true
	fake.mu.Unlock()

	recs, _, err := client.FetchSatcat(context.Background(), []int{2012})
	if err != nil || len(recs) != 1 {
		t.Fatalf("after expiry got %v, err %v; want 1 record", recs, err)
	}
	if logins, _ := fake.stats(); logins != 2 {
		t.Fatalf("logins = %d, want 2", logins)
	}
}

func TestSpaceTrack_FetchSatcat_KeepsEarlierBatches(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t)
	fake.satcat[2012] = "2024-05-12"
	fake.satcat[25544] = ""
	fake.satcat[40069] = ""
	fake.satcatFail = http.StatusInternalServerError
	fake.satcatFailFrom = 2

	recs, _, err := newTestSpaceTrackClient(srv.URL, WithSpaceTrackBatchSize(2)).
		FetchSatcat(context.Background(), []int{2012, 25544, 40069})
	if !errors.Is(err, ErrSpaceTrackStatus) {
		t.Fatalf("err = %v, want ErrSpaceTrackStatus", err)
	}
	if len(recs) != 2 || recs[0].NoradID != 2012 || recs[1].NoradID != 25544 {
		t.Fatalf("got %+v, want the first batch kept", recs)
	}
}

func TestSpaceTrack_FetchSatcat_ServerErrorFails(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t)
	fake.satcatFail = http.StatusInternalServerError

	_, _, err := newTestSpaceTrackClient(srv.URL).FetchSatcat(context.Background(), []int{25544})
	if !errors.Is(err, ErrSpaceTrackStatus) {
		t.Fatalf("err = %v, want ErrSpaceTrackStatus", err)
	}
}

func TestSpaceTrack_FetchSatcat_EmptyIDsMakesNoRequests(t *testing.T) {
	fake, srv := newFakeSpaceTrack(t)
	recs, n, err := newTestSpaceTrackClient(srv.URL).FetchSatcat(context.Background(), nil)
	if err != nil || len(recs) != 0 || n != 0 {
		t.Fatalf("got %v, %d bytes, err %v; want nothing", recs, n, err)
	}
	if logins, _ := fake.stats(); logins != 0 {
		t.Fatalf("logins = %d, want 0", logins)
	}
}
