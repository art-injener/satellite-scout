package freqplan

import (
	"math"
	"strings"
)

// Длительности гибкой стоянки (ADR-004 §2.4).
// Короткая проба без энергии; при сигнале — от вида TX и baud, не фиксированные 15 с.
const (
	ProbeMs          int64   = 5_000  // нет энергии — уходим; по водопаду всплеск маяка проявляется в обнаружителе за ~5 с
	DefaultFrameBits float64 = 2048.0 // типичный AX.25 / телеметрийный кадр
	DwellK           float64 = 4.0    // запас на синхронизацию и ≥2 кадра CRC
	DwellMinMs       int64   = 2_000  // ADR: неизвестные baud → от 2 с
	TelemetryFloorMs int64   = 30_000 // пол маяка: 15 с мало для автоподбора по CRC
	DwellMaxMs       int64   = 60_000 // потолок формулы под редкие маяки
	CWHoldMs         int64   = 20_000
	SSTVHoldMs       int64   = 120_000
	VoiceHoldMs      int64   = 15_000
	MaxHoldMs        int64   = 180_000
	GoneMs           int64   = 10_000 // как bandscan.HoldMs: сигнал пропал — уход
	// Земная или непрерывная энергия рядом с номиналом TX: не держим полный пол маяка.
	TerrestrialHoldMs int64 = 8_000
	MinSNRDB                = 10
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
	if ok && sight.SNRDB >= MinSNRDB && !(sight.LastSeenMs > 0 && nowMs-sight.LastSeenMs > GoneMs) {
		mode, baud := e.Mode, e.Baud
		if sight.Modulation != "" {
			mode = sight.Modulation
		}
		if sight.Baud > 0 {
			baud = sight.Baud
		}
		holdMs := holdForSighting(mode, e.Name, baud, sight)
		if sight.BeaconPeriodS > 0 {
			periodHold := int64(math.Round(sight.BeaconPeriodS * 1.2 * 1000))
			if periodHold < ProbeMs {
				periodHold = ProbeMs
			}
			// Период маяка удлиняет только подтверждённый КА, не земную помеху.
			o := strings.ToLower(strings.TrimSpace(sight.Origin))
			if (o == "satellite" || o == "satellite_probable") && periodHold > holdMs {
				holdMs = periodHold
			}
		}
		return holdUntil(startMs, nowMs, holdMs, maxEnd, sight.LastSeenMs)
	}

	// Энергия в полосе без матча с номиналом: стоим пол телеметрии — точности
	// классификации важнее скорость обхода молчащих TX.
	if s, ok := strongestFresh(sights, nowMs); ok {
		return holdUntil(startMs, nowMs, TelemetryFloorMs, maxEnd, s.LastSeenMs)
	}
	return nowMs
}

// holdUntil — цель start+holdMs, не выше maxEnd; ранний wake по LastSeen+GoneMs.
func holdUntil(startMs, nowMs, holdMs, maxEnd, lastSeenMs int64) int64 {
	targetEnd := startMs + holdMs
	if targetEnd > maxEnd {
		targetEnd = maxEnd
	}
	if nowMs >= targetEnd {
		return nowMs
	}
	if lastSeenMs > 0 {
		wake := lastSeenMs + GoneMs
		if wake > nowMs && wake < targetEnd {
			return wake
		}
	}
	return targetEnd
}

// strongestFresh — самый сильный участок с С/Ш ≥ MinSNRDB, ещё не пропавший.
func strongestFresh(sights []Sighting, nowMs int64) (Sighting, bool) {
	var best Sighting
	found := false
	for _, s := range sights {
		if s.SNRDB < MinSNRDB {
			continue
		}
		if s.LastSeenMs > 0 && nowMs-s.LastSeenMs > GoneMs {
			continue
		}
		if !found || s.SNRDB > best.SNRDB {
			best, found = s, true
		}
	}
	return best, found
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
		s, ok := MatchSighting(sights, e.NoradID, e.FreqHz)
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
	if isTelemetryMode(m) {
		return telemetryDwellMs(baud)
	}
	return VoiceHoldMs
}

