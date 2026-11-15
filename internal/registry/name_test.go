package registry

import (
	"context"
	"testing"
)

func TestRegistry_DisplayName(t *testing.T) {
	r := New()
	ctx := context.Background()
	_ = r.ReplaceSatFacts(ctx, SourceSonik, []SatFact{
		{NoradID: 44881, Name: "CAS-6 (TO-108)"},
		{NoradID: 40905, Name: "OBJECT G"},
		{NoradID: 25544, Name: "ISS"},
	}, testAt)
	_ = r.ReplaceTransmitters(ctx, SourceCurated, []Transmitter{
		{NoradID: 57166, Name: "METEOR-M2-3", DownlinkHz: 137_900_000},
	}, testAt)

	cases := []struct {
		name    string
		norad   int
		tleName string
		want    string
	}{
		{"placeholder takes the sonik name", 44881, "OBJECT C", "CAS-6 (TO-108)"},
		{"placeholder takes the curated name", 57166, "OBJECT A", "METEOR-M2-3"},
		{"to be assigned is a placeholder", 44881, "TBA - TO BE ASSIGNED", "CAS-6 (TO-108)"},
		{"empty is a placeholder", 44881, "", "CAS-6 (TO-108)"},
		{"real TLE name stays", 25544, "ISS (ZARYA)", "ISS (ZARYA)"},
		{"sonik placeholder changes nothing", 40905, "OBJECT G", "OBJECT G"},
		{"unknown satellite keeps the TLE name", 99999, "OBJECT B", "OBJECT B"},
	}
	for _, c := range cases {
		if got := r.DisplayName(c.norad, c.tleName); got != c.want {
			t.Errorf("%s: DisplayName(%d, %q) = %q, want %q", c.name, c.norad, c.tleName, got, c.want)
		}
	}
}
