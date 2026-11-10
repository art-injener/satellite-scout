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
	// RememberMs — сколько таблица «Эфир» хранит участок после последнего появления.
	// Новый проход обновляет ту же строку, а не заводит её заново.
	RememberMs = 60 * 60 * 1000
	// rememberMinOnMs — короче этого всплеск не передатчик: после HoldMs он из таблицы уходит.
	rememberMinOnMs = 2_000
	// rememberPlainHz — шире этого без разбора модуляции участок не держат час:
	// на высоком усилении шум сливается в одну широкую отметку.
	rememberPlainHz = 50_000
	// burstJoinHz — короткие всплески ближе этого по центру считаются одним источником.
	// Дальше канала 12,5 кГц соседа не задевает.
	burstJoinHz = 5_000
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
	Origin      string  `json:"origin"`
	Emission    string  `json:"emission"`
	Hint        string  `json:"hint,omitempty"`
	// Structure — что за сигнал по структуре (ADR-009 §6.1); nil, пока участок не разобран.
	Structure *signalclass.Result `json:"structure,omitempty"`
	Stats     Stats               `json:"stats"`
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

	// shown — строка уже была в таблице. Пока не прошёл час, её не снимают.
	shown bool

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
	t.joinCache()
}

// match относит пики решения к участкам. Пик у огибающей участка — тот же сигнал. Пик,
// задевший несколько участков, сливает их в старший: это куски одного сигнала. Пик без участка
// заводит новый. После решения сливаются и участки, чьи огибающие сошлись.
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
		parts[keep] = append(parts[keep], p)
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

// merge оставляет одну строку. Уже показанные строки друг в друга не сливаются.
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
		if s == keep || (keep.shown && s.shown) {
			continue
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
		if keep.structure == nil && s.structure != nil {
			keep.structure = s.structure
			keep.votes = append(keep.votes, s.votes...)
		}
		gone = append(gone, s)
	}
	if len(gone) > 0 {
		keep.envelope()
		t.segs = slices.DeleteFunc(t.segs, func(s *segment) bool { return slices.Contains(gone, s) })
	}
	return keep, gone
}

// separateInTime — вспышки не пересекались по времени, их длительность можно сложить.
func separateInTime(a, b *segment) bool {
	if a.firstSeenMs == 0 || b.firstSeenMs == 0 {
		return false
	}
	return a.lastSeenMs < b.firstSeenMs || b.lastSeenMs < a.firstSeenMs
}

// cacheHit — ближайший уже виденный участок в пределах burstJoinHz.
// Огибающая могла остыть, а центр тот же источник.
func (t *Tracker) cacheHit(p Peak) *segment {
	c := (p.LoHz + p.HiHz) / 2
	var best *segment
	bestD := float64(burstJoinHz)
	for _, s := range t.segs {
		if s.centerHz == 0 || t.nowMs-s.lastSeenMs > RememberMs {
			continue
		}
		d := math.Abs(s.centerHz - c)
		if d < bestD || (d == bestD && best != nil && s.firstSeenMs < best.firstSeenMs) {
			best, bestD = s, d
		}
	}
	return best
}

// joinCache склеивает короткие участки, чьи центры ближе burstJoinHz.
// Два уже показанных передатчика так не соединяются.
func (t *Tracker) joinCache() {
	for {
		merged := false
		for i := 0; i < len(t.segs); i++ {
			for j := i + 1; j < len(t.segs); j++ {
				a, b := t.segs[i], t.segs[j]
				if a.remembered() && b.remembered() {
					continue
				}
				if a.centerHz == 0 || b.centerHz == 0 {
					continue
				}
				if math.Abs(a.centerHz-b.centerHz) > burstJoinHz {
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

// preferKeep — уже показанная строка важнее более ранней, чтобы слияние не меняло её номер.
func preferKeep(s, keep *segment) bool {
	if s.shown != keep.shown {
		return s.shown
	}
	if s.firstSeenMs != keep.firstSeenMs {
		return s.firstSeenMs < keep.firstSeenMs
	}
	return s.id < keep.id
}

// remembered — строка в часовой таблице. Первый раз решает qualifies,
// дальше строка остаётся, даже если полоса разъехалась.
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

func (t *Tracker) add(ms uint64) *segment {
	if len(t.segs) >= t.limit {
		oldest := 0
		for i, s := range t.segs {
			if s.lastSeenMs < t.segs[oldest].lastSeenMs {
				oldest = i
			}
		}
		t.segs = slices.Delete(t.segs, oldest, oldest+1)
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
	var out []Signal
	for _, s := range t.segs {
		if s.hits < confirmHits || t.nowMs-s.lastSeenMs > maxAgeMs || !s.remembered() {
			continue
		}
		origin, norad := s.origin(expected)
		var structure *signalclass.Result
		if raw := s.structureForClient(t.nowMs); raw != nil {
			shown := applyOrigin(*raw, origin)
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
			Origin:      origin,
			Emission:    s.emission(),
			Hint:        bandHint(s.center()),
			Structure:   structure,
			Stats: Stats{
				FirstSeenMs: s.firstSeenMs,
				ObservedS:   round1(s.observedMs / 1000),
				OnS:         round1(s.onMs / 1000),
				Bursts:      s.bursts,
				DriftHzS:    round1(s.drift()),
			},
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
func (s *segment) origin(expected []Expected) (string, int) {
	c := s.center()
	tol := (s.hiHz-s.loHz)/2 + s.binHz + crystalPPM*c
	var tx *Expected
	for i := range expected {
		if d := math.Abs(expected[i].FreqHz - c); d <= tol && (tx == nil || d < math.Abs(tx.FreqHz-c)) {
			tx = &expected[i]
		}
	}
	if tx == nil {
		s.txUUID = ""
	} else if o := s.follows(tx, c); o != "" {
		return o, tx.NoradID
	}
	if s.observedMs >= newObservationMs && math.Abs(c-s.refHz) < s.binHz {
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
