package freqplan

import (
	"strings"
	"testing"
	"time"
)

// haFetchedAt is the download time of haSample: the page has no year in its dates.
var haFetchedAt = time.Date(2026, time.November, 12, 10, 0, 0, 0, time.UTC)

// haRow builds a pass row in the markup of heavens-above.com/AmateurSats.aspx.
func haRow(norad, name, freq string) string {
	return haPassRow(norad, name, "12 Nov", "14:30:01", "14:37:58", "45", freq)
}

func haPassRow(norad, name, date, start, end, maxEl, freq string) string {
	return `<tr><td><a href="satinfo.aspx?satid=` + norad + `&lat=47.3&lng=39.7&cul=en">` + name +
		`</a></td><td>` + date + `</td><td>` + start + `</td><td align="center">` + maxEl + `°</td>` +
		`<td align="center">71° (ENE)</td><td>` + end + `</td><td>` + freq + `</td></tr>`
}

func haPage(rows ...string) string {
	return `<table class="standardTable">` + strings.Join(rows, "\n") + `</table>`
}

var haSample = `<html><body>
<table class="userTable"><tr><td>Location</td></tr></table>
<table class="standardTable">
<tr><th rowspan="2">Satellite</th><th>Date</th><th>Start</th><th colspan="2">Highest point</th><th>End</th><th>Downlink Frequencies (MHz)</th></tr>
<tr><th>Altitude</th><th>Azimuth</th></tr>
` + haRow("25544", "ISS", "437.800") +
	haRow("25544", "ISS", "437.800") +
	haRow("7530", "OSCAR 7", "145.950 USB") +
	haRow("20442", "OSCAR 19 (LUSAT)", "437.125/150") +
	haRow("40069", "METEOR M2", "137.100 / 137.900") +
	haRow("23439", "RADIO ROSTO", "29.354- 29.394") +
	haRow("39446", "UWE-3", "435.000/436.395/437.385") +
	haRow("40074", "UKUBE 1", ".") +
	haRow("27939", "MOZHAYETS 4", "") +
	haRow("99999", "A &amp; B", "435.100") + `
</table></body></html>`

