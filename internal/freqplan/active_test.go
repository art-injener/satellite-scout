package freqplan

import (
	"slices"
	"testing"
)

func TestActiveFreqs_VisibleNowAndRisingBeforeUntil(t *testing.T) {
	const now, until int64 = 1_000_000, 1_180_000 // горизонт ~3 мин
	plan := []Entry{
		{Name: "up-now", FreqHz: 437_000_000, AOS: 900_000, LOS: 1_100_000},
		{Name: "rising", FreqHz: 401_000_000, AOS: 1_050_000, LOS: 1_200_000},
		{Name: "too-late", FreqHz: 145_900_000, AOS: 1_200_000, LOS: 1_300_000},
		{Name: "already-set", FreqHz: 436_000_000, AOS: 800_000, LOS: 950_000},
	}
	got := ActiveFreqs(plan, now, until, nil)
	want := []uint64{401_000_000, 437_000_000}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestActiveFreqs_AllowFiltersBand(t *testing.T) {
	plan := []Entry{
		{FreqHz: 437_000_000, AOS: 1, LOS: 100},
		{FreqHz: 145_900_000, AOS: 1, LOS: 100},
	}
	allow := func(hz int64) bool { return hz >= 400_000_000 }
	got := ActiveFreqs(plan, 10, 50, allow)
	if want := []uint64{437_000_000}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestActiveFreqs_DedupsAndSkipsNonPositive(t *testing.T) {
	plan := []Entry{
		{FreqHz: 437_000_000, AOS: 1, LOS: 100},
		{FreqHz: 437_000_000, AOS: 1, LOS: 100},
		{FreqHz: 0, AOS: 1, LOS: 100},
	}
	got := ActiveFreqs(plan, 10, 50, nil)
	if want := []uint64{437_000_000}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestActiveFreqs_NoPassWindowKept(t *testing.T) {
	// Как у Judge: без AOS/LOS частота всё равно учитывается.
	plan := []Entry{{FreqHz: 401_000_000}}
	got := ActiveFreqs(plan, 10, 50, nil)
	if want := []uint64{401_000_000}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestActiveTargets_SortedUniqueWithNames(t *testing.T) {
	plan := []Entry{
		{NoradID: 25544, Name: "ISS", FreqHz: 437_800_000, AOS: 0, LOS: 100},
		{NoradID: 27607, Name: "SO-50", FreqHz: 436_795_000, AOS: 0, LOS: 100},
		{NoradID: 25544, Name: "ISS-vhf", FreqHz: 145_800_000, AOS: 0, LOS: 100},
		{NoradID: 1, Name: "dup", FreqHz: 437_800_000, AOS: 0, LOS: 100},
		{NoradID: 2, Name: "out", FreqHz: 401_000_000, AOS: 200, LOS: 300},
	}
	got := ActiveTargets(plan, 10, 50, nil)
	want := []Target{
		{FreqHz: 145_800_000, Name: "ISS-vhf", NoradID: 25544},
		{FreqHz: 436_795_000, Name: "SO-50", NoradID: 27607},
		{FreqHz: 437_800_000, Name: "ISS", NoradID: 25544},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFilterTargetsByNorad_KeepsOnlyGroup(t *testing.T) {
	in := []Target{
		{FreqHz: 145_900_000, Name: "AO-7", NoradID: 7530},
		{FreqHz: 437_800_000, Name: "ISS", NoradID: 25544},
		{FreqHz: 436_795_000, Name: "SO-50", NoradID: 27607},
	}
	got := FilterTargetsByNorad(in, map[int]struct{}{25544: {}, 27607: {}})
	want := []Target{
		{FreqHz: 437_800_000, Name: "ISS", NoradID: 25544},
		{FreqHz: 436_795_000, Name: "SO-50", NoradID: 27607},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFilterTargetsByNorad_EmptyAllowPassthrough(t *testing.T) {
	in := []Target{{FreqHz: 1, NoradID: 1}}
	if got := FilterTargetsByNorad(in, nil); !slices.Equal(got, in) {
		t.Errorf("got %+v, want passthrough", got)
	}
}
