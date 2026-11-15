package registry

// satnogsCoveredBy — источники, после которых прямой SatNOGS для КА не нужен.
// Сам SatNOGS пробел не закрывает: его как раз и догружаем.
var satnogsCoveredBy = []string{SourceCurated, SourceHeavensAbove, SourceAMSAT, SourceSonik}

// FilterSatNOGSGaps оставляёт NORAD без передатчиков curated/HA/AMSAT/sonik.
// Для них прямой запрос в SatNOGS DB ещё имеет смысл; остальные уже закрыты.
func (r *Registry) FilterSatNOGSGaps(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if coveredByPrimarySources(r.txs[id]) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func coveredByPrimarySources(bySrc map[string][]Transmitter) bool {
	for _, src := range satnogsCoveredBy {
		if len(bySrc[src]) > 0 {
			return true
		}
	}
	return false
}
