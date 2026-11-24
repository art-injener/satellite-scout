package main

import (
	"strings"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/bandscan"
	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/signalclass"
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

func TestAirHopWorthy_SkipsCarrierAndKeepsPulse(t *testing.T) {
	pulse := bandscan.Signal{
		CenterHz: 435_844_000, SNRDB: 12, Emission: bandscan.EmissionPulsed, Origin: "unknown",
	}
	if !airHopWorthy(pulse) {
		t.Fatal("pulsed unknown must be worthy")
	}
	carrier := bandscan.Signal{
		CenterHz: 436_224_000, SNRDB: 14, Emission: bandscan.EmissionContinuous,
		Origin:    bandscan.OriginTerrestrial,
		Structure: &signalclass.Result{Modulation: signalclass.ModCarrier},
	}
	if airHopWorthy(carrier) {
		t.Fatal("continuous terrestrial carrier must not hop")
	}
	weak := bandscan.Signal{CenterHz: 435_000_000, SNRDB: 5, Emission: bandscan.EmissionPulsed}
	if airHopWorthy(weak) {
		t.Fatal("SNR below MinSNRDB must not hop")
	}
}

func TestAirHint_KeepsFrameDemodProfiles(t *testing.T) {
	svc := bandscan.NewService(nil, nil)
	// Seed a remembered signal via direct service helper if available, or test through app.AirHint.
	_ = svc
	gotDemod, gotAlt := airHintDemods(&signalclass.Result{
		Modulation: signalclass.Mod2FSK,
		Params:     signalclass.Params{Baud: 9600},
		Demod:      "fsk9600_g3ruh",
		DemodAlt:   []string{"fsk9600_geoscan", "gmsk9600_usp"},
	})
	if gotDemod != "fsk9600_g3ruh" {
		t.Fatalf("demod = %q, want fsk9600_g3ruh (must not rewrite to fsk9600)", gotDemod)
	}
	if len(gotAlt) != 2 || gotAlt[0] != "fsk9600_geoscan" || gotAlt[1] != "gmsk9600_usp" {
		t.Fatalf("alt = %v, want [fsk9600_geoscan gmsk9600_usp]", gotAlt)
	}
}
