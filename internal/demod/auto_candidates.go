package demod

import (
	"slices"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

// DemodAuto — профиль «Авто»: параллельный подбор кадрового демодулятора по CRC,
// а по подсказке также SSTV и CW. Голосовые профили доступны только в ручном режиме.
const DemodAuto = "auto"

// TXHint — режим и символьная скорость одного передатчика из реестра.
type TXHint struct {
	Mode string
	Baud float64
}

// CandidateHints — подсказки для списка кандидатов (радиопрофиль, карточка, реестр).
type CandidateHints struct {
	KnownSat    bool   // true — приём известного КА (DopplerNorad > 0)
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

// CandidatePlan — двухэтапный план подбора: сначала целевые кандидаты, затем (при отсутствии
// CRC на активном сигнале) расширение на семейство той же модуляции.
type CandidatePlan struct {
	Initial  []string
	Expanded []string
}

// BuildCandidates собирает полный список кандидатов по приоритету:
// принято, карточка, заявлено по всем передатчикам на частоте, обнаружено,
// затем остальные кадровые профили.
// Некадровые профили (sstv, cw) добавляются только при наличии явной подсказки.
// Голосовые профили (fm, am, usb, lsb, wfm) в режим «Авто» не входят.
func BuildCandidates(h CandidateHints) []string {
	out := collectHintedCandidates(h)
	return appendFrameProfiles(out, ImplementedFrameProfiles(), h.Max)
}

// BuildCandidatePlan строит поэтапный план подбора.
// Для известного КА с данными сначала проверяются только его кандидаты; при длительном
// активном сигнале без CRC список расширяется на семейство той же модуляции.
// Для неизвестного Эфира или КА без данных сразу идёт полный перебор кадровых профилей.
func BuildCandidatePlan(h CandidateHints) CandidatePlan {
	hinted := collectHintedCandidates(h)
	if !h.KnownSat || len(hinted) == 0 {
		return CandidatePlan{Initial: appendFrameProfiles(hinted, ImplementedFrameProfiles(), h.Max)}
	}
	expanded := expandSameFamily(hinted, h.Max)
	if len(expanded) <= len(hinted) {
		return CandidatePlan{Initial: hinted}
	}
	return CandidatePlan{Initial: hinted, Expanded: expanded}
}

func collectHintedCandidates(h CandidateHints) []string {
	limit := h.Max
	var out []string
	add := func(ids ...string) {
		for _, id := range ids {
			if limit > 0 && len(out) >= limit {
				return
			}
			if id == "" || id == DemodAuto {
				continue
			}
			if !allowedAutoCandidate(id) {
				continue
			}
			if slices.Contains(out, id) {
				continue
			}
			out = append(out, id)
		}
	}
	add(h.Accepted)
	add(h.CardProfile)
	add(Candidates(h.Mode, h.Baud)...)
	for _, tx := range h.TXHints {
		add(Candidates(tx.Mode, tx.Baud)...)
	}
	add(h.Blind)
	add(h.BlindAlt...)
	add(Candidates(h.AirMode, h.AirBaud)...)
	return out
}

func appendFrameProfiles(base, extra []string, limit int) []string {
	out := append([]string(nil), base...)
	for _, id := range extra {
		if limit > 0 && len(out) >= limit {
			return out
		}
		if !isFrameProfile(id) || slices.Contains(out, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// expandSameFamily дополняет целевых кандидатов КА всеми кадровыми профилями
// тех же физических семейств модуляции (например FSK/GFSK/GMSK или BPSK).
func expandSameFamily(base []string, limit int) []string {
	families := make(map[string]bool, len(base))
	for _, id := range base {
		if !isFrameProfile(id) {
			continue
		}
		if fam := modulationFamilyGroup(id); fam != "" {
			families[fam] = true
		}
	}
	if len(families) == 0 {
		return nil
	}
	var extra []string
	for _, id := range ImplementedFrameProfiles() {
		if families[modulationFamilyGroup(id)] {
			extra = append(extra, id)
		}
	}
	return appendFrameProfiles(base, extra, limit)
}

const (
	familyGroupFSK = "FSK"
	familyGFSK     = "GFSK"
	familyGMSK     = "GMSK"
	familyMSK      = "MSK"
)

func modulationFamilyGroup(id string) string {
	m, ok := Meta(id)
	if !ok {
		return ""
	}
	switch m.Family {
	case familyGroupFSK, familyGFSK, familyGMSK, familyMSK:
		return familyGroupFSK
	default:
		return m.Family
	}
}

// autoBandwidthFor подбирает оптимальную ширину канала под активных кандидатов «Авто».
// Для узких кандидатов (до 4800 бод) сужает полосу до 15 кГц вместо 18 кГц;
// для одиночного CW держит запас 3 кГц на остаток Доплера.
func autoBandwidthFor(candidates []string, fallback uint32) uint32 {
	var maxBW uint32
	for _, id := range candidates {
		m, ok := Meta(id)
		if !ok || m.DefaultBandwidthHz == 0 {
			continue
		}
		bw := m.DefaultBandwidthHz
		if id == DemodCW && bw < 3_000 {
			bw = 3_000
		}
		if bw > maxBW {
			maxBW = bw
		}
	}
	if maxBW == 0 {
		return fallback
	}
	if maxBW > sdr.MaxChannelBandwidthHz {
		maxBW = sdr.MaxChannelBandwidthHz
	}
	return maxBW
}

// isAutoNonFrameProfile — некадровые профили, которые режим «Авто» может подобрать по подсказке.
func isAutoNonFrameProfile(id string) bool {
	return id == DemodSSTV || id == DemodCW
}

func allowedAutoCandidate(id string) bool {
	return isFrameProfile(id) || isAutoNonFrameProfile(id)
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
