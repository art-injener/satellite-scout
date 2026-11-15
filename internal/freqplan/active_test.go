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