func parseHASample(t *testing.T) *HeavensAbove {
	t.Helper()
	src, err := ParseHeavensAbove(strings.NewReader(haSample), haFetchedAt)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func freqsOf(txs []Tx) []int64 {
	out := make([]int64, len(txs))
	for i, tx := range txs {
		out[i] = tx.FreqHz
	}
	return out
}

func TestParseHeavensAbove_SingleFrequencyOncePerSatellite(t *testing.T) {
	iss := parseHASample(t).Transmitters(25544)

	if len(iss) != 1 || iss[0].FreqHz != 437_800_000 {
		t.Fatalf("ISS = %+v, want one 437.800", iss)
	}
	if iss[0].Source != SourceHeavensAbove || iss[0].Name != "ISS" {
		t.Fatalf("ISS source/name = %q/%q", iss[0].Source, iss[0].Name)
	}
}

func TestParseHeavensAbove_FrequencyFormats(t *testing.T) {
	src := parseHASample(t)
	cases := []struct {
		name  string
		norad int
		want  []int64
	}{
		{"mode suffix", 7530, []int64{145_950_000}},
		{"short second value", 20442, []int64{437_125_000, 437_150_000}},
		{"slash with spaces", 40069, []int64{137_100_000, 137_900_000}},
		{"range midpoint", 23439, []int64{29_374_000}},
		{"three values", 39446, []int64{435_000_000, 436_395_000, 437_385_000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := freqsOf(src.Transmitters(tc.norad))
			if len(got) != len(tc.want) {
				t.Fatalf("freqs = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("freqs = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestParseHeavensAbove_ModeFromSuffix(t *testing.T) {
	oscar7 := parseHASample(t).Transmitters(7530)

	if len(oscar7) != 1 || oscar7[0].Mode != "USB" {
		t.Fatalf("OSCAR 7 = %+v, want mode USB", oscar7)
	}
}

func TestParseHeavensAbove_EmptyFrequencySkipped(t *testing.T) {
	src := parseHASample(t)

	for _, norad := range []int{40074, 27939} {
		if txs := src.Transmitters(norad); len(txs) != 0 {
			t.Fatalf("norad %d = %+v, want none", norad, txs)
		}
	}
}

func TestParseHeavensAbove_NameUnescaped(t *testing.T) {
	txs := parseHASample(t).Transmitters(99999)

	if len(txs) != 1 || txs[0].Name != "A & B" {
		t.Fatalf("txs = %+v, want name A & B", txs)
	}
}

func TestParseHeavensAbove_NoTableIsError(t *testing.T) {
	if _, err := ParseHeavensAbove(strings.NewReader("<html>maintenance</html>"), haFetchedAt); err == nil {
		t.Fatal("want error when pass table is missing")
	}
}

func TestParseHeavensAbove_PassesKeepEveryRow(t *testing.T) {
	src := parseHASample(t)

	passes := src.Passes()
	if len(passes) != 10 {
		t.Fatalf("len(passes) = %d, want 10 (one per row)", len(passes))
	}
	iss := passes[0]
	wantStart := time.Date(2026, time.November, 12, 14, 30, 1, 0, time.UTC)
	wantEnd := time.Date(2026, time.November, 12, 14, 37, 58, 0, time.UTC)
	if iss.NoradID != 25544 || !iss.Start.Equal(wantStart) || !iss.End.Equal(wantEnd) {
		t.Fatalf("first pass = %+v, want ISS %v..%v", iss, wantStart, wantEnd)
	}
	if iss.MaxElDeg != 45 {
		t.Fatalf("MaxElDeg = %v, want 45", iss.MaxElDeg)
	}
}

func TestParseHeavensAbove_PassOverMidnightEndsNextDay(t *testing.T) {
	page := haPage(haPassRow("25544", "ISS", "12 Nov", "23:58:00", "00:05:30", "30", "437.800"))

	src, err := ParseHeavensAbove(strings.NewReader(page), haFetchedAt)
	if err != nil {
		t.Fatal(err)
	}

	p := src.Passes()[0]
	if want := time.Date(2026, time.November, 13, 0, 5, 30, 0, time.UTC); !p.End.Equal(want) {
		t.Fatalf("End = %v, want %v", p.End, want)
	}
}

func TestParseHeavensAbove_YearFollowsFetchTime(t *testing.T) {
	page := haPage(haPassRow("25544", "ISS", "1 Jan", "01:00:00", "01:08:00", "30", "437.800"))
	fetched := time.Date(2026, time.December, 31, 20, 0, 0, 0, time.UTC)

	src, err := ParseHeavensAbove(strings.NewReader(page), fetched)
	if err != nil {
		t.Fatal(err)
	}

	if got := src.Passes()[0].Start.Year(); got != 2027 {
		t.Fatalf("year = %d, want 2027 for a January pass fetched on Dec 31", got)
	}
}

func TestParseHeavensAbove_RowWithBadTimeHasNoPass(t *testing.T) {
	page := haPage(haPassRow("25544", "ISS", "12 Nov", "soon", "14:37:58", "45", "437.800"))

	src, err := ParseHeavensAbove(strings.NewReader(page), haFetchedAt)
	if err != nil {
		t.Fatal(err)
	}

	if len(src.Passes()) != 0 {
		t.Fatalf("passes = %+v, want none", src.Passes())
	}
	if len(src.Transmitters(25544)) != 1 {
		t.Fatal("frequency must survive a bad time cell")
	}
}

func TestParseHeavensAbove_LocationFromLinks(t *testing.T) {
	cases := []struct {
		name string
		page string
	}{
		{"raw ampersand", haSample},
		{"escaped ampersand", strings.NewReplacer("&lat=", "&amp;lat=", "&lng=", "&amp;lng=").Replace(haSample)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, err := ParseHeavensAbove(strings.NewReader(tc.page), haFetchedAt)
			if err != nil {
				t.Fatal(err)
			}

			lat, lon, ok := src.Location()
			if !ok || lat != 47.3 || lon != 39.7 {
				t.Fatalf("Location = %v, %v, %v; want 47.3, 39.7, true", lat, lon, ok)
			}
		})
	}
}
