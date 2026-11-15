package registry

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// Registry реестр КА: снимки источников в памяти и, если открыт через Open, в SQLite.
// Ошибка записи в базу не откатывает память: реестр продолжает работать, вызывающий пишет WARN.
type Registry struct {
	// writeMu держит порядок записей: память и база меняются в одной последовательности.
	writeMu sync.Mutex

	mu     sync.RWMutex
	facts  map[int]map[string]SatFact
	orbits map[int]Orbit
	txs    map[int]map[string][]Transmitter
	loads  map[string]SourceLoad
	heard  map[int]struct{} // NORAD с хотя бы одним визитом energy=true

	store *store // nil — только память
}

// New создаёт реестр только в памяти.
func New() *Registry {
	return &Registry{
		facts:  make(map[int]map[string]SatFact),
		orbits: make(map[int]Orbit),
		txs:    make(map[int]map[string][]Transmitter),
		loads:  make(map[string]SourceLoad),
		heard:  make(map[int]struct{}),
	}
}

// Open открывает базу реестра (создаёт файл и таблицы при первом запуске) и поднимает сохранённое.
func Open(ctx context.Context, path string) (*Registry, error) {
	st, err := openStore(ctx, path)
	if err != nil {
		return nil, err
	}
	r := New()
	if err = st.load(ctx, r); err != nil {
		_ = st.close()
		return nil, err
	}
	r.store = st
	return r, nil
}

// Close закрывает базу. Реестр в памяти остаётся доступным для чтения.
func (r *Registry) Close() error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.store == nil {
		return nil
	}
	err := r.store.close()
	r.store = nil
	return err
}

// ReplaceSatFacts заменяет все сведения о КА от источника новым снимком.
func (r *Registry) ReplaceSatFacts(ctx context.Context, source string, facts []SatFact, at time.Time) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	norm := make([]SatFact, 0, len(facts))
	r.mu.Lock()
	for id, bySrc := range r.facts {
		delete(bySrc, source)
		if len(bySrc) == 0 {
			delete(r.facts, id)
		}
	}
	for _, f := range facts {
		f.Source = source
		if f.FetchedAt.IsZero() {
			f.FetchedAt = at
		}
		r.putFactLocked(f)
		norm = append(norm, f)
	}
	r.mu.Unlock()

	if r.store == nil {
		return nil
	}
	return r.store.replaceFacts(ctx, source, norm)
}

// UpsertSatFacts дописывает сведения источника, не удаляя остальные его записи.
// Нужно, когда проход оборвался и пришла только часть ответа.
func (r *Registry) UpsertSatFacts(ctx context.Context, source string, facts []SatFact, at time.Time) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	norm := make([]SatFact, 0, len(facts))
	r.mu.Lock()
	for _, f := range facts {
		f.Source = source
		if f.FetchedAt.IsZero() {
			f.FetchedAt = at
		}
		r.putFactLocked(f)
		norm = append(norm, f)
	}
	r.mu.Unlock()

	if r.store == nil || len(norm) == 0 {
		return nil
	}
	return r.store.upsertFacts(ctx, source, norm)
}

// ReplaceTransmitters заменяет все передатчики источника новым снимком.
// Снимок без изменений базу не трогает.
func (r *Registry) ReplaceTransmitters(ctx context.Context, source string, txs []Transmitter, at time.Time) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	next := groupTx(source, txs, at)
	r.mu.Lock()
	if r.sourceTxEqualLocked(source, next) {
		r.mu.Unlock()
		return nil
	}
	for id, bySrc := range r.txs {
		delete(bySrc, source)
		if len(bySrc) == 0 {
			delete(r.txs, id)
		}
	}
	for id, list := range next {
		r.setTxLocked(id, source, list)
	}
	r.mu.Unlock()

	if r.store == nil {
		return nil
	}
	return r.store.replaceTx(ctx, source, nil, next)
}

// ReplaceTransmittersByNorad заменяет передатчики источника только для перечисленных NORAD.
// Пустой список у NORAD удаляет его передатчики от этого источника.
func (r *Registry) ReplaceTransmittersByNorad(
	ctx context.Context,
	source string,
	byNorad map[int][]Transmitter,
	at time.Time,
) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	changed := make(map[int][]Transmitter)
	r.mu.Lock()
	for id, list := range byNorad {
		var norm []Transmitter
		for _, tx := range list {
			tx.NoradID = id
			norm = append(norm, normTx(tx, source, at))
		}
		if txListEqual(r.txs[id][source], norm) {
			continue
		}
		r.setTxLocked(id, source, norm)
		changed[id] = norm
	}
	r.mu.Unlock()

	if r.store == nil || len(changed) == 0 {
		return nil
	}
	return r.store.replaceTx(ctx, source, slices.Collect(maps.Keys(changed)), changed)
}

