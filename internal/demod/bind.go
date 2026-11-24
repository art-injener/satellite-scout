package demod

import (
	"strings"
	"time"

	"github.com/art-injener/satellite-scout/internal/formats"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// Источник привязки кадра к КА (ADR-009 §6.3); первое сработавшее побеждает.
const (
	SatSourceCallsign = "callsign"
	SatSourceFreqPlan = "freq_plan"
	SatSourceSigMF    = "sigmf"
	SatSourceReplay   = "replay"
)

// SatBinding — результат привязки кадра к КА.
type SatBinding struct {
	NoradID int
	SatName string
	Source  string
}

// CallsignIndex — поиск КА по позывному из полезной нагрузки (карточки каталога).
type CallsignIndex interface {
	LookupCallsign(call string) (norad int, name string, ok bool)
}

// PathSatHint — подсказка КА для тракта replay: из SigMF или из конфига.
type PathSatHint struct {
	NoradID int
	SatName string
	Source  string // sigmf | replay
}

// PathSatSource — NORAD/имя, закреплённые за трактом (replay).
type PathSatSource interface {
	Hint(pathID string) (PathSatHint, bool)
}

// PathSatMap — подсказки КА по id тракта.
type PathSatMap map[string]PathSatHint

// Hint — реализация PathSatSource.
func (m PathSatMap) Hint(pathID string) (PathSatHint, bool) {
	if m == nil {
		return PathSatHint{}, false
	}
	h, ok := m[pathID]
	if !ok || h.NoradID <= 0 {
		return PathSatHint{}, false
	}
	return h, true
}

// NameSource — отображаемое имя КА по NORAD (TLE / группа сопровождения).
type NameSource interface {
	SatName(noradID int) string
}

// HorizonSource — КА сейчас над горизонтом (План сеансов).
type HorizonSource interface {
	IsVisible(noradID int) bool
}

// ResolveInput — вход резолвера привязки кадра.
type ResolveInput struct {
	SrcCall      string
	DestCall     string
	FrequencyHz  uint64
	BandwidthHz  uint32
	Now          time.Time
	Transmitters []ExpectedTransmitter
	Doppler      DopplerSource
	Horizon      HorizonSource
	Callsigns    CallsignIndex
	Path         PathSatHint // Source пуст — подсказки тракта нет
	Names        NameSource
}

// ResolveSat выбирает КА кадра по цепочке ADR-009 §6.3.
func ResolveSat(in ResolveInput) SatBinding {
	if b, ok := bindCallsign(in); ok {
		return fillName(b, in.Names)
	}
	if b, ok := bindFreqPlan(in); ok {
		return fillName(b, in.Names)
	}
	if in.Path.NoradID > 0 && (in.Path.Source == SatSourceSigMF || in.Path.Source == SatSourceReplay) {
		return fillName(SatBinding{
			NoradID: in.Path.NoradID,
			SatName: in.Path.SatName,
			Source:  in.Path.Source,
		}, in.Names)
	}
	return SatBinding{}
}

func bindCallsign(in ResolveInput) (SatBinding, bool) {
	if in.Callsigns == nil {
		return SatBinding{}, false
	}
	for _, call := range []string{in.SrcCall, in.DestCall} {
		norad, name, ok := in.Callsigns.LookupCallsign(call)
		if !ok || norad <= 0 {
			continue
		}
		return SatBinding{NoradID: norad, SatName: name, Source: SatSourceCallsign}, true
	}
	return SatBinding{}, false
}

func bindFreqPlan(in ResolveInput) (SatBinding, bool) {
	if in.Doppler == nil || len(in.Transmitters) == 0 || in.FrequencyHz == 0 {
		return SatBinding{}, false
	}
	half := float64(in.BandwidthHz) / 2
	if half <= 0 {
		half = 10_000
	}
	center := float64(in.FrequencyHz)
	lo, hi := center-half, center+half

	rates := make(map[int]*float64, len(in.Transmitters))
	var hitNorad int
	var hits int
	for _, tx := range in.Transmitters {
		if tx.NoradID <= 0 {
			continue
		}
		if in.Horizon != nil && !in.Horizon.IsVisible(tx.NoradID) {
			continue
		}
		rr, seen := rates[tx.NoradID]
		if !seen {
			if v, ok := in.Doppler.RangeRateKmps(tx.NoradID, in.Now); ok {
				rr = &v
			}
			rates[tx.NoradID] = rr
		}
		if rr == nil {
			continue
		}
		f := float64(tx.FreqHz) + tracker.DopplerShiftHz(float64(tx.FreqHz), *rr)
		if f < lo || f > hi {
			continue
		}
		if hits == 0 {
			hitNorad = tx.NoradID
			hits = 1
			continue
		}
		if tx.NoradID != hitNorad {
			return SatBinding{}, false // несколько разных КА в допуске — не гадаем
		}
	}
	if hits != 1 {
		return SatBinding{}, false
	}
	return SatBinding{NoradID: hitNorad, Source: SatSourceFreqPlan}, true
}

func fillName(b SatBinding, names NameSource) SatBinding {
	if b.NoradID <= 0 {
		return b
	}
	if strings.TrimSpace(b.SatName) == "" && names != nil {
		b.SatName = names.SatName(b.NoradID)
	}
	return b
}

// NormalizeCallsign реэкспорт для тестов демода; канон — formats.
func NormalizeCallsign(s string) string {
	return formats.NormalizeCallsign(s)
}
