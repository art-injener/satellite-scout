package bandscan

import (
	"math"
	"slices"
	"strconv"

	"github.com/art-injener/satellite-scout/internal/signalclass"
)

const (
	// HoldMs — сколько участок ещё считается звучащим после последнего появления.
	// Столько журнал визита стоянки считает его присутствием. Пакет между вспышками короче.
	HoldMs = 10_000
	// RememberMs — окно для несформированных участков в cacheHit и для active/черновиков.
	// Уже показанные строки «Эфира» живут до перезапуска (лимит maxSegments).
	RememberMs = 60 * 60 * 1000
	// rememberMinOnMs — короче этого всплеск не передатчик: после HoldMs он из таблицы уходит.
	rememberMinOnMs = 2_000
	// activeMinOnMs — минимальная длительность короткой вспышки для оперативного
	// снимка стоянки (SignalsInBand): короткие пакеты кубсатов длятся от 150 мс.
	activeMinOnMs = 150
	// strongHitSNRDB — порог SNR одиночного решения, достаточный для мгновенного
	// пропуска короткой вспышки в SignalsInBand и очередь классификации.
	strongHitSNRDB = 10
	// rememberPlainHz — шире этого без разбора модуляции участок не держат час:
	// на высоком усилении шум сливается в одну широкую отметку.
	rememberPlainHz = 50_000
	// burstJoinHz — короткие всплески ближе этого по центру считаются одним источником.
	// Дальше канала 12,5 кГц соседа не задевает.
	burstJoinHz = 5_000
	// dopplerJoinHz — один КА после hop: центр мог уйти дальше burstJoinHz из‑за Доплера.
	// Разные NORAD так не склеиваются (см. sameSticky).
	dopplerJoinHz = 30_000
	// maxCacheDriftHzS — предел дрейфа, при котором cacheHit доверяет экстраполяции:
	// реальный Доплер на VHF/UHF до ~150 Гц/с, больший дрейф — гуляющий центроид осколка.
	maxCacheDriftHzS = 500
	// coverMergeMs — накрытие огибающей дольше этого сливает показанные строки-осколки
	// (тоны FSK под общим горбом). Короткое накрытие — мост-вспышка, соседей не трогает.
	coverMergeMs = 3_000
	// confirmHits — решений с превышением, после которых участок показывается: одиночный выброс не сигнал.
	confirmHits = 2
	// hysteresisDB — насколько ниже порога должен упасть уровень, чтобы вспышка кончилась.
	hysteresisDB = 2
	// maxSegments — участков в памяти на тракт; при переполнении уходят давно не появлявшиеся.
	maxSegments = 2000
	// maxStarts — начал вспышек в памяти: 16 интервалов для периода.
	maxStarts = 17
	// maxStepMs — длиннее интервала решение не засчитывается в наблюдение (поток прерывался).
	maxStepMs = 1000

	newObservationMs    = 60_000
	steadyObservationMs = 5 * 60_000
	steadyOnFraction    = 0.9
	periodicMinGaps     = 4
	periodicSpread      = 0.2
	// crystalPPM — ошибка опорника в допуске совпадения: поправка кварца её не обнуляет.
	crystalPPM = 2e-6
	// envelopeMs — за сколько последних появлений берутся края участка: у слабого сигнала
	// склоны на пороге то есть, то нет, и края одного решения дёргаются.
	envelopeMs = 2000
	// centerAlpha — сглаживание частоты участка во времени, доля нового решения.
	centerAlpha = 0.3
)

// Значения origin (ADR-009 §5.4).
const (
	OriginSatellite         = "satellite"
	OriginSatelliteProbable = "satellite_probable"
	OriginTerrestrial       = "terrestrial"
	OriginUnknown           = "unknown"
)

// Значения emission — характер излучения (ADR-009 §5.4). У импульсного с устойчивым
// интервалом между началами вспышек есть stats.period_s.
const (
	EmissionContinuous   = "continuous"
	EmissionPulsed       = "pulsed"
	EmissionUndetermined = "undetermined"
)

// StateActive — участок с энергией. Ожидаемые передатчики без энергии — риски tx_markers в demod_status.
const StateActive = "active"

// Expected — передатчик КА, который сейчас должен звучать на FreqHz (с Доплером).
type Expected struct {
	NoradID int
	UUID    string
	FreqHz  float64
	SatName string // имя КА для колонки «Передатчик» (ADR-009 §11.2)
}

