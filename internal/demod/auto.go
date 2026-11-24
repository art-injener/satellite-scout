package demod

import (
	"strconv"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

// Окна автоподбора по умолчанию.
const (
	autoWinDefault    = 30 * time.Second // два кадра с CRC за это окно
	autoReviewDefault = 30 * time.Second // без кадров после закрепления — снова подбор
	autoNeedGood      = 2
)

// AutoPicker — параллельный подбор кадрового профиля на одном канале.
// Успешный выбор помнится по ключу TX/КА: следующий заход сразу «Выбран»,
// снова проба — только если закреплённый профиль не дал кадров.
type AutoPicker struct {
	window time.Duration
	review time.Duration
	now    func() time.Time

	key        string
	candidates []string
	chains     map[string]*Chain
	hits       map[string][]time.Time
	pinned     string
	lastGood   time.Time
	memory     map[string]string // ключ условий → закреплённый профиль
}

// NewAutoPicker создаёт подборщик с окнами по умолчанию.
func NewAutoPicker() *AutoPicker {
	return &AutoPicker{
		window: autoWinDefault,
		review: autoReviewDefault,
		now:    time.Now,
		chains: make(map[string]*Chain),
		hits:   make(map[string][]time.Time),
		memory: make(map[string]string),
	}
}

// SeedMemory задаёт закрепление из радиопрофиля (слой «принято»), чтобы pin
// переживал перезапуск сервера. Не затирает уже выбранный в этой сессии профиль.
func (a *AutoPicker) SeedMemory(key, id string) {
	if a == nil || key == "" || id == "" {
		return
	}
	if a.memory == nil {
		a.memory = make(map[string]string)
	}
	if a.memory[key] != "" {
		return
	}
	a.memory[key] = id
}

// Configure задаёт список кандидатов и ключ условий (NORAD, режим TX, …).
// Смена ключа не забывает прежний выбор: он лежит в memory и восстанавливается
// при возврате. Состав кандидатов обновляется; закрепление с прошлого захода
// остаётся, если профиль ещё в списке.
func (a *AutoPicker) Configure(key string, candidates []string) {
	if a == nil {
		return
	}
	if a.memory == nil {
		a.memory = make(map[string]string)
	}
	sameKey := key == a.key
	sameCand := slicesEqual(candidates, a.candidates)
	if sameKey && sameCand {
		return
	}
	if a.key != "" && a.pinned != "" {
		a.remember(a.key, a.pinned)
	}
	a.key = key
	a.candidates = append([]string(nil), candidates...)
	a.chains = make(map[string]*Chain)
	a.hits = make(map[string][]time.Time)
	for _, id := range a.candidates {
		a.chains[id] = NewChain()
	}
	a.pinned = ""
	a.lastGood = time.Time{}
	if mem := a.memory[key]; mem != "" && candidateHas(a.candidates, mem) {
		a.pinned = mem
		a.lastGood = a.now() // свежий заход: не считать простой с прошлого hop
	}
}

// Locked — закреплённый профиль; пусто — ещё подбор.
func (a *AutoPicker) Locked() string {
	if a == nil {
		return ""
	}
	return a.pinned
}

// Candidates — текущий список id.
func (a *AutoPicker) Candidates() []string {
	if a == nil {
		return nil
	}
	return append([]string(nil), a.candidates...)
}

type taggedPacket struct {
	Profile string
	Packet  Packet
}

// Process гоняет кандидатов (или только закреплённый) по одному блоку IQ.
func (a *AutoPicker) Process(f sdr.IQFrame, base sdr.ChannelSettings, dopplerHz float64) (pkts []taggedPacket, st Status) {
	if a == nil || len(a.candidates) == 0 {
		st.Demod = DemodAuto
		st.Supported = true
		st.AutoPhase = "search"
		return nil, st
	}
	now := a.now()
	if a.pinned != "" {
		return a.processPinned(f, base, dopplerHz, now)
	}
	return a.processSearch(f, base, dopplerHz, now)
}

func (a *AutoPicker) processPinned(f sdr.IQFrame, base sdr.ChannelSettings, dopplerHz float64, now time.Time) ([]taggedPacket, Status) {
	ch := a.chain(a.pinned)
	cs := base
	cs.Demod = a.pinned
	ch.SetDopplerHz(dopplerHz)
	var out []taggedPacket
	for _, p := range ch.Process(f, cs) {
		out = append(out, taggedPacket{Profile: a.pinned, Packet: p})
		a.lastGood = now
	}
	st := ch.Status()
	fillAutoStatus(&st, a.pinned, "locked", a.candidates)
	if !a.lastGood.IsZero() && now.Sub(a.lastGood) >= a.review {
		a.forget(a.key)
		a.unpin()
		fillAutoStatus(&st, "", "search", a.candidates)
	}
	return out, st
}

func (a *AutoPicker) processSearch(f sdr.IQFrame, base sdr.ChannelSettings, dopplerHz float64, now time.Time) ([]taggedPacket, Status) {
	var out []taggedPacket
	var best Status
	have := false
	for _, id := range a.candidates {
		ch := a.chain(id)
		cs := base
		cs.Demod = id
		ch.SetDopplerHz(dopplerHz)
		for _, p := range ch.Process(f, cs) {
			out = append(out, taggedPacket{Profile: id, Packet: p})
			a.noteHit(id, now)
		}
		st := ch.Status()
		if !have || st.SquelchOpen {
			best = st
			have = true
		}
		if a.hitsInWindow(id, now) >= autoNeedGood {
			a.pinned = id
			a.lastGood = now
			a.remember(a.key, id)
			fillAutoStatus(&st, id, "locked", a.candidates)
			return out, st
		}
	}
	fillAutoStatus(&best, "", "search", a.candidates)
	return out, best
}

func fillAutoStatus(st *Status, locked, phase string, candidates []string) {
	st.Demod = DemodAuto
	st.Supported = true
	st.AutoLocked = locked
	st.AutoPhase = phase
	st.AutoCandidates = len(candidates)
	st.AutoIDs = append([]string(nil), candidates...)
}

func (a *AutoPicker) noteHit(id string, now time.Time) {
	a.hits[id] = append(a.hits[id], now)
	a.hits[id] = trimHits(a.hits[id], now, a.window)
}

func (a *AutoPicker) hitsInWindow(id string, now time.Time) int {
	a.hits[id] = trimHits(a.hits[id], now, a.window)
	return len(a.hits[id])
}

func trimHits(hits []time.Time, now time.Time, win time.Duration) []time.Time {
	if win <= 0 || len(hits) == 0 {
		return hits
	}
	cut := now.Add(-win)
	i := 0
	for i < len(hits) && hits[i].Before(cut) {
		i++
	}
	return hits[i:]
}

func (a *AutoPicker) chain(id string) *Chain {
	ch := a.chains[id]
	if ch == nil {
		ch = NewChain()
		a.chains[id] = ch
	}
	return ch
}

func (a *AutoPicker) remember(key, id string) {
	if a == nil || key == "" || id == "" {
		return
	}
	if a.memory == nil {
		a.memory = make(map[string]string)
	}
	a.memory[key] = id
}

func (a *AutoPicker) forget(key string) {
	if a == nil || a.memory == nil || key == "" {
		return
	}
	delete(a.memory, key)
}

func (a *AutoPicker) unpin() {
	a.pinned = ""
	a.lastGood = time.Time{}
	a.hits = make(map[string][]time.Time)
	for id := range a.chains {
		a.chains[id] = NewChain()
	}
}

func candidateHas(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// autoConfigKey — устойчивый ключ TX/КА для памяти выбора.
// При известном NORAD живая частота с доплером не входит: иначе каждый hop
// того же TX считался бы новым. Без NORAD — номинал с шагом 5 кГц.
func autoConfigKey(norad int, freqHz uint64, bw uint32, hints CandidateHints) string {
	_ = bw
	key := "n" + strconv.Itoa(norad) +
		"c" + hints.CardProfile +
		"m" + hints.Mode +
		"b" + strconv.FormatFloat(hints.Baud, 'f', -1, 64)
	if norad <= 0 {
		key += "f" + strconv.FormatUint(freqHz/5_000*5_000, 10)
	}
	return key
}
