package tracker

import (
	"testing"
	"time"
)

var checkBase = time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)

// ourPass builds a predicted pass with AOS/LOS at the given offsets from checkBase.
func ourPass(norad int, aos, los time.Duration) *Pass {
	return &Pass{NoradID: norad, AOS: checkBase.Add(aos).UnixMilli(), LOS: checkBase.Add(los).UnixMilli()}
}

func haRef(norad int, start, end time.Duration) RefPass {
	return RefPass{NoradID: norad, Start: checkBase.Add(start), End: checkBase.Add(end)}
}

func TestComparePasses_MedianDriftOfMidpoints(t *testing.T) {
	ours := []*Pass{
		ourPass(1, 30*time.Second, 10*time.Minute+30*time.Second),
		ourPass(2, time.Hour+20*time.Second, time.Hour+8*time.Minute+20*time.Second),
		ourPass(3, 2*time.Hour-90*time.Second, 2*time.Hour+6*time.Minute-90*time.Second),
	}
	ref := []RefPass{
		haRef(1, 0, 10*time.Minute),
		haRef(2, time.Hour, time.Hour+8*time.Minute),
		haRef(3, 2*time.Hour, 2*time.Hour+6*time.Minute),
	}

	got := ComparePasses(ours, ref)

	if got.Matched != 3 {
		t.Fatalf("Matched = %d, want 3", got.Matched)
	}
	if got.MedianDriftS != 30 {
		t.Fatalf("MedianDriftS = %v, want 30 (abs drifts 30, 20, 90)", got.MedianDriftS)
	}
}

func TestComparePasses_ElevationThresholdDoesNotCountAsDrift(t *testing.T) {
	// Наш AOS считается от 5°, у эталона от горизонта: пролёт короче с обеих сторон.
	ours := []*Pass{ourPass(1, time.Minute, 9*time.Minute)}
	ref := []RefPass{haRef(1, 0, 10*time.Minute)}

	got := ComparePasses(ours, ref)

	if got.Matched != 1 || got.MedianDriftS != 0 {
		t.Fatalf("got %+v, want one pair with zero drift", got)
	}
}

func TestComparePasses_StaleTLEHalfHourShiftIsMatched(t *testing.T) {
	ours := []*Pass{ourPass(1, 30*time.Minute, 40*time.Minute)}
	ref := []RefPass{haRef(1, 0, 10*time.Minute)}

	got := ComparePasses(ours, ref)

	if got.Matched != 1 || got.MedianDriftS != 1800 {
		t.Fatalf("got %+v, want one pair drifting 1800 s", got)
	}
}

func TestComparePasses_NoPairForOtherSatelliteOrFarPass(t *testing.T) {
	ours := []*Pass{
		ourPass(2, 0, 10*time.Minute),
		ourPass(1, 90*time.Minute, 100*time.Minute),
	}
	ref := []RefPass{haRef(1, 0, 10*time.Minute)}

	got := ComparePasses(ours, ref)

	if got.Matched != 0 {
		t.Fatalf("Matched = %d, want 0", got.Matched)
	}
}

func TestComparePasses_PicksNearestOfOurPasses(t *testing.T) {
	ours := []*Pass{
		ourPass(1, -40*time.Minute, -30*time.Minute),
		ourPass(1, 10*time.Second, 10*time.Minute+10*time.Second),
	}
	ref := []RefPass{haRef(1, 0, 10*time.Minute)}

	got := ComparePasses(ours, ref)

	if got.Matched != 1 || got.MedianDriftS != 10 {
		t.Fatalf("got %+v, want nearest pass drifting 10 s", got)
	}
}

func TestComparePasses_ISSDriftSeparately(t *testing.T) {
	ours := []*Pass{
		ourPass(1, 0, 10*time.Minute),
		ourPass(issNoradID, time.Hour+2*time.Minute, time.Hour+12*time.Minute),
	}
	ref := []RefPass{
		haRef(1, 0, 10*time.Minute),
		haRef(issNoradID, time.Hour, time.Hour+10*time.Minute),
	}

	got := ComparePasses(ours, ref)

	if got.ISSDriftS == nil || *got.ISSDriftS != 120 {
		t.Fatalf("ISSDriftS = %v, want 120", got.ISSDriftS)
	}
}

func TestComparePasses_NoReferenceIsEmpty(t *testing.T) {
	got := ComparePasses([]*Pass{ourPass(1, 0, 10*time.Minute)}, nil)

	if got.Matched != 0 || got.MedianDriftS != 0 || got.ISSDriftS != nil {
		t.Fatalf("got %+v, want empty check", got)
	}
}
