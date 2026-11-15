package registry

import (
	"context"
	"testing"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

func TestNormalizeGroupLabel_Default(t *testing.T) {
	if got := NormalizeGroupLabel(""); got != LabelInBandAlive {
		t.Fatalf("empty = %q, want %q", got, LabelInBandAlive)
	}
	if got := NormalizeGroupLabel(LabelWeather); got != LabelWeather {
		t.Fatalf("weather = %q", got)
	}
}

func TestValidGroupLabel(t *testing.T) {
	for _, label := range []string{"", LabelInBandAlive, LabelInBand, LabelAlive, LabelHeard, LabelStations, LabelWeather} {
		if !ValidGroupLabel(label) {
			t.Errorf("%q must be valid", label)
		}
	}
	if ValidGroupLabel("cubesat") {
		t.Fatal("unknown label must be rejected")
	}
}

func TestRegistry_HasLabel(t *testing.T) {
	r := New()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.ReplaceTransmitters(ctx, SourceAMSAT, []Transmitter{
		{NoradID: 1, DownlinkHz: 145_900_000},
		{NoradID: 4, DownlinkHz: 1_691_000_000},
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceHeavensAbove, []Transmitter{
		{NoradID: 2, DownlinkHz: 2_275_000_000},
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceSonik, []Transmitter{
		{NoradID: 3, DownlinkHz: 437_500_000, Status: "active", Mode: "FM"},
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceCurated, []Transmitter{
		{NoradID: 5, Name: "AO-7", DownlinkHz: 145_950_000},
		{NoradID: 25544, Name: "ISS", DownlinkHz: 145_800_000, Mode: "FM"},
		{NoradID: 59051, Name: "METEOR-M2-4", DownlinkHz: 137_900_000, Mode: "LRPT"},
	}, testAt))

	cases := []struct {
		name  string
		norad int
		label string
		bands Bands
		want  bool
	}{
		{"in band", 1, LabelInBand, rtlBands, true},
		{"alive but out of band", 2, LabelInBand, rtlBands, false},
		{"no radio paths: in band if any TX", 2, LabelInBand, nil, true},
		{"alive by AMSAT", 1, LabelAlive, rtlBands, true},
		{"sonik only not alive", 3, LabelAlive, rtlBands, false},
		{"curated counts as alive for the operator", 5, LabelAlive, rtlBands, true},
		{"heard without visits is false", 1, LabelHeard, rtlBands, false},
		{"ISS is a station", 25544, LabelStations, rtlBands, true},
		{"AO-7 is not a station", 5, LabelStations, rtlBands, false},
		{"LRPT near 137 is weather", 59051, LabelWeather, rtlBands, true},
		{"FM voice is not weather", 1, LabelWeather, rtlBands, false},
	}
	for _, c := range cases {
		if got := r.HasLabel(c.norad, c.label, c.bands); got != c.want {
			t.Errorf("%s: HasLabel(%d, %s) = %v, want %v", c.name, c.norad, c.label, got, c.want)
		}
	}
}

func TestRegistry_MatchGroupLabel_Default(t *testing.T) {
	r := New()
	ctx := context.Background()
	_ = r.PutOrbits(ctx, SourceSpaceTrack, []*tracker.TLE{
		orbitTLE(1, 15.5), orbitTLE(2, 15.5), orbitTLE(3, 15.5), orbitTLE(4, 1.0027), orbitTLE(5, 14.2),
	}, testAt)
	_ = r.ReplaceTransmitters(ctx, SourceAMSAT, []Transmitter{
		{NoradID: 1, DownlinkHz: 145_900_000},
		{NoradID: 4, DownlinkHz: 1_691_000_000},
	}, testAt)
	_ = r.ReplaceTransmitters(ctx, SourceHeavensAbove, []Transmitter{
		{NoradID: 2, DownlinkHz: 2_275_000_000},
	}, testAt)
	_ = r.ReplaceTransmitters(ctx, SourceSonik, []Transmitter{
		{NoradID: 3, DownlinkHz: 437_500_000, Status: "active"},
	}, testAt)
	_ = r.ReplaceTransmitters(ctx, SourceCurated, []Transmitter{
		{NoradID: 5, Name: "AO-7", DownlinkHz: 145_950_000},
	}, testAt)

	cases := []struct {
		name  string
		norad int
		bands Bands
		want  bool
	}{
		{"alive LEO in band", 1, rtlBands, true},
		{"alive but only out of band", 2, rtlBands, false},
		{"no radio paths skip the band check", 2, nil, true},
		{"sonik only is not alive", 3, rtlBands, false},
		{"geostationary", 4, rtlBands, false},
		{"curated is the operator's choice", 5, rtlBands, true},
		{"unknown satellite", 8, rtlBands, false},
	}
	for _, c := range cases {
		if got := r.MatchGroupLabel(c.norad, LabelInBandAlive, c.bands); got != c.want {
			t.Errorf("%s: MatchGroupLabel(%d) = %v, want %v", c.name, c.norad, got, c.want)
		}
	}
}

func TestRegistry_MatchGroupLabel_SkipsAliveUntilLivenessKnown(t *testing.T) {
	r := New()
	_ = r.ReplaceTransmitters(context.Background(), SourceSonik, []Transmitter{
		{NoradID: 3, DownlinkHz: 437_500_000, Status: "active"},
	}, testAt)
	if r.KnowsLiveness() {
		t.Fatal("sonik alone must not confirm liveness")
	}
	if !r.MatchGroupLabel(3, LabelInBandAlive, rtlBands) {
		t.Fatal("before liveness sources arrive, in-band TX must stay in the group")
	}
}

func TestRegistry_MatchGroupLabel_Stations(t *testing.T) {
	r := New()
	_ = r.ReplaceTransmitters(context.Background(), SourceCurated, []Transmitter{
		{NoradID: 25544, Name: "ISS", DownlinkHz: 145_800_000},
		{NoradID: 5, Name: "AO-7", DownlinkHz: 145_950_000},
	}, testAt)
	if !r.MatchGroupLabel(25544, LabelStations, rtlBands) {
		t.Fatal("ISS must match stations label")
	}
	if r.MatchGroupLabel(5, LabelStations, rtlBands) {
		t.Fatal("AO-7 must not match stations label")
	}
}

func TestSortPreferringBand(t *testing.T) {
	ant := Bands{{MinHz: 430_000_000, MaxHz: 450_000_000}}
	txs := []Transmitter{
		{NoradID: 1, DownlinkHz: 145_800_000, Name: "VHF"},
		{NoradID: 1, DownlinkHz: 437_800_000, Name: "UHF"},
		{NoradID: 1, DownlinkHz: 137_100_000, Name: "WX"},
	}
	got := SortPreferringBand(txs, ant)
	if got[0].Name != "UHF" {
		t.Fatalf("first = %s, want UHF inside antenna band", got[0].Name)
	}
	if got[1].Name != "VHF" || got[2].Name != "WX" {
		t.Fatalf("stable order outside band broken: %+v", got)
	}
	if SortPreferringBand(txs, nil)[0].Name != "VHF" {
		t.Fatal("empty antenna bands must keep original order")
	}
}