// Signal — участок для SSE band_signals.
type Signal struct {
	ID          string  `json:"id"`
	CenterHz    int64   `json:"center_hz"`
	BandwidthHz int64   `json:"bandwidth_hz"`
	SNRDB       float32 `json:"snr_db"`
	State       string  `json:"state"`
	LastSeenMs  uint64  `json:"last_seen_ms"`
	NoradID     int     `json:"norad_id,omitempty"`
	SatName     string  `json:"sat_name,omitempty"`
	Origin      string  `json:"origin"`
	Emission    string  `json:"emission"`
	Hint        string  `json:"hint,omitempty"`
	// Structure — что за сигнал по структуре (ADR-009 §6.1); nil, пока участок не разобран.
	Structure *signalclass.Result `json:"structure,omitempty"`
	Stats     Stats               `json:"stats"`
	// Приём (кадры / голос): пишет demod через RecordRx, не фронт.
	FramesGood  int    `json:"frames_good,omitempty"`
	LastKind    string `json:"last_kind,omitempty"`
	LastSummary string `json:"last_summary,omitempty"`
	LastDecoder string `json:"last_decoder,omitempty"`
	LastRxMs    uint64 `json:"last_rx_ms,omitempty"`
	// Тело последнего кадра для модалки «Кадр» (ADR-009 §7.2).
	LastSrc     string      `json:"last_src,omitempty"`
	LastDest    string      `json:"last_dest,omitempty"`
	LastInfo    string      `json:"last_info,omitempty"`
	LastInfoHex string      `json:"last_info_hex,omitempty"`
	LastParams  []HearParam `json:"last_params,omitempty"`
	// RxHistory — последние приёмы участка до рестарта (модалка «Кадр»), старые→новые.
	RxHistory []HearRx `json:"rx_history,omitempty"`
}

// Stats — статистика участка по времени (ADR-009 §5.5).
type Stats struct {
	FirstSeenMs uint64  `json:"first_seen_ms"`
	ObservedS   float64 `json:"observed_s"`
	OnS         float64 `json:"on_s"`
	Bursts      int     `json:"bursts"`
	PeriodS     float64 `json:"period_s,omitempty"`
	DriftHzS    float64 `json:"drift_hz_s"`
}

// span — края пиков участка в одном решении.
type span struct {
	ms     uint64
	lo, hi float64
}

type segment struct {
	id int
	// loHz, hiHz — огибающая: края пиков за envelopeMs до последнего появления.
	loHz, hiHz float64
	env        []span
	centerHz   float64 // сглаженный центр тяжести мощности
	binHz      float64
	snr        float32
	hits       int
	on         bool

	firstSeenMs, lastSeenMs uint64
	observedMs, onMs        float64
	bursts                  int
	starts                  []uint64

	// Опорная точка ухода центра: первое появление.
	refMs uint64
	refHz float64

	// Совпавший ожидаемый передатчик и обе частоты в момент совпадения.
	txUUID           string
	txRefHz, segAtTx float64
	// Липкий КА: после hop expected другого окна пуст, имя в «Эфире» не снимаем.
	stickyOrigin string
	stickyNorad  int
	stickyName   string

	// shown — строка уже была в таблице «Эфир»; живёт до перезапуска / eviction.
	shown bool
	// Приём с демодулятора: кадры и последний kind (в т.ч. голос).
	framesGood                         int
	lastKind, lastSummary, lastDecoder string
	lastSrc, lastDest, lastInfo        string
	lastInfoHex                        string
	lastParams                         []HearParam
	lastRxMs                           uint64
	rxHistory                          []HearRx

	// coveredMs — с какого решения огибающая участка непрерывно накрыта огибающей
	// другого показанного участка; 0 — не накрыта. Долгое накрытие — признак осколка
	// чужого сигнала, а не самостоятельного передатчика.
	coveredMs uint64

	// Разбор структуры: когда поставлен последний, последние ответы и показанный.
	classifiedMs uint64
	votes        []signalclass.Result
	structure    *signalclass.Result
	// sstvUntilMs — до этого времени держим service=sstv после VIS (0 — не держали).
	sstvUntilMs uint64
	sstvLabel   string
	// stationUntilMs — до этого держим имя RDS, пока новый разбор без PS.
	stationUntilMs uint64
	stationName    string
	// Сырые сегменты RDS PS: за один разбор редко приходят все четыре.
	rdsParts [4][2]byte
	rdsHave  [4]bool
}

