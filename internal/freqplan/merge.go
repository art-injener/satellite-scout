package freqplan

import (
	"math"
	"slices"
)

// Merge строит план: для каждого пролёта собирает передатчики из sources
// (порядок = приоритет: первый побеждает на близкой частоте). Строки
// сортируются по AOS, затем по частоте. UpdatedMs не трогает — штамп
// ставит запись в CSV.
func Merge(passes []Pass, sources []Source) []Entry {
	if len(passes) == 0 {
		return nil
	}
	var out []Entry
	for _, p := range passes {
		txs := collect(p.NoradID, sources)
		for _, tx := range txs {
			name := tx.Name
			if name == "" {
				name = p.SatName
			}
			out = append(out, Entry{
				NoradID: p.NoradID, Name: name, UUID: tx.UUID,
				FreqHz: tx.FreqHz, Mode: tx.Mode, Baud: tx.Baud,
				AOS: p.AOS, LOS: p.LOS, Source: tx.Source,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b Entry) int {
		if a.AOS != b.AOS {
			return int(a.AOS - b.AOS)
		}
		if a.FreqHz != b.FreqHz {
			return int(a.FreqHz - b.FreqHz)
		}
		return a.NoradID - b.NoradID
	})
	return out
}

// collect сливает передатчики NORAD из источников; уже занятая частота
// (в допуске nearHz) не перекрывается более слабым источником.
func collect(noradID int, sources []Source) []Tx {
	var out []Tx
	for _, src := range sources {
		if src == nil {
			continue
		}
		for _, tx := range src.Transmitters(noradID) {
			if tx.FreqHz <= 0 {
				continue
			}
			if covered(out, tx.FreqHz) {
				continue
			}
			out = append(out, tx)
		}
	}
	return out
}

func covered(have []Tx, freqHz int64) bool {
	for _, tx := range have {
		if math.Abs(float64(tx.FreqHz-freqHz)) <= nearHz {
			return true
		}
	}
	return false
}
