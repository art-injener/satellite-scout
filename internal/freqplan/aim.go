package freqplan

import "math"

// Пороги наведения канала на энергию участка (hold / hop).
const (
	// AimMinDeltaHz — меньше этого сдвиг не трогаем: шум центра тяжести.
	AimMinDeltaHz int64 = 200
	// AimLiveDeltaHz — live-пик ушёл дальше: одна поправка после входного наведения.
	AimLiveDeltaHz int64 = 400
	// AimMatchHz — |Δf| «участок у LO» для наведения (шире airMatchHz: Доплер + ошибка каталога).
	AimMatchHz int64 = 100_000
	// AimMinSNRDB — порог СШ для наведения; ниже MinSNRDB стоянки, чтобы ловить слабые импульсы.
	AimMinSNRDB float32 = 6
)

// Причины решения наведения (для debug-журнала эфирной приёмки).
const (
	AimReasonOperatorLocked = "operator_locked"
	AimReasonNoSight        = "no_sight"
	AimReasonDeltaSmall     = "delta_small"
	AimReasonApplyEntry     = "apply_entry"
	AimReasonApplyLive      = "apply_live"
	AimReasonWaitLive       = "wait_live"
	AimReasonStableLive     = "stable_live"
	AimReasonPhaseDone      = "phase_done"
)

// Источник пика для наведения.
const (
	AimSourceRemembered = "remembered"
	AimSourceLive       = "live"
)

// HoldAimOffsetHz — OffsetHz канала так, чтобы LO + Offset + Doppler совпал с пиком участка.
// dopplerHz — поправка, которую цепочка уже прибавит (Авто/Вручную); 0 если её нет.
func HoldAimOffsetHz(loHz, sightCenterHz int64, dopplerHz float64) int64 {
	return sightCenterHz - loHz - int64(math.Round(dopplerHz))
}

// HoldAimSight — свежий участок в AimMatchHz от LO, ближайший к центру стоянки.
// Громкий чужой пик на краю окна (помеха / другой КА) не должен перетягивать канал с каталожной частоты.
func HoldAimSight(loHz uint64, sights []Sighting, nowMs int64) (Sighting, bool) {
	var best Sighting
	found := false
	c := int64(loHz) //nolint:gosec // частоты окна > 0
	bestDist := int64(0)
	for _, s := range sights {
		dist := absInt64(s.CenterHz, c)
		if dist > AimMatchHz {
			continue
		}
		if s.SNRDB < AimMinSNRDB {
			continue
		}
		if s.LastSeenMs > 0 && nowMs-s.LastSeenMs > GoneMs {
			continue
		}
		if !found || dist < bestDist || (dist == bestDist && s.SNRDB > best.SNRDB) {
			best, found, bestDist = s, true, dist
		}
	}
	return best, found
}

// AimPhase — сколько раз уже навели канал на этой стоянке.
type AimPhase int

const (
	AimNone  AimPhase = iota // ещё не наводили
	AimEntry                 // вход: память или первый пик
	AimLive                  // live-поправка сделана или не нужна
)

// AimState — состояние наведения на одну стоянку hop.
type AimState struct {
	Phase          AimPhase
	OperatorLocked bool
	AimedAtHz      int64 // абсолютная частота последнего наведения
	StartMs        int64
	CenterHz       uint64
}

// AimDecision — применить ли новый OffsetHz.
type AimDecision struct {
	Apply      bool
	OffsetHz   int64
	SightHz    int64
	Reason     string  // константа AimReason*
	Source     string  // AimSourceRemembered / AimSourceLive
	SightSNRDB float32 // СШ выбранного участка
	SightBWHz  int64   // ширина участка из bandscan, если есть
}

func aimFromSight(s Sighting, source string) AimDecision {
	return AimDecision{
		SightHz:    s.CenterHz,
		Source:     source,
		SightSNRDB: s.SNRDB,
		SightBWHz:  s.BandwidthHz,
	}
}

// DecideHoldAim — когда сдвинуть канал на пик (не каждый БПФ; Apply максимум дважды за стоянку).
// remembered — участки из памяти (вход в hop); live — текущие в полосе.
// После ручной смены сдвига (OperatorLocked) больше не трогаем до сброса стоянки.
func DecideHoldAim(st AimState, loHz uint64, remembered, live []Sighting, nowMs int64, dopplerHz float64) (AimDecision, AimState) {
	out := st
	if out.OperatorLocked {
		return AimDecision{Reason: AimReasonOperatorLocked}, out
	}
	lo := int64(loHz) //nolint:gosec // частоты окна > 0

	switch out.Phase {
	case AimNone:
		s, ok := HoldAimSight(loHz, remembered, nowMs)
		source := AimSourceRemembered
		if !ok {
			s, ok = HoldAimSight(loHz, live, nowMs)
			source = AimSourceLive
		}
		if !ok {
			return AimDecision{Reason: AimReasonNoSight}, out
		}
		out.Phase = AimEntry
		out.AimedAtHz = s.CenterHz
		off := HoldAimOffsetHz(lo, s.CenterHz, dopplerHz)
		dec := aimFromSight(s, source)
		dec.OffsetHz = off
		if absInt64(off, 0) < AimMinDeltaHz {
			dec.Reason = AimReasonDeltaSmall
			return dec, out
		}
		dec.Apply = true
		dec.Reason = AimReasonApplyEntry
		return dec, out

	case AimEntry:
		s, ok := HoldAimSight(loHz, live, nowMs)
		if !ok {
			return AimDecision{Reason: AimReasonWaitLive}, out
		}
		if absInt64(s.CenterHz, out.AimedAtHz) < AimLiveDeltaHz {
			// Стабильный live: закрываем фазу после пробы (не каждый тик).
			if nowMs >= out.StartMs+ProbeMs {
				out.Phase = AimLive
				dec := aimFromSight(s, AimSourceLive)
				dec.Reason = AimReasonStableLive
				return dec, out
			}
			dec := aimFromSight(s, AimSourceLive)
			dec.Reason = AimReasonWaitLive
			return dec, out
		}
		// Пик ушёл или live не совпал с памятью — правим сразу (короткие импульсы).
		off := HoldAimOffsetHz(lo, s.CenterHz, dopplerHz)
		out.Phase = AimLive
		out.AimedAtHz = s.CenterHz
		dec := aimFromSight(s, AimSourceLive)
		dec.OffsetHz = off
		if absInt64(off, 0) < AimMinDeltaHz {
			dec.Reason = AimReasonDeltaSmall
			return dec, out
		}
		dec.Apply = true
		dec.Reason = AimReasonApplyLive
		return dec, out

	default:
		return AimDecision{Reason: AimReasonPhaseDone}, out
	}
}

// ResetAimState — новая стоянка: сброс фазы и ручного запрета.
func ResetAimState(startMs int64, centerHz uint64) AimState {
	return AimState{StartMs: startMs, CenterHz: centerHz}
}

// AimNeedsWake — пока не закрыли наведение, стоянка должна будить чаще коротких импульсов.
func AimNeedsWake(st AimState) bool {
	return !st.OperatorLocked && (st.Phase == AimNone || st.Phase == AimEntry)
}