func (s *segment) center() float64 { return s.centerHz }

// coveredLong — участок непрерывно накрыт чужой огибающей дольше coverMergeMs.
func (s *segment) coveredLong(nowMs uint64) bool {
	return s.coveredMs != 0 && nowMs-s.coveredMs >= coverMergeMs
}

// structureForClient — structure с именем RDS, даже если последний vote его стёр.
func (s *segment) structureForClient(nowMs uint64) *signalclass.Result {
	if s.structure == nil {
		return nil
	}
	nSeg := 0
	for _, ok := range s.rdsHave {
		if ok {
			nSeg++
		}
	}
	needSticky := s.stationName != "" && (s.stationUntilMs == 0 || nowMs < s.stationUntilMs)
	needProgress := nSeg > 0 && nSeg < 4 && s.stationName == ""
	if !needSticky && !needProgress {
		return s.structure
	}
	if needSticky && s.structure.Station == s.stationName && !needProgress {
		return s.structure
	}
	out := *s.structure
	if needSticky {
		out.Station = s.stationName
		if out.Service == signalclass.ServiceBroadcast || out.Service == "" {
			out.Service = signalclass.ServiceBroadcast
			out.ServiceLabel = signalclass.LabelBroadcast + ": " + s.stationName
			if out.ServiceBasis == "" {
				out.ServiceBasis = signalclass.BasisSignature
			}
		}
		if out.Demod == "" {
			out.Demod = "wfm"
		}
	} else if needProgress {
		// Пока имя не собрано — в подписи видно, что RDS уже цепляется.
		out.ServiceLabel = signalclass.LabelBroadcast + " (RDS " + strconv.Itoa(nSeg) + "/4)"
	}
	return &out
}

// take переносит в участок его пики решения: огибающая, частота, уровень.
func (s *segment) take(dec Decision, ps []Peak) {
	lo, hi := ps[0].LoHz, ps[0].HiHz
	var w, wf float64
	var snr float32
	for _, p := range ps {
		lo, hi = min(lo, p.LoHz), max(hi, p.HiHz)
		w += p.weight()
		wf += p.weight() * p.CenterHz()
		snr = max(snr, p.SNRDB)
	}
	s.env = append(s.env, span{ms: dec.TimeMs, lo: lo, hi: hi})
	s.env = slices.DeleteFunc(s.env, func(e span) bool { return e.ms+envelopeMs < dec.TimeMs })
	s.envelope()
	if c := wf / w; s.centerHz == 0 {
		s.centerHz = c
	} else {
		s.centerHz += centerAlpha * (c - s.centerHz)
	}
	s.snr, s.binHz = snr, dec.BinHz
	if s.refHz == 0 {
		s.refHz = s.centerHz
	}
}

func (s *segment) envelope() {
	if len(s.env) == 0 {
		return
	}
	s.loHz, s.hiHz = s.env[0].lo, s.env[0].hi
	for _, e := range s.env[1:] {
		s.loHz, s.hiHz = min(s.loHz, e.lo), max(s.hiHz, e.hi)
	}
}

// near — пик или огибающая [lo, hi] задевает огибающую участка с запасом pad.
func (s *segment) near(lo, hi, pad float64) bool {
	return len(s.env) > 0 && lo-pad <= s.hiHz && s.loHz <= hi+pad
}

// Tracker ведёт участки тракта по абсолютной частоте: после перестройки ушедшие за край
// не удаляются, а перестают наблюдаться. Вызывать из одной горутины.
type Tracker struct {
	segs   []*segment
	nextID int
	limit  int
	nowMs  uint64 // время потока последнего решения
	// Годная полоса последнего решения: разбирать можно только участки внутри неё.
	usableLo, usableHi float64
}

// NewTracker создаёт пустой учёт участков.
func NewTracker() *Tracker {
	return &Tracker{limit: maxSegments}
}