// PutOrbits записывает элементы орбит. Более свежая эпоха побеждает;
// при той же эпохе Space-Track старше Celestrak, тот старше кеша на диске.
func (r *Registry) PutOrbits(ctx context.Context, source string, tles []*tracker.TLE, at time.Time) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	var changed []Orbit
	r.mu.Lock()
	for _, tle := range tles {
		if tle == nil || tle.NoradID <= 0 {
			continue
		}
		if old, ok := r.orbits[tle.NoradID]; ok && !orbitReplaces(old, tle.Epoch, source) {
			continue
		}
		o := Orbit{
			NoradID: tle.NoradID, Name: tle.Name, Line1: tle.Line1, Line2: tle.Line2,
			Epoch: tle.Epoch, Source: source, FetchedAt: at,
		}
		r.orbits[o.NoradID] = o
		changed = append(changed, o)
	}
	r.mu.Unlock()

	if r.store == nil || len(changed) == 0 {
		return nil
	}
	return r.store.putOrbits(ctx, changed)
}

// MarkLoad записывает удачную (err == nil) или неудачную загрузку источника.
func (r *Registry) MarkLoad(ctx context.Context, source string, at time.Time, err error) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	r.mu.Lock()
	l := r.loads[source]
	l.Source = source
	if err == nil {
		l.LastOK, l.Err = at, ""
	} else {
		l.LastFail, l.Err = at, err.Error()
	}
	r.loads[source] = l
	r.mu.Unlock()

	if r.store == nil {
		return nil
	}
	return r.store.putLoad(ctx, l)
}

// Satellite возвращает карточку КА после слияния источников.
func (r *Registry) Satellite(norad int) (Satellite, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	facts, txs := r.facts[norad], r.txs[norad]
	orbit, hasOrbit := r.orbits[norad]
	if len(facts) == 0 && len(txs) == 0 && !hasOrbit {
		return Satellite{}, false
	}
	var op *Orbit
	if hasOrbit {
		op = &orbit
	}
	return mergeSatellite(norad, facts, txs, op), true
}

// Decayed сообщает, что КА сошёл с орбиты: такой КА не попадает в пролёты и группу.
func (r *Registry) Decayed(norad int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status, _, _ := mergeStatus(r.facts[norad], nil)
	return status == StatusReEntered
}

// Transmitters рабочие передатчики КА после слияния источников (реализация freqplan.Source).
func (r *Registry) Transmitters(norad int) []freqplan.Tx {
	r.mu.RLock()
	merged := mergeTransmitters(r.txs[norad])
	r.mu.RUnlock()
	if len(merged) == 0 {
		return nil
	}
	out := make([]freqplan.Tx, len(merged))
	for i, tx := range merged {
		out[i] = freqplan.Tx{
			UUID: tx.UUID, Name: tx.Name, FreqHz: tx.DownlinkHz,
			Mode: tx.Mode, Baud: tx.Baud, Source: tx.Source,
		}
	}
	return out
}

// Orbit возвращает элементы орбиты КА.
func (r *Registry) Orbit(norad int) (Orbit, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.orbits[norad]
	return o, ok
}

// Loads возвращает журнал загрузок по источникам.
func (r *Registry) Loads() map[string]SourceLoad {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return maps.Clone(r.loads)
}

// orbitReplaces: более свежая эпоха побеждает, при равной — более старший источник.
func orbitReplaces(old Orbit, epoch time.Time, source string) bool {
	if epoch.After(old.Epoch) {
		return true
	}
	if epoch.Before(old.Epoch) {
		return false
	}
	return orbitRank(source) > orbitRank(old.Source)
}

func orbitRank(source string) int {
	switch source {
	case SourceSpaceTrack:
		return 2
	case tracker.TLESourceCelestrak:
		return 1
	default:
		return 0
	}
}

func (r *Registry) putFactLocked(f SatFact) {
	bySrc := r.facts[f.NoradID]
	if bySrc == nil {
		bySrc = make(map[string]SatFact)
		r.facts[f.NoradID] = bySrc
	}
	bySrc[f.Source] = f
}

func (r *Registry) setTxLocked(norad int, source string, list []Transmitter) {
	bySrc := r.txs[norad]
	if len(list) == 0 {
		if bySrc != nil {
			delete(bySrc, source)
			if len(bySrc) == 0 {
				delete(r.txs, norad)
			}
		}
		return
	}
	if bySrc == nil {
		bySrc = make(map[string][]Transmitter)
		r.txs[norad] = bySrc
	}
	bySrc[source] = list
}

func (r *Registry) sourceTxEqualLocked(source string, next map[int][]Transmitter) bool {
	n := 0
	for id, bySrc := range r.txs {
		list, ok := bySrc[source]
		if !ok {
			continue
		}
		n++
		if !txListEqual(list, next[id]) {
			return false
		}
	}
	return n == len(next)
}

// groupTx раскладывает передатчики по NORAD с источником и временем получения.
func groupTx(source string, txs []Transmitter, at time.Time) map[int][]Transmitter {
	out := make(map[int][]Transmitter)
	for _, tx := range txs {
		if tx.NoradID <= 0 {
			continue
		}
		out[tx.NoradID] = append(out[tx.NoradID], normTx(tx, source, at))
	}
	return out
}

func normTx(tx Transmitter, source string, at time.Time) Transmitter {
	tx.Source = source
	if tx.FetchedAt.IsZero() {
		tx.FetchedAt = at
	}
	return tx
}

func txListEqual(a, b []Transmitter) bool {
	return slices.EqualFunc(a, b, Transmitter.sameRecord)
}
