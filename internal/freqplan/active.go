package freqplan

import "slices"

// Target — активный передатчик плана для обхода «Авто приём».
type Target struct {
	FreqHz  uint64
	Name    string
	NoradID int
}

// ActiveTargets — цели плана, чей пролёт пересекает [nowMs, untilMs]
// (над горизонтом сейчас или восходящие до until). По возрастанию частоты,
// одна запись на частоту (первая в плане). allow == nil — без фильтра полосы.
func ActiveTargets(plan []Entry, nowMs, untilMs int64, allow func(int64) bool) []Target {
	if len(plan) == 0 {
		return nil
	}
	seen := make(map[uint64]struct{}, len(plan))
	var out []Target
	for _, e := range plan {
		if e.FreqHz <= 0 {
			continue
		}
		if allow != nil && !allow(e.FreqHz) {
			continue
		}
		if !passOverlaps(e.AOS, e.LOS, nowMs, untilMs) {
			continue
		}
		hz := uint64(e.FreqHz) //nolint:gosec // частоты передатчиков положительны и далеко от MaxUint64
		if _, ok := seen[hz]; ok {
			continue
		}
		seen[hz] = struct{}{}
		out = append(out, Target{FreqHz: hz, Name: e.Name, NoradID: e.NoradID})
	}
	slices.SortFunc(out, func(a, b Target) int {
		switch {
		case a.FreqHz < b.FreqHz:
			return -1
		case a.FreqHz > b.FreqHz:
			return 1
		default:
			return 0
		}
	})
	return out
}

// ActiveFreqs — частоты ActiveTargets (совместимость с обзором по кластерам).
func ActiveFreqs(plan []Entry, nowMs, untilMs int64, allow func(int64) bool) []uint64 {
	targets := ActiveTargets(plan, nowMs, untilMs, allow)
	if len(targets) == 0 {
		return nil
	}
	out := make([]uint64, len(targets))
	for i, t := range targets {
		out[i] = t.FreqHz
	}
	return out
}

// FilterTargetsByNorad оставляет цели с NORAD из allow. Пустой allow — без фильтра
// (как «группа ещё не собрана»). Порядок и частоты сохраняются.
func FilterTargetsByNorad(targets []Target, allow map[int]struct{}) []Target {
	if len(targets) == 0 || len(allow) == 0 {
		return targets
	}
	out := make([]Target, 0, len(targets))
	for _, t := range targets {
		if _, ok := allow[t.NoradID]; ok {
			out = append(out, t)
		}
	}
	return out
}
