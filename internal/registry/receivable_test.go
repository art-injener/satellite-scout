package registry

import (
	"context"
	"fmt"
	"testing"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

var rtlBands = Bands{{MinHz: 24_100_000, MaxHz: 1_766_000_000}}

// orbitTLE собирает элементы с заданным средним движением (оборотов в сутки) в колонках 53–63.
func orbitTLE(norad int, meanMotion float64) *tracker.TLE {
	return &tracker.TLE{
		NoradID: norad,
		Epoch:   testAt,
		Line2: fmt.Sprintf(
			"2 %05d  51.6400 247.4627 0006703 130.5360 325.0288 %11.8f%05d0", norad, meanMotion, 1),
	}
}

func TestRegistry_Receivable(t *testing.T) {
	r := New()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.PutOrbits(ctx, SourceSpaceTrack, []*tracker.TLE{
		orbitTLE(1, 15.5), orbitTLE(2, 15.5), orbitTLE(3, 15.5), orbitTLE(4, 1.0027), orbitTLE(5, 14.2),
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceAMSAT, []Transmitter{
		{NoradID: 1, DownlinkHz: 145_900_000},
		{NoradID: 4, DownlinkHz: 1_691_000_000},
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceHeavensAbove, []Transmitter{
		{NoradID: 2, DownlinkHz: 2_275_000_000},
		{NoradID: 7, DownlinkHz: 437_000_000},
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceSonik, []Transmitter{
		{NoradID: 3, UUID: "s3", DownlinkHz: 437_500_000, Status: "active"},
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceCurated, []Transmitter{
		{NoradID: 5, Name: "AO-7", DownlinkHz: 145_950_000},
	}, testAt))

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
		{"orbit not known yet", 7, rtlBands, true},
		{"unknown satellite", 8, rtlBands, false},
	}
	for _, c := range cases {
		if got := r.Receivable(c.norad, c.bands); got != c.want {
			t.Errorf("%s: Receivable(%d) = %v, want %v", c.name, c.norad, got, c.want)
		}
	}
}

func TestRegistry_Receivable_SatnogsDeadOverridesAMSAT(t *testing.T) {
	r := New()
	ctx := context.Background()
	_ = r.ReplaceTransmitters(ctx, SourceAMSAT, []Transmitter{{NoradID: 1, DownlinkHz: 145_900_000}}, testAt)
	_ = r.ReplaceTransmittersByNorad(ctx, SourceSatNOGS, map[int][]Transmitter{
		1: {{DownlinkHz: 145_900_000, Status: "active", Alive: new(false)}},
	}, testAt)

	if r.Receivable(1, rtlBands) {
		t.Fatal("satellite dead by SatNOGS must not be receivable")
	}
}

func TestRegistry_Receivable_SatnogsAliveFlagAloneIsNotEnough(t *testing.T) {
	r := New()
	_ = r.ReplaceTransmittersByNorad(context.Background(), SourceSatNOGS, map[int][]Transmitter{
		1: {{DownlinkHz: 136_232_000, Status: "active", Alive: new(true)}},
	}, testAt)

	if r.Receivable(1, rtlBands) {
		t.Fatal("SatNOGS alive flag is set by editors, it must not confirm liveness alone")
	}
}

func TestRegistry_KnowsLiveness(t *testing.T) {
	r := New()
	ctx := context.Background()
	if r.KnowsLiveness() {
		t.Fatal("empty registry knows nothing")
	}
	_ = r.ReplaceTransmitters(ctx, SourceCurated, []Transmitter{{NoradID: 1, DownlinkHz: 145_900_000}}, testAt)
	_ = r.ReplaceTransmitters(ctx, SourceSonik, []Transmitter{{NoradID: 2, DownlinkHz: 437_000_000}}, testAt)
	_ = r.ReplaceTransmittersByNorad(ctx, SourceSatNOGS, map[int][]Transmitter{
		4: {{DownlinkHz: 437_100_000, Status: "active", Alive: new(true)}},
	}, testAt)
	if r.KnowsLiveness() {
		t.Fatal("curated, sonik and SatNOGS do not confirm that a satellite is alive")
	}
	_ = r.ReplaceTransmitters(ctx, SourceAMSAT, []Transmitter{{NoradID: 3, DownlinkHz: 145_800_000}}, testAt)
	if !r.KnowsLiveness() {
		t.Fatal("AMSAT snapshot tells liveness")
	}
}

func TestBands_Allow(t *testing.T) {
	if !Bands(nil).Allow(10_000_000_000) {
		t.Error("empty bands must allow any frequency")
	}
	for hz, want := range map[int64]bool{24_100_000: true, 437_000_000: true, 1_766_000_000: true, 2_275_000_000: false, 0: false} {
		if got := rtlBands.Allow(hz); got != want {
			t.Errorf("Allow(%d) = %v, want %v", hz, got, want)
		}
	}
}
