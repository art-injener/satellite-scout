package freqplan

import (
	"slices"
	"testing"
)

func TestPrioritizeTargets_HeardFirst(t *testing.T) {
	in := []Target{
		{FreqHz: 145_800_000, NoradID: 1, Name: "A"},
		{FreqHz: 401_000_000, NoradID: 2, Name: "B"},
		{FreqHz: 436_800_000, NoradID: 3, Name: "C"},
	}
	heard := map[int]int64{2: 100, 3: 200}
	got := PrioritizeTargets(in, func(norad int, _ uint64) int64 { return heard[norad] })
	want := []Target{in[2], in[1], in[0]} // C (200), B (100), A
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPrioritizeTargets_StableWithoutHeard(t *testing.T) {
	in := []Target{
		{FreqHz: 1, NoradID: 1},
		{FreqHz: 2, NoradID: 2},
	}
	got := PrioritizeTargets(in, func(int, uint64) int64 { return 0 })
	if !slices.Equal(got, in) {
		t.Fatalf("got %+v", got)
	}
}