// Update переносит решение обнаружителя в участки и их статистику.
func (t *Tracker) Update(dec Decision) {
	t.nowMs = dec.TimeMs
	matched := t.match(dec)
	dt := min(dec.DurationMs, maxStepMs)
	lo, hi := dec.UsableHz()
	t.usableLo, t.usableHi = lo, hi
	for _, s := range t.segs {
		if s.loHz >= lo && s.hiHz <= hi {
			s.observe(dec, dt, matched[s])
		}
	}
	t.trackCovered(matched)
	t.joinCache()
}

// trackCovered ведёт непрерывность накрытия показанных участков огибающими совпавших
// в решении и сливает накрытых дольше coverMergeMs: тон FSK под общим горбом — осколок
// одного сигнала, а не соседний передатчик. Сам участок с энергией накрытым не считается.
func (t *Tracker) trackCovered(matched map[*segment]bool) {
	covered := make(map[*segment]bool)
	for m := range matched {
		if !m.shown {
			continue
		}
		for _, s := range t.segs {
			// Старшая строка осколком младшей не считается: маяк под проезжающим
			// горбом КА должен пережить пролёт.
			if s == m || !s.shown || s.firstSeenMs < m.firstSeenMs || s.loHz < m.loHz || s.hiHz > m.hiHz {
				continue
			}
			covered[s] = true
			if s.coveredMs == 0 {
				s.coveredMs = t.nowMs
			}
		}
	}
	for _, s := range t.segs {
		if !covered[s] || matched[s] {
			s.coveredMs = 0
		}
	}
	for {
		merged := false
		for m := range matched {
			if !m.shown || !slices.Contains(t.segs, m) {
				continue
			}
			for _, s := range t.segs {
				if s != m && s.shown && s.coveredLong(t.nowMs) &&
					s.loHz >= m.loHz && s.hiHz <= m.hiHz {
					t.merge([]*segment{m, s})
					merged = true
					break
				}
			}
			if merged {
				break
			}
		}
		if !merged {
			return
		}
	}
}

// match относит пики решения к участкам. Пик у огибающей участка — тот же сигнал. Пик,
// задевший несколько участков, сливает их в старший: это куски одного сигнала. Пик без участка
// заводит новый. Показанные строки не сливаются — тогда пик достаётся ближайшему по центру:
// старшая строка не должна уносить чужой сигнал, как маяк — горб пролетающего КА.
// После решения сливаются и участки, чьи огибающие сошлись.
func (t *Tracker) match(dec Decision) map[*segment]bool {
	pad := mergeGapBins * dec.BinHz
	parts := make(map[*segment][]Peak, len(dec.Peaks))
	for _, p := range dec.Peaks {
		var touched []*segment
		for _, s := range t.segs {
			if s.near(p.LoHz, p.HiHz, pad) {
				touched = append(touched, s)
			}
		}
		if len(touched) == 0 {
			if cached := t.cacheHit(p); cached != nil {
				touched = append(touched, cached)
			} else {
				touched = append(touched, t.add(dec.TimeMs))
			}
		}
		keep, gone := t.merge(touched)
		for _, g := range gone {
			parts[keep] = append(parts[keep], parts[g]...)
			delete(parts, g)
		}
		owner := keep
		if len(gone) == 0 && len(touched) > 1 {
			owner = peakOwner(touched, p)
		}
		parts[owner] = append(parts[owner], p)
	}
	matched := make(map[*segment]bool, len(parts))
	for s, ps := range parts {
		s.take(dec, ps)
		matched[s] = true
	}
	for t.mergeTouching(matched, pad) {
	}
	return matched
}

// peakOwner — участок, чья огибающая лучше всего совпадает с пиком по форме; при равенстве —
// старший. Узкой строке широкий пик не достаётся, даже если центры совпали: центр сглаженный
// и врёт на лаге сглаживания, а форма огибающей различает «свой» сигнал всегда.
func peakOwner(segs []*segment, p Peak) *segment {
	best := segs[0]
	bestFit := peakFit(best, p)
	for _, s := range segs[1:] {
		if f := peakFit(s, p); f > bestFit || (f == bestFit && preferKeep(s, best)) {
			best, bestFit = s, f
		}
	}
	return best
}

// peakFit — доля пересечения пика с огибающей участка в их объединении (Жаккар).
func peakFit(s *segment, p Peak) float64 {
	inter := min(s.hiHz, p.HiHz) - max(s.loHz, p.LoHz)
	if inter <= 0 {
		return 0
	}
	union := max(s.hiHz, p.HiHz) - min(s.loHz, p.LoHz)
	return inter / union
}

