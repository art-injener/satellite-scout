package freqplan

import "math"

// Window — окно обхода (центр и ширина годной полосы).
type Window struct {
	CenterHz uint64
	SpanHz   uint32
}

// Sighting — обнаруженный участок в полосе (из bandscan, без зависимости пакета).
type Sighting struct {
	CenterHz int64
	NoradID  int
	Origin   string
	Emission string
	SNRDB    float32
}

// Visit — итог стоянки обхода по одному ожидаемому передатчику из плана.
type Visit struct {
	RadioPath     string
	CenterHz      uint64
	SpanHz        uint32
	WindowStartMs int64
	WindowEndMs   int64
	NoradID       int
	Name          string
	UUID          string
	FreqHz        int64
	Energy        bool
	Origin        string
	Emission      string
	Received      bool // MVP: совпадает с Energy; кадр демода — позже
	Source        string
}

// Judge сопоставляет план и обнаружения с оконной стоянкой.
// В журнал попадают только TX плана, чья частота в окне и чей пролёт
// пересекается со стоянкой. Received пока = Energy.
func Judge(pathID string, w Window, startMs, endMs int64, plan []Entry, sights []Sighting) []Visit {
	if w.SpanHz == 0 || len(plan) == 0 {
		return nil
	}
	lo := int64(w.CenterHz) - int64(w.SpanHz)/2
	hi := int64(w.CenterHz) + int64(w.SpanHz)/2
	var out []Visit
	for _, e := range plan {
		if e.FreqHz < lo || e.FreqHz > hi {
			continue
		}
		if !passOverlaps(e.AOS, e.LOS, startMs, endMs) {
			continue
		}
		v := Visit{
			RadioPath: pathID, CenterHz: w.CenterHz, SpanHz: w.SpanHz,
			WindowStartMs: startMs, WindowEndMs: endMs,
			NoradID: e.NoradID, Name: e.Name, UUID: e.UUID, FreqHz: e.FreqHz,
			Source: e.Source,
		}
		if s, ok := closestSighting(sights, e.FreqHz); ok {
			v.Energy, v.Received = true, true
			v.Origin, v.Emission = s.Origin, s.Emission
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func passOverlaps(aos, los, startMs, endMs int64) bool {
	if los > 0 && aos > 0 {
		return aos < endMs && los > startMs
	}
	// Нет окна пролёта — частота из плана всё равно учитывается в полосе.
	return true
}

func closestSighting(sights []Sighting, freqHz int64) (Sighting, bool) {
	var best Sighting
	bestDist := float64(nearHz) + 1
	found := false
	for _, s := range sights {
		d := math.Abs(float64(s.CenterHz - freqHz))
		if d <= nearHz && d < bestDist {
			best, bestDist, found = s, d, true
		}
	}
	return best, found
}
