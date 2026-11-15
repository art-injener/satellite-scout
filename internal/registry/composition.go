package registry

import "slices"

// Источники, чьи NORAD входят в состав каталога для Space-Track (ADR-011 §2.7).
// SatNOGS Network и sonik сюда не входят: sonik даёт только свежие запуски в tracker.
var catalogCompositionSources = []string{SourceCurated, SourceHeavensAbove, SourceAMSAT}

// CatalogCompositionIDs возвращает NORAD из curated, Heavens-Above и AMSAT.
// Это основа запроса GP/SATCAT; свежие запуски sonik добавляет TLEStore отдельно.
func (r *Registry) CatalogCompositionIDs() []int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[int]struct{})
	for norad, bySrc := range r.txs {
		if norad <= 0 {
			continue
		}
		for _, src := range catalogCompositionSources {
			if len(bySrc[src]) > 0 {
				seen[norad] = struct{}{}
				break
			}
		}
	}
	ids := make([]int, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
