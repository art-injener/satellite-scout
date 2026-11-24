package demod

import (
	"slices"
)

// DemodAuto — профиль «Авто»: параллельный подбор кадрового демодулятора по CRC.
const DemodAuto = "auto"

// TXHint — режим и символьная скорость одного передатчика из реестра.
type TXHint struct {
	Mode string
	Baud float64
}

// CandidateHints — подсказки для списка кандидатов (радиопрофиль, карточка, реестр).
type CandidateHints struct {
	Accepted    string // принято по CRC (радиопрофиль)
	CardProfile string
	Mode        string
	Baud        float64
	TXHints     []TXHint // все передатчики этого КА на близкой частоте
	AirMode     string   // модуляция от классификатора (не входит в ключ памяти)
	AirBaud     float64
	Blind       string
	BlindAlt    []string
	Max         int // 0 — все реализованные кадровые профили без ограничения
}

// BuildCandidates собирает id по приоритету: принято, обнаружено, карточка,
// заявлено по всем передатчикам на частоте, затем остальные кадровые профили.
// Некадровые профили (sstv, cw) добавляются только при наличии явной подсказки.
func BuildCandidates(h CandidateHints) []string {
	limit := h.Max
	var out []string
	add := func(allowNonFrame bool, ids ...string) {
		for _, id := range ids {
			if limit > 0 && len(out) >= limit {
				return
			}
			if id == "" || id == DemodAuto {
				continue
			}
			if !allowedAutoCandidate(id, allowNonFrame) {
				continue
			}
			if slices.Contains(out, id) {
				continue
			}
			out = append(out, id)
		}
	}
	add(true, h.Accepted)
	add(true, h.Blind)
	add(true, h.BlindAlt...)
	add(true, h.CardProfile)
	add(true, Candidates(h.Mode, h.Baud)...)
	for _, tx := range h.TXHints {
		add(true, Candidates(tx.Mode, tx.Baud)...)
	}
	add(true, Candidates(h.AirMode, h.AirBaud)...)
	add(false, ImplementedFrameProfiles()...)
	return out
}

// isAutoNonFrameProfile — некадровые профили, которые режим «Авто» может подобрать по подсказке.
func isAutoNonFrameProfile(id string) bool {
	return id == DemodSSTV || id == DemodCW
}

func allowedAutoCandidate(id string, allowNonFrame bool) bool {
	return isFrameProfile(id) || (allowNonFrame && isAutoNonFrameProfile(id))
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

// MatchTXModeBauds возвращает режимы и скорости всех передатчиков данного КА
// в пределах допуска частоты, упорядоченные по близости к freqHz.
func MatchTXModeBauds(txs []ExpectedTransmitter, norad int, freqHz uint64) []TXHint {
	type matched struct {
		hint  TXHint
		delta uint64
	}
	var list []matched
	for _, tx := range txs {
		if tx.NoradID != norad || tx.Mode == "" {
			continue
		}
		delta := absDiffU64(uint64(tx.FreqHz), freqHz) //nolint:gosec // частоты TX > 0
		if delta > txMatchMaxHz {
			continue
		}
		list = append(list, matched{
			hint:  TXHint{Mode: tx.Mode, Baud: tx.Baud},
			delta: delta,
		})
	}
	if len(list) == 0 {
		return nil
	}
	slices.SortStableFunc(list, func(a, b matched) int {
		switch {
		case a.delta < b.delta:
			return -1
		case a.delta > b.delta:
			return 1
		default:
			return 0
		}
	})
	out := make([]TXHint, 0, len(list))
	for _, m := range list {
		if !slices.Contains(out, m.hint) {
			out = append(out, m.hint)
		}
	}
	return out
}

// MatchTXModeBaud — режим и скорость ближайшего передатчика данного КА на частоте канала.
func MatchTXModeBaud(txs []ExpectedTransmitter, norad int, freqHz uint64) (string, float64, bool) {
	hints := MatchTXModeBauds(txs, norad, freqHz)
	if len(hints) == 0 {
		return "", 0, false
	}
	return hints[0].Mode, hints[0].Baud, true
}

func absDiffU64(a, b uint64) uint64 {
	if a >= b {
		return a - b
	}
	return b - a
}
