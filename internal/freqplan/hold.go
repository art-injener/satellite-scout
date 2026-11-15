package freqplan

import "strings"

// Длительности гибкой стоянки (константы, не конфиг).
const (
	ProbeMs         int64 = 15_000
	TelemetryHoldMs int64 = 20_000
	CWHoldMs        int64 = 20_000
	SSTVHoldMs      int64 = 120_000
	VoiceHoldMs     int64 = 15_000
	MaxHoldMs       int64 = 180_000
	GoneMs          int64 = 10_000 // как bandscan.HoldMs: сигнал пропал — уход
	MinSNRDB              = 10
)

// DecideHold — до какого момента (Unix мс) стоять на окне.
// Нет ожидаемого TX плана: dwell конфига. Иначе проба ProbeMs, затем по виду
// сигнала; СШ < MinSNRDB или нет энергии — без продления; пропал > GoneMs — уход;
// потолок MaxHoldMs. holdUntil <= now означает «уйти сейчас».
func DecideHold(w Window, startMs, nowMs, configDwellMs int64, plan []Entry, sights []Sighting) int64 {
	if nowMs < startMs {
		nowMs = startMs
	}
	maxEnd := startMs + MaxHoldMs
	expected := expectedInWindow(w, startMs, plan)
	if len(expected) == 0 {
		end := startMs + configDwellMs
		if end > maxEnd {
			end = maxEnd
		}
		if nowMs >= end {
			return nowMs
		}
		return end
	}

	probeEnd := startMs + ProbeMs
	if nowMs < probeEnd {
		return probeEnd
	}

	e, sight, ok := matchExpected(expected, sights)
	if !ok {
		return nowMs
	}
	if sight.SNRDB < MinSNRDB {
		return nowMs
	}
	if sight.LastSeenMs > 0 && nowMs-sight.LastSeenMs > GoneMs {
		return nowMs
	}

	targetEnd := startMs + kindHoldMs(e.Mode, e.Name, e.Baud)
	if targetEnd > maxEnd {
		targetEnd = maxEnd
	}
	if nowMs >= targetEnd {
		return nowMs
	}
	// Разбудить раньше цели, чтобы проверить «пропал».
	if sight.LastSeenMs > 0 {
		wake := sight.LastSeenMs + GoneMs
		if wake > nowMs && wake < targetEnd {
			return wake
		}
	}
	return targetEnd
}

func expectedInWindow(w Window, startMs int64, plan []Entry) []Entry {
	if w.SpanHz == 0 || len(plan) == 0 {
		return nil
	}
	lo := int64(w.CenterHz) - int64(w.SpanHz)/2
	hi := int64(w.CenterHz) + int64(w.SpanHz)/2
	until := startMs + MaxHoldMs
	var out []Entry
	for _, e := range plan {
		if e.FreqHz < lo || e.FreqHz > hi {
			continue
		}
		if !passOverlaps(e.AOS, e.LOS, startMs, until) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func matchExpected(expected []Entry, sights []Sighting) (Entry, Sighting, bool) {
	var best Entry
	var bestSight Sighting
	found := false
	for _, e := range expected {
		s, ok := closestSighting(sights, e.FreqHz)
		if !ok {
			continue
		}
		if !found || s.SNRDB > bestSight.SNRDB {
			best, bestSight, found = e, s, true
		}
	}
	return best, bestSight, found
}

func kindHoldMs(mode, name string, baud float64) int64 {
	m := strings.ToUpper(mode)
	n := strings.ToUpper(name)
	if strings.Contains(m, "SSTV") || strings.Contains(n, "SSTV") {
		return SSTVHoldMs
	}
	if m == "CW" || strings.HasPrefix(m, "CW ") || strings.Contains(m, " CW") {
		return CWHoldMs
	}
	if baud > 0 && isTelemetryMode(m) {
		return TelemetryHoldMs
	}
	return VoiceHoldMs
}

func isTelemetryMode(modeUpper string) bool {
	for _, p := range []string{"AFSK", "GMSK", "BPSK", "G3RUH", "FSK"} {
		if strings.Contains(modeUpper, p) {
			return true
		}
	}
	return false
}
