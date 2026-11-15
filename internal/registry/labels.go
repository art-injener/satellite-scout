package registry

import (
	"slices"
	"strings"
)

// Метки отбора группы «над нами» (ADR-011 §2.4).
const (
	LabelInBandAlive = "in_band_alive" // по умолчанию: в диапазоне станции и жив
	LabelInBand      = "in_band"
	LabelAlive       = "alive"
	LabelHeard       = "heard"
	LabelStations    = "stations"
	LabelWeather     = "weather"
)

// inhabitedStations — обитаемые станции: МКС и CSS (Тяньгун).
var inhabitedStations = map[int]struct{}{
	25544: {}, // ISS
	48274: {}, // CSS / Tiangong
}

// weatherDownlinkMinHz / MaxHz — полоса APT/LRPT около 137 МГц.
const (
	weatherDownlinkMinHz = 136_000_000
	weatherDownlinkMaxHz = 138_000_000
)

// NormalizeGroupLabel пустую метку приводит к значению по умолчанию.
func NormalizeGroupLabel(label string) string {
	if label == "" {
		return LabelInBandAlive
	}
	return label
}

// ValidGroupLabel сообщает, что строка — известная метка группы (пустая = дефолт).
func ValidGroupLabel(label string) bool {
	switch NormalizeGroupLabel(label) {
	case LabelInBandAlive, LabelInBand, LabelAlive, LabelHeard, LabelStations, LabelWeather:
		return true
	default:
		return false
	}
}

// HasLabel сообщает, что КА несёт указанную метку реестра.
// Диапазон — у приёмника (bands), не у антенны. Метка «слышали» пока всегда false.
func (r *Registry) HasLabel(norad int, label string, bands Bands) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.hasLabelLocked(norad, NormalizeGroupLabel(label), bands)
}

// MatchGroupLabel отбирает КА в группу «над нами»: низкая орбита и выбранная метка.
// Пока источники живости (HA/AMSAT) не загружены, требование «жив» не отсекает никого,
// иначе группа на старте была бы пустой.
func (r *Registry) MatchGroupLabel(norad int, label string, bands Bands) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.matchGroupLabelLocked(norad, NormalizeGroupLabel(label), bands)
}

// Receivable — строгий отбор «низкая орбита, в диапазоне и жив» (или curated).
// Для карты и пролётов больше не используется; оставлен для проверок живости и совместимости.
func (r *Registry) Receivable(norad int, bands Bands) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if o, ok := r.orbits[norad]; ok && !lowOrbit(o.Line2) {
		return false
	}
	if !r.hasInBandLocked(norad, bands) {
		return false
	}
	return r.hasAliveLocked(norad)
}

// SortPreferringBand ставит передатчики внутри полосы антенны выше остальных.
// Полоса антенны КА не отсекает — только задаёт порядок.
func SortPreferringBand(txs []Transmitter, preferred Bands) []Transmitter {
	if len(txs) < 2 || len(preferred) == 0 {
		return txs
	}
	out := slices.Clone(txs)
	slices.SortStableFunc(out, func(a, b Transmitter) int {
		ain, bin := preferred.Allow(a.DownlinkHz), preferred.Allow(b.DownlinkHz)
		switch {
		case ain == bin:
			return 0
		case ain:
			return -1
		default:
			return 1
		}
	})
	return out
}

func (r *Registry) matchGroupLabelLocked(norad int, label string, bands Bands) bool {
	if o, ok := r.orbits[norad]; ok && !lowOrbit(o.Line2) {
		return false
	}
	switch label {
	case LabelInBandAlive:
		if !r.hasInBandLocked(norad, bands) {
			return false
		}
		if r.knowsLivenessLocked() && !r.hasAliveLocked(norad) {
			return false
		}
		return true
	case LabelInBand, LabelAlive, LabelHeard, LabelStations, LabelWeather:
		return r.hasLabelLocked(norad, label, bands)
	default:
		return false
	}
}

func (r *Registry) hasLabelLocked(norad int, label string, bands Bands) bool {
	switch label {
	case LabelInBand:
		return r.hasInBandLocked(norad, bands)
	case LabelAlive:
		return r.hasAliveLocked(norad)
	case LabelHeard:
		return false // статистика приёма ещё не подключена
	case LabelStations:
		_, ok := inhabitedStations[norad]
		return ok
	case LabelWeather:
		return r.hasWeatherLocked(norad)
	case LabelInBandAlive:
		return r.hasInBandLocked(norad, bands) && r.hasAliveLocked(norad)
	default:
		return false
	}
}

func (r *Registry) hasInBandLocked(norad int, bands Bands) bool {
	for _, tx := range mergeTransmitters(r.txs[norad]) {
		if bands.Allow(tx.DownlinkHz) {
			return true
		}
	}
	return false
}

func (r *Registry) hasAliveLocked(norad int) bool {
	txs := r.txs[norad]
	if len(txs[SourceCurated]) > 0 {
		return true
	}
	status, _, _ := mergeStatus(r.facts[norad], txs)
	return status == StatusAlive
}

func (r *Registry) hasWeatherLocked(norad int) bool {
	for _, tx := range mergeTransmitters(r.txs[norad]) {
		if isWeatherTransmitter(tx) {
			return true
		}
	}
	return false
}

func isWeatherTransmitter(tx Transmitter) bool {
	if tx.DownlinkHz < weatherDownlinkMinHz || tx.DownlinkHz > weatherDownlinkMaxHz {
		return false
	}
	mode := strings.ToUpper(tx.Mode)
	return strings.Contains(mode, "APT") || strings.Contains(mode, "LRPT")
}

func (r *Registry) knowsLivenessLocked() bool {
	for _, bySrc := range r.txs {
		for _, src := range livenessSources {
			if len(bySrc[src]) > 0 {
				return true
			}
		}
	}
	return false
}
