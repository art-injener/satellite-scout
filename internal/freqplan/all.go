package freqplan

import "slices"

// All возвращает копию передатчиков curated по всем NORAD (для реестра КА).
func (c *Curated) All() map[int][]Tx {
	if c == nil {
		return nil
	}
	return cloneByNorad(c.byNorad)
}

// All возвращает копию передатчиков Heavens-Above по всем NORAD (для реестра КА).
func (h *HeavensAbove) All() map[int][]Tx {
	if h == nil {
		return nil
	}
	return cloneByNorad(h.byNorad)
}

// All возвращает копию передатчиков AMSAT по всем NORAD (для реестра КА).
func (a *AMSAT) All() map[int][]Tx {
	if a == nil {
		return nil
	}
	return cloneByNorad(a.byNorad)
}

func cloneByNorad(in map[int][]Tx) map[int][]Tx {
	out := make(map[int][]Tx, len(in))
	for id, txs := range in {
		out[id] = slices.Clone(txs)
	}
	return out
}