// holdForSighting — длительность по виду TX, урезанная без подтверждения КА.
// Пол маяка (телеметрия ≥30 с) только для satellite / satellite_probable или pulsed;
// иначе помеха рядом с номиналом держит hop и выглядит как зависание.
// CW / голос / SSTV не урезаем: там другие окна.
func holdForSighting(mode, name string, baud float64, sight Sighting) int64 {
	hold := kindHoldMs(mode, name, baud)
	if hold != telemetryDwellMs(baud) && !isTelemetryMode(strings.ToUpper(mode)) {
		return hold
	}
	o := strings.ToLower(strings.TrimSpace(sight.Origin))
	em := strings.ToLower(strings.TrimSpace(sight.Emission))
	if o == "satellite" || o == "satellite_probable" || em == "pulsed" {
		return hold
	}
	if hold > TerrestrialHoldMs {
		return TerrestrialHoldMs
	}
	return hold
}

// telemetryDwellMs — ADR-004: k × frame_bits / baud, с полом под период маяка.
func telemetryDwellMs(baud float64) int64 {
	if baud <= 0 {
		return TelemetryFloorMs
	}
	ms := int64(math.Round(DwellK * DefaultFrameBits / baud * 1000))
	if ms < DwellMinMs {
		ms = DwellMinMs
	}
	if ms < TelemetryFloorMs {
		ms = TelemetryFloorMs
	}
	if ms > DwellMaxMs {
		ms = DwellMaxMs
	}
	return ms
}

func isTelemetryMode(modeUpper string) bool {
	for _, p := range []string{"AFSK", "GMSK", "BPSK", "G3RUH", "FSK", "OOK"} {
		if strings.Contains(modeUpper, p) {
			return true
		}
	}
	return false
}

// airMatchHz — |Δf| «этот участок — цель hop эфира».
const airMatchHz = 25_000

// DecideHoldAir — стоянка на hop «+эфир» (нет TX плана): проба, затем по виду/периоду.
// Нет энергии после пробы — уход сразу (не полный dwell конфига).
func DecideHoldAir(w Window, startMs, nowMs int64, sights []Sighting) int64 {
	if nowMs < startMs {
		nowMs = startMs
	}
	maxEnd := startMs + MaxHoldMs
	probeEnd := startMs + ProbeMs
	if nowMs < probeEnd {
		return probeEnd
	}
	sight, ok := sightAtCenter(w.CenterHz, sights, nowMs)
	if !ok {
		return nowMs
	}
	holdMs := holdForSighting(sight.Modulation, "", sight.Baud, sight)
	if sight.BeaconPeriodS > 0 {
		periodHold := int64(math.Round(sight.BeaconPeriodS * 2 * 1000))
		if periodHold < ProbeMs {
			periodHold = ProbeMs
		}
		if periodHold > holdMs {
			holdMs = periodHold
		}
		if holdMs > DwellMaxMs {
			holdMs = DwellMaxMs
		}
	}
	// Зонды OOK и импульсы: не урезаем до TerrestrialHoldMs — нужен период/кадр.
	em := strings.ToLower(strings.TrimSpace(sight.Emission))
	mod := strings.ToUpper(strings.TrimSpace(sight.Modulation))
	if em == "pulsed" || strings.Contains(mod, "OOK") {
		if holdMs < TelemetryFloorMs {
			holdMs = TelemetryFloorMs
		}
	}
	return holdUntil(startMs, nowMs, holdMs, maxEnd, sight.LastSeenMs)
}

func sightAtCenter(centerHz uint64, sights []Sighting, nowMs int64) (Sighting, bool) {
	var best Sighting
	found := false
	c := int64(centerHz) //nolint:gosec // частоты канала < MaxInt64
	for _, s := range sights {
		if absInt64(s.CenterHz, c) > airMatchHz {
			continue
		}
		if s.SNRDB < MinSNRDB {
			continue
		}
		if s.LastSeenMs > 0 && nowMs-s.LastSeenMs > GoneMs {
			continue
		}
		if !found || s.SNRDB > best.SNRDB {
			best, found = s, true
		}
	}
	return best, found
}

func absInt64(a, b int64) int64 {
	if a >= b {
		return a - b
	}
	return b - a
}
