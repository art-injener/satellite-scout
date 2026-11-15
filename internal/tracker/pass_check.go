package tracker

import (
	"math"
	"slices"
	"time"
)

// passMatchWindow — наибольший сдвиг середины, при котором пролёт эталона и наш считаются одним.
// Соседние пролёты одного КА на низкой орбите идут через ~90 мин, поэтому половина периода
// ещё однозначна, а сдвиг в полчаса от старого TLE попадает в пару.
const passMatchWindow = 45 * time.Minute

// RefPass — пролёт из стороннего прогноза (например Heavens-Above) для сверки с нашим.
type RefPass struct {
	NoradID int
	Start   time.Time
	End     time.Time
}

// PassCheck — итог сверки наших пролётов со сторонним прогнозом.
// Сдвиг — модуль разницы середин пролётов: середина не зависит от порога высоты AOS/LOS,
// а ошибка TLE вдоль орбиты сдвигает весь пролёт.
type PassCheck struct {
	Source       string    `json:"source"`
	Matched      int       `json:"matched"`
	MedianDriftS float64   `json:"median_drift_s"`
	ISSDriftS    *float64  `json:"iss_drift_s,omitempty"`
	CheckedAt    time.Time `json:"checked_at,omitzero"`
}

// ComparePasses ищет каждому пролёту эталона ближайший наш того же КА и сводит сдвиги.
// Source и CheckedAt заполняет вызывающий.
func ComparePasses(ours []*Pass, ref []RefPass) PassCheck {
	byNorad := make(map[int][]int64)
	for _, p := range ours {
		if p != nil {
			byNorad[p.NoradID] = append(byNorad[p.NoradID], (p.AOS+p.LOS)/2)
		}
	}

	var all, iss []float64
	for _, r := range ref {
		mid := (r.Start.UnixMilli() + r.End.UnixMilli()) / 2
		best := int64(math.MaxInt64)
		for _, m := range byNorad[r.NoradID] {
			best = min(best, absMs(m-mid))
		}
		if best > passMatchWindow.Milliseconds() {
			continue
		}
		drift := float64(best) / 1000
		all = append(all, drift)
		if r.NoradID == issNoradID {
			iss = append(iss, drift)
		}
	}

	out := PassCheck{Matched: len(all), MedianDriftS: medianFloat(all)}
	if len(iss) > 0 {
		v := medianFloat(iss)
		out.ISSDriftS = &v
	}
	return out
}

func medianFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	return s[len(s)/2]
}

func absMs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
