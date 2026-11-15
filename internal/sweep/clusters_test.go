package sweep

import (
	"slices"
	"testing"
)

func TestClusters_SingleFreqIsCenter(t *testing.T) {
	got := Clusters([]uint64{401_000_000}, 2_048_000)
	want := []Range{{CenterHz: 401_000_000}}
	if !rangesEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestClusters_CloseFreqsShareOneRange(t *testing.T) {
	// Полезная ширина при полосе 2 МГц и запасе 10 % с края: 1,6 МГц.
	got := Clusters([]uint64{436_000_000, 436_800_000}, 2_000_000)
	want := []Range{{LoHz: 436_000_000, HiHz: 436_800_000}}
	if !rangesEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestClusters_FarFreqsSplitByUsefulBand(t *testing.T) {
	// 2,4 МГц между краями > 1,638 МГц полезной ширины при 2,048 МГц.
	got := Clusters([]uint64{435_400_000, 437_800_000}, 2_048_000)
	want := []Range{{CenterHz: 435_400_000}, {CenterHz: 437_800_000}}
	if !rangesEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestClusters_SortsAndDedups(t *testing.T) {
	got := Clusters([]uint64{437_000_000, 145_900_000, 437_000_000}, 2_000_000)
	want := []Range{{CenterHz: 145_900_000}, {CenterHz: 437_000_000}}
	if !rangesEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestClusters_EmptyOrZeroBand(t *testing.T) {
	if got := Clusters(nil, 2_000_000); got != nil {
		t.Errorf("empty freqs: got %+v", got)
	}
	if got := Clusters([]uint64{145_900_000}, 0); got != nil {
		t.Errorf("zero band: got %+v", got)
	}
}

func TestMergeRanges_UnionsAndMergesOverlap(t *testing.T) {
	got := MergeRanges(
		[]Range{{CenterHz: 145_900_000}, {LoHz: 435_400_000, HiHz: 437_800_000}},
		[]Range{{CenterHz: 401_000_000}, {LoHz: 436_000_000, HiHz: 438_000_000}},
	)
	want := []Range{
		{CenterHz: 145_900_000},
		{CenterHz: 401_000_000},
		{LoHz: 435_400_000, HiHz: 438_000_000},
	}
	if !rangesEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMergeRanges_CenterInsideIntervalAbsorbed(t *testing.T) {
	got := MergeRanges(
		[]Range{{LoHz: 435_000_000, HiHz: 438_000_000}},
		[]Range{{CenterHz: 437_000_000}},
	)
	want := []Range{{LoHz: 435_000_000, HiHz: 438_000_000}}
	if !rangesEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMergeRanges_NilArgs(t *testing.T) {
	if got := MergeRanges(nil, nil); got != nil {
		t.Errorf("got %+v", got)
	}
	got := MergeRanges([]Range{{CenterHz: 145_900_000}}, nil)
	if !rangesEqual(got, []Range{{CenterHz: 145_900_000}}) {
		t.Errorf("got %+v", got)
	}
}

func TestPlanWindows_ClustersUnionConfigThenCut(t *testing.T) {
	const band uint32 = 2_048_000
	cfg := []Range{{CenterHz: 145_900_000}, {LoHz: 435_400_000, HiHz: 437_800_000}}
	planFreqs := []uint64{401_000_000, 436_500_000}
	got := Windows(MergeRanges(cfg, Clusters(planFreqs, band)), band)
	// 145,9; 401; плюс резка UHF-кластера конфига (план 436,5 внутри него).
	want := []uint64{145_900_000, 401_000_000, 435_780_800, 437_419_200}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func rangesEqual(a, b []Range) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
