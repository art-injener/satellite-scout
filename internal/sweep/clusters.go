package sweep

import (
	"slices"
)

// EdgeMargin — доля годной полосы у каждого края, куда несущую не ставим (Доплер).
const EdgeMargin = 0.1

// Clusters строит жадные диапазоны по частотам TX: ширина кластера не больше
// полезной части годной полосы (1 − 2·EdgeMargin). Одна частота — окно-центр.
func Clusters(freqs []uint64, bandHz uint32) []Range {
	if bandHz == 0 || len(freqs) == 0 {
		return nil
	}
	useful := uint64(float64(bandHz) * (1 - 2*EdgeMargin))
	if useful == 0 {
		useful = uint64(bandHz)
	}
	sorted := slices.Clone(freqs)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	var out []Range
	for i := 0; i < len(sorted); {
		first := sorted[i]
		last := first
		j := i + 1
		for j < len(sorted) && sorted[j]-first <= useful {
			last = sorted[j]
			j++
		}
		if first == last {
			out = append(out, Range{CenterHz: first})
		} else {
			out = append(out, Range{LoHz: first, HiHz: last})
		}
		i = j
	}
	return out
}

// MergeRanges объединяет списки диапазонов: перекрытия и касания сливает,
// точку внутри интервала поглощает. Результат по возрастанию частоты.
func MergeRanges(lists ...[]Range) []Range {
	var pts []uint64
	var spans []Range
	for _, list := range lists {
		for _, r := range list {
			if r.CenterHz > 0 {
				pts = append(pts, r.CenterHz)
				continue
			}
			if r.HiHz > r.LoHz {
				spans = append(spans, Range{LoHz: r.LoHz, HiHz: r.HiHz})
			}
		}
	}
	slices.SortFunc(spans, func(a, b Range) int {
		switch {
		case a.LoHz < b.LoHz:
			return -1
		case a.LoHz > b.LoHz:
			return 1
		case a.HiHz < b.HiHz:
			return -1
		case a.HiHz > b.HiHz:
			return 1
		default:
			return 0
		}
	})
	merged := make([]Range, 0, len(spans))
	for _, sp := range spans {
		if n := len(merged); n > 0 && sp.LoHz <= merged[n-1].HiHz {
			if sp.HiHz > merged[n-1].HiHz {
				merged[n-1].HiHz = sp.HiHz
			}
			continue
		}
		merged = append(merged, sp)
	}

	slices.Sort(pts)
	pts = slices.Compact(pts)
	var out []Range
	pi := 0
	for _, sp := range merged {
		for pi < len(pts) && pts[pi] < sp.LoHz {
			out = append(out, Range{CenterHz: pts[pi]})
			pi++
		}
		for pi < len(pts) && pts[pi] <= sp.HiHz {
			pi++ // точка внутри интервала
		}
		out = append(out, sp)
	}
	for ; pi < len(pts); pi++ {
		out = append(out, Range{CenterHz: pts[pi]})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
