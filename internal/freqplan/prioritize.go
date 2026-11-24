package freqplan

import "slices"

// PrioritizeTargets ставит цели со свежей энергией (heardMs > 0) в начало круга.
// Внутри групп порядок прежний. Внутри стоянки частоту не меняем — только пересборка окон.
func PrioritizeTargets(targets []Target, heardMs func(norad int, freqHz uint64) int64) []Target {
	if len(targets) == 0 || heardMs == nil {
		return targets
	}
	type ranked struct {
		t     Target
		heard int64
		idx   int
	}
	items := make([]ranked, len(targets))
	for i, t := range targets {
		items[i] = ranked{t: t, heard: heardMs(t.NoradID, t.FreqHz), idx: i}
	}
	slices.SortStableFunc(items, func(a, b ranked) int {
		ah, bh := a.heard > 0, b.heard > 0
		switch {
		case ah && !bh:
			return -1
		case !ah && bh:
			return 1
		case ah && bh && a.heard != b.heard:
			if a.heard > b.heard {
				return -1
			}
			return 1
		default:
			return a.idx - b.idx
		}
	})
	out := make([]Target, len(items))
	for i, it := range items {
		out[i] = it.t
	}
	return out
}