// mergeTouching сливает один участок решения с соседями, чьи огибающие он задел; true — слил.
func (t *Tracker) mergeTouching(matched map[*segment]bool, pad float64) bool {
	for s := range matched {
		touched := []*segment{s}
		for _, o := range t.segs {
			if o != s && o.near(s.loHz, s.hiHz, pad) {
				touched = append(touched, o)
			}
		}
		if len(touched) == 1 {
			continue
		}
		keep, gone := t.merge(touched)
		if len(gone) == 0 {
			continue
		}
		for _, g := range gone {
			delete(matched, g)
		}
		matched[keep] = true
		return true
	}
	return false
}

// merge оставляет одну строку. Уже показанные с разным КА друг в друга не сливаются;
// один stickyNorad (доплер-дубль) — склеивается. Показанные без опознанного КА сливаются,
// если один давно накрыт огибающей другого: это осколки одного сигнала.
// Разнесённые по времени вспышки складывают длительность.
func (t *Tracker) merge(touched []*segment) (*segment, []*segment) {
	keep := touched[0]
	for _, s := range touched[1:] {
		if preferKeep(s, keep) {
			keep = s
		}
	}
	gone := make([]*segment, 0, len(touched)-1)
	for _, s := range touched {
		if s == keep {
			continue
		}
		if keep.shown && s.shown && !sameSticky(keep, s) {
			differentSats := keep.stickyNorad > 0 && s.stickyNorad > 0 && keep.stickyNorad != s.stickyNorad
			if differentSats || (!s.coveredLong(t.nowMs) && !keep.coveredLong(t.nowMs)) {
				continue
			}
		}
		if separateInTime(keep, s) {
			keep.onMs += s.onMs
			keep.observedMs += s.observedMs
			keep.bursts += s.bursts
			keep.starts = append(keep.starts, s.starts...)
			if len(keep.starts) > maxStarts {
				keep.starts = keep.starts[len(keep.starts)-maxStarts:]
			}
		}
		keep.env = append(keep.env, s.env...)
		keep.hits += s.hits
		keep.on = keep.on || s.on
		keep.lastSeenMs = max(keep.lastSeenMs, s.lastSeenMs)
		keep.absorbStructure(s)
		if keep.stickyNorad == 0 && s.stickyNorad > 0 {
			keep.stickyNorad, keep.stickyOrigin, keep.stickyName = s.stickyNorad, s.stickyOrigin, s.stickyName
		} else if keep.stickyName == "" && s.stickyName != "" && keep.stickyNorad == s.stickyNorad {
			keep.stickyName = s.stickyName
		}
		keep.absorbHear(s)
		gone = append(gone, s)
	}
	if len(gone) > 0 {
		keep.envelope()
		t.segs = slices.DeleteFunc(t.segs, func(s *segment) bool { return slices.Contains(gone, s) })
	}
	return keep, gone
}

// sameSticky — один КА: доплер-дубли после hop склеиваем, разные передатчики нет.
func sameSticky(a, b *segment) bool {
	return a.stickyNorad > 0 && a.stickyNorad == b.stickyNorad
}

// separateInTime — вспышки не пересекались по времени, их длительность можно сложить.
func separateInTime(a, b *segment) bool {
	if a.firstSeenMs == 0 || b.firstSeenMs == 0 {
		return false
	}
	return a.lastSeenMs < b.firstSeenMs || b.lastSeenMs < a.firstSeenMs
}

// cacheHit — ближайший уже виденный участок. Без КА центр экстраполируется по дрейфу
// участка за время тишины: после долгого фейда Доплер уносит сигнал дальше burstJoinHz,
// а сходиться с замороженным центром он не обязан. С stickyNorad — dopplerJoinHz.
func (t *Tracker) cacheHit(p Peak) *segment {
	c := (p.LoHz + p.HiHz) / 2
	var best *segment
	bestD := math.Inf(1)
	for _, s := range t.segs {
		if s.centerHz == 0 {
			continue
		}
		aged := t.nowMs > s.lastSeenMs && t.nowMs-s.lastSeenMs > RememberMs
		if aged && !s.shown {
			continue
		}
		limit := float64(burstJoinHz)
		center := s.centerHz
		if s.stickyNorad > 0 {
			limit = dopplerJoinHz
		} else if t.nowMs > s.lastSeenMs {
			gapS := float64(t.nowMs-s.lastSeenMs) / 1000
			if dr := s.drift(); math.Abs(dr) <= maxCacheDriftHzS {
				center += dr * gapS
				limit = min(dopplerJoinHz, limit+math.Abs(dr)*gapS)
			}
		}
		d := math.Abs(center - c)
		if d >= limit {
			continue
		}
		if d < bestD || (d == bestD && best != nil && s.firstSeenMs < best.firstSeenMs) {
			best, bestD = s, d
		}
	}
	return best
}

