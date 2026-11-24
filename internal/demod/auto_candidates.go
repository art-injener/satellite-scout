package demod

import (
	"slices"
)

// DemodAuto — профиль «Авто»: параллельный подбор кадрового демодулятора по CRC.
const DemodAuto = "auto"

// CandidateHints — подсказки для списка кандидатов (радиопрофиль, карточка, реестр).
type CandidateHints struct {
	Accepted    string // принято по CRC (радиопрофиль)
	CardProfile string
	Mode        string
	Baud        float64
	Blind       string
	BlindAlt    []string
	Max         int // 0 — пять кандидатов
}

// BuildCandidates собирает id по приоритету ADR-012: принято → обнаружено →
// карточка → заявлено (mode/baud) → запасной список кадровых профилей.
func BuildCandidates(h CandidateHints) []string {
	max := h.Max
	if max <= 0 {
		max = 5
	}
	var out []string
	add := func(ids ...string) {
		for _, id := range ids {
			if id == "" || id == DemodAuto {
				continue
			}
			if !isFrameProfile(id) {
				continue
			}
			if slices.Contains(out, id) {
				continue
			}
			out = append(out, id)
			if len(out) >= max {
				return
			}
		}
	}
	add(h.Accepted)
	add(h.Blind)
	add(h.BlindAlt...)
	add(h.CardProfile)
	add(Candidates(h.Mode, h.Baud)...)
	add(ImplementedFrameProfiles()...)
	return out
}

// ImplementedFrameProfiles — профили с выделителем кадров в цепочке (порядок метаданных).
func ImplementedFrameProfiles() []string {
	var out []string
	for _, m := range ListMeta() {
		if isFrameProfile(m.ID) {
			out = append(out, m.ID)
		}
	}
	return out
}

func isFrameProfile(id string) bool {
	p, ok := profiles[id]
	return ok && p.newDeframer != nil
}

// txMatchMaxHz — максимальное |Δf| при выборе TX для mode/baud.
const txMatchMaxHz = 25_000

// MatchTXModeBaud — режим и скорость локального передатчика, ближайшего к частоте канала.
func MatchTXModeBaud(txs []ExpectedTransmitter, norad int, freqHz uint64) (mode string, baud float64, ok bool) {
	var best ExpectedTransmitter
	var bestDelta uint64
	found := false
	for _, tx := range txs {
		if tx.NoradID != norad || tx.Mode == "" {
			continue
		}
		delta := absDiffU64(uint64(tx.FreqHz), freqHz)
		if delta > txMatchMaxHz {
			continue
		}
		if !found || delta < bestDelta {
			best = tx
			bestDelta = delta
			found = true
		}
	}
	if !found {
		return "", 0, false
	}
	return best.Mode, best.Baud, true
}

func absDiffU64(a, b uint64) uint64 {
	if a >= b {
		return a - b
	}
	return b - a
}
