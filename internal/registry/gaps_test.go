package registry

import (
	"context"
	"testing"
	"time"
)

func TestRegistry_FilterSatNOGSGaps(t *testing.T) {
	r := New()
	ctx := context.Background()
	at := time.Now()

	_ = r.ReplaceTransmitters(ctx, SourceCurated, []Transmitter{
		{NoradID: 25544, DownlinkHz: 145_800_000},
	}, at)
	_ = r.ReplaceTransmitters(ctx, SourceSonik, []Transmitter{
		{NoradID: 57166, DownlinkHz: 137_900_000},
	}, at)
	_ = r.ReplaceTransmitters(ctx, SourceAMSAT, []Transmitter{
		{NoradID: 7530, DownlinkHz: 435_795_000},
	}, at)
	_ = r.ReplaceTransmittersByNorad(ctx, SourceSatNOGS, map[int][]Transmitter{
		99999: {{NoradID: 99999, DownlinkHz: 437_000_000}},
	}, at)

	got := r.FilterSatNOGSGaps([]int{25544, 57166, 7530, 40069, 99999, 0, -1})
	want := []int{40069, 99999}
	if len(got) != len(want) {
		t.Fatalf("FilterSatNOGSGaps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FilterSatNOGSGaps = %v, want %v", got, want)
		}
	}
}

func TestRegistry_FilterSatNOGSGaps_Empty(t *testing.T) {
	r := New()
	if got := r.FilterSatNOGSGaps(nil); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}
