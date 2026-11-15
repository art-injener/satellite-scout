package freqplan

import "slices"

// ActiveFreqs — частоты плана, чей пролёт пересекает [nowMs, untilMs]
// (над горизонтом сейчас или восходящие до until). allow == nil — без фильтра полосы.
func ActiveFreqs(plan []Entry, nowMs, untilMs int64, allow func(int64) bool) []uint64 {
	if len(plan) == 0 {
		return nil
	}
	seen := make(map[uint64]struct{}, len(plan))
	var out []uint64
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
		out = append(out, hz)
	}
	slices.Sort(out)
	return out
}