// joinCache склеивает короткие участки ближе burstJoinHz.
// Два показанных с разным КА не соединяются; один stickyNorad — да (доплер-дубль).
func (t *Tracker) joinCache() {
	for {
		merged := false
		for i := 0; i < len(t.segs); i++ {
			for j := i + 1; j < len(t.segs); j++ {
				a, b := t.segs[i], t.segs[j]
				if a.remembered() && b.remembered() && !sameSticky(a, b) {
					continue
				}
				if a.centerHz == 0 || b.centerHz == 0 {
					continue
				}
				limit := float64(burstJoinHz)
				if sameSticky(a, b) {
					limit = dopplerJoinHz
				}
				if math.Abs(a.centerHz-b.centerHz) > limit {
					continue
				}
				t.merge([]*segment{a, b})
				merged = true
				break
			}
			if merged {
				break
			}
		}
		if !merged {
			return
		}
	}
}

// observe засчитывает участку решение, пока он в годной полосе: вспышка начинается
// с порога и кончается на hysteresisDB ниже него.
func (s *segment) observe(dec Decision, dt float64, matched bool) {
	s.observedMs += dt
	was := s.on
	if matched {
		s.on = true
		s.hits++
	} else {
		s.on = s.on && dec.ExcessIn(s.loHz, s.hiHz) >= ThresholdDB-hysteresisDB
	}
	if !s.on {
		return
	}
	s.onMs += dt
	s.lastSeenMs = dec.TimeMs
	if !was {
		s.bursts++
		s.starts = append(s.starts, dec.TimeMs)
		if len(s.starts) > maxStarts {
			s.starts = s.starts[1:]
		}
	}
}

// preferKeep — pin КА и уже показанная строка важнее, чтобы слияние не меняло номер и приём.
func preferKeep(s, keep *segment) bool {
	if s.pinned() != keep.pinned() {
		return s.pinned()
	}
	if s.shown != keep.shown {
		return s.shown
	}
	if s.firstSeenMs != keep.firstSeenMs {
		return s.firstSeenMs < keep.firstSeenMs
	}
	return s.id < keep.id
}

// remembered — строка в таблице «Эфир». Первый раз решает qualifies;
// дальше остаётся до перезапуска / eviction, даже если полоса разъехалась.
func (s *segment) remembered() bool {
	if s.shown {
		return true
	}
	if !s.qualifies() {
		return false
	}
	s.shown = true
	return true
}

func (s *segment) qualifies() bool {
	if s.onMs < rememberMinOnMs {
		return false
	}
	if s.hiHz-s.loHz <= rememberPlainHz {
		return true
	}
	if s.structure == nil {
		return false
	}
	m := s.structure.Modulation
	return m != "" && m != signalclass.ModUndetermined && m != signalclass.ModUnknown
}

// hitConfirmed — участок подтверждён для оперативной работы и классификации:
// не менее confirmHits решений, одиночный сильный всплеск или закреплённый КА.
func (s *segment) hitConfirmed() bool {
	return s.hits >= confirmHits || (s.hits >= 1 && s.snr >= strongHitSNRDB) || s.pinned()
}

// activeBurst — короткая свежая вспышка для удержания и наведения канала
// без записи в долгоживущую таблицу «Эфир».
func (s *segment) activeBurst() bool {
	if !s.hitConfirmed() || s.onMs < activeMinOnMs {
		return false
	}
	return s.hiHz-s.loHz <= rememberPlainHz
}

func (s *segment) visibleInList(keepShown, aged bool) bool {
	if keepShown {
		if s.hits < confirmHits && !s.pinned() {
			return false
		}
		return s.shown || (!aged && s.remembered())
	}
	if aged && !s.pinned() {
		return false
	}
	return s.shown || s.activeBurst()
}

