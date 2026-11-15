package main

import (
	"strings"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
)

func haTestPage(t *testing.T, rows ...string) *freqplan.HeavensAbove {
	t.Helper()
	page := `<table class="standardTable">` + strings.Join(rows, "") + `</table>`
	fetched := time.Date(2026, time.November, 12, 10, 0, 0, 0, time.UTC)
	ha, err := freqplan.ParseHeavensAbove(strings.NewReader(page), fetched)
	if err != nil {
		t.Fatal(err)
	}
	return ha
}

func haTestRow(norad, start, end string) string {
	return `<tr><td><a href="satinfo.aspx?satid=` + norad + `&lat=47.2357&lng=39.7015">SAT</a></td>` +
		`<td>12 Nov</td><td>` + start + `</td><td>40°</td><td>90° (E)</td><td>` + end + `</td><td>437.800</td></tr>`
}

func TestHARefPasses_KeepsOnlyFuturePassesInHorizon(t *testing.T) {
	ha := haTestPage(t,
		haTestRow("1", "09:00:00", "09:10:00"),
		haTestRow("2", "12:00:00", "12:10:00"),
		haTestRow("3", "23:00:00", "23:10:00"),
	)
	from := time.Date(2026, time.November, 12, 11, 0, 0, 0, time.UTC)

	got := haRefPasses(ha, 47.2357, 39.7015, from, from.Add(6*time.Hour))

	if len(got) != 1 || got[0].NoradID != 2 {
		t.Fatalf("got %+v, want only NORAD 2", got)
	}
}

func TestHARefPasses_OtherLocationIsSkipped(t *testing.T) {
	ha := haTestPage(t, haTestRow("2", "12:00:00", "12:10:00"))
	from := time.Date(2026, time.November, 12, 11, 0, 0, 0, time.UTC)

	// Станцию перенесли, а кеш страницы ещё посчитан для старых координат.
	got := haRefPasses(ha, 55.75, 37.62, from, from.Add(6*time.Hour))

	if len(got) != 0 {
		t.Fatalf("got %+v, want none for another location", got)
	}
}

func TestHARefPasses_NoPageOffline(t *testing.T) {
	from := time.Now()

	if got := haRefPasses(nil, 47.2357, 39.7015, from, from.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("got %+v, want none without page", got)
	}
}
