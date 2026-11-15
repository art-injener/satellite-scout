package registry

import (
	"context"
	"fmt"
	"testing"
)

func TestRegistry_CatalogCompositionIDs_CuratedHAAMSATOnly(t *testing.T) {
	r := New()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.ReplaceTransmitters(ctx, SourceCurated, []Transmitter{
		{NoradID: 57166, DownlinkHz: 137_900_000},
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceHeavensAbove, []Transmitter{
		{NoradID: 25544, DownlinkHz: 145_800_000},
		{NoradID: 43017, DownlinkHz: 145_960_000},
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceAMSAT, []Transmitter{
		{NoradID: 43017, DownlinkHz: 145_960_000},
		{NoradID: 40069, DownlinkHz: 137_100_000},
	}, testAt))
	must(r.ReplaceTransmitters(ctx, SourceSonik, []Transmitter{
		{NoradID: 99999, DownlinkHz: 437_000_000},
	}, testAt))
	must(r.ReplaceTransmittersByNorad(ctx, SourceSatNOGS, map[int][]Transmitter{
		88888: {{NoradID: 88888, DownlinkHz: 437_500_000}},
	}, testAt))

	got := r.CatalogCompositionIDs()
	want := []int{25544, 40069, 43017, 57166}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("CatalogCompositionIDs = %v, want %v", got, want)
	}
}
