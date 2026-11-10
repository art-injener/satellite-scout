package sweep

import (
	"slices"
	"testing"
)

func TestWindows_CenterIsOneWindow(t *testing.T) {
	got := Windows([]Range{{CenterHz: 145_900_000}}, 2_000_000)
	if want := []uint64{145_900_000}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWindows_EdgesSplitByBand(t *testing.T) {
	got := Windows([]Range{{LoHz: 435_000_000, HiHz: 439_000_000}}, 2_000_000)
	if want := []uint64{435_400_000, 437_000_000, 438_600_000}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWindows_EdgesCenteredWhenNotMultiple(t *testing.T) {
	// 3 МГц на окна по 2 МГц: два окна с перекрытием, лишняя полоса поровну по краям.
	got := Windows([]Range{{LoHz: 435_000_000, HiHz: 438_000_000}}, 2_000_000)
	if want := []uint64{435_700_000, 437_300_000}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWindows_SatelliteUHFClusterAtRTLUsableBand(t *testing.T) {
	// Цели 435,4–437,8 (2,4 МГц) при годной 2,048 МГц: два окна, не уже полосы.
	const band uint32 = 2_048_000
	got := Windows([]Range{{LoHz: 435_400_000, HiHz: 437_800_000}}, band)
	want := []uint64{435_780_800, 437_419_200}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWindows_SeamLiesInsideBothWindows(t *testing.T) {
	// 434.5–438.5 МГц при полосе 2.048 МГц раньше сходились краями на 436.5 МГц.
	const band uint32 = 2_048_000
	got := Windows([]Range{{LoHz: 434_500_000, HiHz: 438_500_000}}, band)
	want := []uint64{434_861_600, 436_500_000, 438_138_400}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	half := uint64(band) / 2
	seam := (got[0] + got[1]) / 2
	if seam <= got[0] || seam >= got[0]+half || seam <= got[1]-half || seam >= got[1] {
		t.Errorf("seam %d is not inside both windows %d and %d", seam, got[0], got[1])
	}
}

func TestWindows_NarrowEdgesGiveMiddle(t *testing.T) {
	got := Windows([]Range{{LoHz: 145_800_000, HiHz: 146_000_000}}, 2_000_000)
	if want := []uint64{145_900_000}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWindows_SortedAndDeduplicated(t *testing.T) {
	got := Windows([]Range{
		{CenterHz: 437_000_000},
		{CenterHz: 145_900_000},
		{LoHz: 436_000_000, HiHz: 438_000_000},
	}, 2_000_000)
	if want := []uint64{145_900_000, 437_000_000}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWindows_ZeroBand(t *testing.T) {
	if got := Windows([]Range{{CenterHz: 145_900_000}}, 0); got != nil {
		t.Errorf("zero band must give no windows, got %v", got)
	}
}