func (t *Tracker) add(ms uint64) *segment {
	if len(t.segs) >= t.limit {
		oldest := -1
		for i, s := range t.segs {
			if s.pinned() {
				continue
			}
			if oldest < 0 || s.lastSeenMs < t.segs[oldest].lastSeenMs {
				oldest = i
			}
		}
		if oldest >= 0 {
			t.segs = slices.Delete(t.segs, oldest, oldest+1)
		}
	}
	t.nextID++
	s := &segment{id: t.nextID, firstSeenMs: ms, lastSeenMs: ms, refMs: ms}
	t.segs = append(t.segs, s)
	return s
}

// Signals — подтверждённые участки за последний час, по возрастанию частоты.
// expected — ожидаемые передатчики сейчас: по ним решается, КА ли это.
func (t *Tracker) Signals(expected []Expected) []Signal {
	return t.list(expected, RememberMs)
}

// active — участки, которые звучат или замолчали только что. Журнал визита стоянки.
func (t *Tracker) active(expected []Expected) []Signal {
	return t.list(expected, HoldMs)
}

func (t *Tracker) list(expected []Expected, maxAgeMs uint64) []Signal {
	// Signals (RememberMs): показанные до перезапуска. active (HoldMs): только свежие,
	// включая короткие вспышки от activeMinOnMs без записи в память таблицы «Эфир».
	keepShown := maxAgeMs > HoldMs
	var out []Signal
	for _, s := range t.segs {
		aged := t.nowMs > s.lastSeenMs && t.nowMs-s.lastSeenMs > maxAgeMs
		if !s.visibleInList(keepShown, aged) {
			continue
		}
		origin, norad := s.origin(expected)
		var structure *signalclass.Result
		if raw := s.structureForClient(t.nowMs); raw != nil {
			shown := applyOrigin(*raw, origin)
			// Служба могла смениться после голосования, пробы или липких SSTV/RDS:
			// рекомендация демодулятора считается по итоговой строке, а не по голосу.
			shown.Demod, shown.DemodAlt = signalclass.SuggestedDemod(shown)
			structure = &shown
		}
		sig := Signal{
			ID:          "s" + strconv.Itoa(s.id),
			CenterHz:    int64(math.Round(s.center())),
			BandwidthHz: int64(math.Round(s.hiHz - s.loHz)),
			SNRDB:       float32(math.Round(float64(s.snr)*10) / 10),
			State:       StateActive,
			LastSeenMs:  s.lastSeenMs,
			NoradID:     norad,
			SatName:     s.stickyName,
			Origin:      origin,
			Emission:    s.emission(),
			Hint:        bandHint(s.center()),
			Structure:   structure,
			FramesGood:  s.framesGood,
			LastKind:    s.lastKind,
			LastSummary: s.lastSummary,
			LastDecoder: s.lastDecoder,
			LastRxMs:    s.lastRxMs,
			LastSrc:     s.lastSrc,
			LastDest:    s.lastDest,
			LastInfo:    s.lastInfo,
			LastInfoHex: s.lastInfoHex,
			LastParams:  cloneHearParams(s.lastParams),
			RxHistory:   cloneHearHistory(s.rxHistory),
			Stats: Stats{
				FirstSeenMs: s.firstSeenMs,
				ObservedS:   round1(s.observedMs / 1000),
				OnS:         round1(s.onMs / 1000),
				Bursts:      s.bursts,
				DriftHzS:    round1(s.drift()),
			},
		}
		if norad == 0 {
			sig.SatName = ""
		}
		if sig.Emission == EmissionPulsed {
			sig.Stats.PeriodS = round1(s.period())
		}
		out = append(out, sig)
	}
	slices.SortFunc(out, func(a, b Signal) int { return int(a.CenterHz - b.CenterHz) })
	return out
}

