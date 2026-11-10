package main

import (
	"math"
	"slices"
	"strconv"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/services"
)

// planAugment — TransmitterProvider: SatNOGS (или иной base) плюс частоты из плана.
// Лента панорамы и маркеры/bandscan берут GetTransmitters — план попадает туда же.
type planAugment struct {
	base services.TransmitterProvider
	plan func() []freqplan.Entry
}

func (p *planAugment) GetPrimaryTransmitter(noradID int) *services.TransmitterInfo {
	if p.base != nil {
		if tx := p.base.GetPrimaryTransmitter(noradID); tx != nil {
			return tx
		}
	}
	list := p.GetTransmitters(noradID)
	if len(list) == 0 {
		return nil
	}
	tx := list[0]
	return &tx
}

func (p *planAugment) GetTransmitters(noradID int) []services.TransmitterInfo {
	var base []services.TransmitterInfo
	if p.base != nil {
		base = p.base.GetTransmitters(noradID)
	}
	var plan []freqplan.Entry
	if p.plan != nil {
		plan = p.plan()
	}
	return mergePlanTX(noradID, base, plan)
}

func (p *planAugment) RequestFetch(noradIDs []int) {
	if p.base != nil {
		p.base.RequestFetch(noradIDs)
	}
}

// mergePlanTX добавляет к base частоты плана для norad (допуск 5 кГц), сортирует по частоте.
func mergePlanTX(noradID int, base []services.TransmitterInfo, plan []freqplan.Entry) []services.TransmitterInfo {
	out := slices.Clone(base)
	for _, e := range plan {
		if e.NoradID != noradID || e.FreqHz <= 0 {
			continue
		}
		if coveredTX(out, e.FreqHz) {
			continue
		}
		out = append(out, services.TransmitterInfo{
			UUID:        planTXUUID(e),
			FreqHz:      e.FreqHz,
			Mode:        e.Mode,
			Modulation:  e.Mode,
			Baud:        e.Baud,
			Description: e.Name,
		})
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b services.TransmitterInfo) int {
		switch {
		case a.FreqHz < b.FreqHz:
			return -1
		case a.FreqHz > b.FreqHz:
			return 1
		default:
			return 0
		}
	})
	for i := range out {
		out[i].Count = len(out)
	}
	return out
}

func coveredTX(have []services.TransmitterInfo, freqHz int64) bool {
	for _, tx := range have {
		if math.Abs(float64(tx.FreqHz-freqHz)) <= 5_000 {
			return true
		}
	}
	return false
}

func planTXUUID(e freqplan.Entry) string {
	if e.UUID != "" {
		return e.UUID
	}
	return "plan:" + strconv.Itoa(e.NoradID) + ":" + strconv.FormatInt(e.FreqHz, 10)
}