// origin: КА — участок совпал с ожидаемым передатчиком и сдвинулся вместе с ним;
// земной — частота стоит дольше минуты, а передатчика рядом нет.
// Нет TX рядом (hop / другой LO): отдаём липкий NORAD последнего удачного матча.
func (s *segment) origin(expected []Expected) (string, int) {
	c := s.center()
	tol := (s.hiHz-s.loHz)/2 + s.binHz + crystalPPM*c
	var tx *Expected
	for i := range expected {
		if d := math.Abs(expected[i].FreqHz - c); d <= tol && (tx == nil || d < math.Abs(tx.FreqHz-c)) {
			tx = &expected[i]
		}
	}
	if tx != nil {
		if o := s.follows(tx, c); o != "" {
			s.stickyOrigin, s.stickyNorad = o, tx.NoradID
			if tx.SatName != "" {
				s.stickyName = tx.SatName
			}
			return o, tx.NoradID
		}
		// Риска рядом, участок не следует. Приём или уже подтверждённый КА не стираем.
		// UUID не трогаем: иначе follows каждый раз обнулит сдвиг Доплера.
		if s.pinned() || s.stickyOrigin == OriginSatellite {
			return s.stickyOrigin, s.stickyNorad
		}
		s.stickyOrigin, s.stickyNorad, s.stickyName = "", 0, ""
	} else if s.stickyNorad > 0 {
		return s.stickyOrigin, s.stickyNorad
	} else {
		s.txUUID = ""
	}
	if s.pinned() {
		return s.stickyOrigin, s.stickyNorad
	}
	if s.observedMs >= newObservationMs && math.Abs(c-s.refHz) < s.binHz {
		s.stickyOrigin, s.stickyNorad, s.stickyName = "", 0, ""
		return OriginTerrestrial, 0
	}
	return OriginUnknown, 0
}

// follows: «КА?», пока расчётная частота не ушла на 2 бина; «КА», если участок сдвинулся вместе
// с ней хотя бы наполовину; пусто, если передатчик ушёл, а участок остался.
func (s *segment) follows(tx *Expected, c float64) string {
	if tx.UUID != s.txUUID {
		s.txUUID, s.txRefHz, s.segAtTx = tx.UUID, tx.FreqHz, c
	}
	txShift := tx.FreqHz - s.txRefHz
	if math.Abs(txShift) < 2*s.binHz {
		return OriginSatelliteProbable
	}
	if math.Abs(c-s.segAtTx-txShift) <= max(s.binHz, math.Abs(txShift)/2) {
		return OriginSatellite
	}
	return ""
}

// emission: непрерывный — в эфире почти всё время наблюдения не меньше 5 минут; импульсный —
// вспышки. До минуты наблюдения и у постоянного сигнала до 5 минут — не определён.
func (s *segment) emission() string {
	if s.observedMs < newObservationMs {
		return EmissionUndetermined
	}
	if s.onMs >= steadyOnFraction*s.observedMs {
		if s.observedMs >= steadyObservationMs {
			return EmissionContinuous
		}
		return EmissionUndetermined
	}
	return EmissionPulsed
}

// period — медиана интервалов между началами вспышек, если разброс в пределах 20 %; иначе 0.
func (s *segment) period() float64 {
	if len(s.starts) < periodicMinGaps+1 {
		return 0
	}
	gaps := make([]float64, len(s.starts)-1)
	for i := range gaps {
		gaps[i] = float64(s.starts[i+1]-s.starts[i]) / 1000
	}
	m := median(slices.Clone(gaps))
	for _, g := range gaps {
		if math.Abs(g-m) > periodicSpread*m {
			return 0
		}
	}
	return m
}

// drift — уход центра от первого появления, Гц/с.
func (s *segment) drift() float64 {
	if s.lastSeenMs <= s.refMs {
		return 0
	}
	return (s.center() - s.refHz) / (float64(s.lastSeenMs-s.refMs) / 1000)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// bandPlan — места в частотном плане; узкие участки раньше широких, в которые они входят.
var bandPlan = []struct {
	loHz, hiHz float64
	label      string
}{
	{433_075_000, 434_790_000, "ЛПД 433"},
	{446_000_000, 446_200_000, "PMR 446"},
	{87_500_000, 108_000_000, "Вещ. ЧМ"},
	{137_000_000, 138_000_000, "Метео"},
	{400_150_000, 406_000_000, "Радиозонд"},
	{144_000_000, 146_000_000, "Люб. 2 м"},
	{430_000_000, 440_000_000, "Люб. 70 см"},
}

// bandHint — подпись места в частотном плане; пусто вне известных участков.
func bandHint(hz float64) string {
	for _, b := range bandPlan {
		if hz >= b.loHz && hz <= b.hiHz {
			return b.label
		}
	}
	return ""
}
