package bandscan

import (
	"slices"

	"github.com/art-injener/satellite-scout/internal/signalclass"
	"github.com/art-injener/satellite-scout/internal/signals"
)

const (
	// sliceMs — отрезок для разбора непрерывного сигнала. 2 с: VIS SSTV ~0,9 с и
	// несколько групп RDS (PS за четыре группы 0A).
	sliceMs = 2000
	// maxSliceMs — самый длинный отрезок вспышки: столько держит кольцо IQ.
	maxSliceMs = historySeconds * 1000
	// burstMarginMs — запас до и после вспышки: решение обнаружителя запаздывает на интервал.
	burstMarginMs = 200
	// reclassifyMs — известный участок разбирается заново раз в 10 с.
	reclassifyMs = 10_000
	// rdsRetryMs — вещание без имени PS: сегменты RDS копятся, ждём не 10 с.
	rdsRetryMs = 3_000
	// structureVotes — по скольким последним разборам сглаживается ответ: одна ошибка
	// не перекрашивает маркер.
	structureVotes = 3
	// sstvStickyMs — сколько держать уверенный SSTV (VIS), пока новые разборы без VIS.
	sstvStickyMs = 60_000
	// stationStickyMs — сколько держать имя RDS, пока свежий разбор без PS.
	stationStickyMs = 120_000
)

// classifyJob — участок, который пора разобрать, и отрезок потока для него.
type classifyJob struct {
	id          int
	centerHz    float64
	bandwidthHz float64
	fromMs      uint64
	toMs        uint64
}

// nextJob выбирает участок для разбора: сначала ни разу не разобранные, затем
// вещание без имени RDS, затем давнее всех. historyFromMs — с какого времени
// в кольце есть IQ. Выбранный считается поставленным в очередь.
func (t *Tracker) nextJob(historyFromMs uint64) (classifyJob, bool) {
	var best *segment
	var bestJob classifyJob
	for _, s := range t.segs {
		if s.hits < confirmHits || s.loHz < t.usableLo || s.hiHz > t.usableHi {
			continue
		}
		if s.classifiedMs != 0 && t.nowMs < s.classifiedMs+s.reclassifyAfter() {
			continue
		}
		job, ok := s.job(t.nowMs, historyFromMs)
		if !ok {
			continue
		}
		if best == nil || betterClassify(s, best) {
			best, bestJob = s, job
		}
	}
	if best == nil {
		return classifyJob{}, false
	}
	// Помечен как взятый в очередь; если вырезка IQ не удалась — сбросить classifiedMs.
	best.classifiedMs = t.nowMs
	return bestJob, true
}

// reclassifyAfter — обычный участок 10 с; вещание без полного имени PS — чаще,
// иначе очередь залипает на одном удачном DFM, а соседи ждут по 10 с.
func (s *segment) reclassifyAfter() uint64 {
	if s.stationName != "" {
		return reclassifyMs
	}
	for _, ok := range s.rdsHave {
		if ok {
			return rdsRetryMs
		}
	}
	if s.structure != nil && s.structure.Service == signalclass.ServiceBroadcast {
		return rdsRetryMs
	}
	return reclassifyMs
}

// betterClassify — a важнее b: сначала ещё не разобранные, потом более старый разбор.
// Эфирное радио без PS не ставим выше новых: иначе один «глухой» RDS блокирует остальных.
func betterClassify(a, b *segment) bool {
	if (a.classifiedMs == 0) != (b.classifiedMs == 0) {
		return a.classifiedMs == 0
	}
	return a.classifiedMs < b.classifiedMs
}

// job — отрезок для разбора: у звучащего дольше sliceMs — последние sliceMs, у кончившейся
// вспышки — она целиком с запасом, но не длиннее кольца. Короткую вспышку ждём до конца.
func (s *segment) job(nowMs, historyFromMs uint64) (classifyJob, bool) {
	if len(s.starts) == 0 {
		return classifyJob{}, false
	}
	start := s.starts[len(s.starts)-1]
	var from, to uint64
	switch {
	case s.on && nowMs-start >= sliceMs:
		from, to = nowMs-sliceMs, nowMs
	case s.on:
		return classifyJob{}, false
	default:
		to = s.lastSeenMs + burstMarginMs
		if to > nowMs {
			return classifyJob{}, false
		}
		from = max(start-burstMarginMs, to-maxSliceMs)
	}
	// Запас в кольце есть, но на старте/после перестройки истории может быть меньше запроса.
	if from < historyFromMs {
		from = historyFromMs
	}
	if to <= from {
		return classifyJob{}, false
	}
	return classifyJob{
		id: s.id, centerHz: s.center(), bandwidthHz: s.hiHz - s.loHz, fromMs: from, toMs: to,
	}, true
}

// SetStructure добавляет ответ разбора участку id; участок мог уже слиться с другим.
func (t *Tracker) SetStructure(id int, r signalclass.Result) {
	t.setStructure(id, r, nil, 0)
}

// setStructure — то же, плюс вырезка участка: по ней запускается проба, если сигнатура её требует.
func (t *Tracker) setStructure(id int, r signalclass.Result, iq []complex64, rate float64) {
	i := slices.IndexFunc(t.segs, func(s *segment) bool { return s.id == id })
	if i < 0 {
		return
	}
	s := t.segs[i]
	r = signalclass.Resolve(r, signals.Input{
		CenterHz: s.center(),
		Emission: s.emission(),
		OnS:      float64(s.onMs) / 1000,
		Bursts:   s.bursts,
	}, iq, rate)
	s.votes = append(s.votes, r)
	if len(s.votes) > structureVotes {
		s.votes = s.votes[1:]
	}
	s.structure = vote(s.votes, s.structure)
	s.stickSSTV(t.nowMs, r)
	s.stickStation(t.nowMs, r)
}

// stickSSTV — VIS держит службу SSTV ещё sstvStickyMs, даже если свежий разбор без VIS.
func (s *segment) stickSSTV(nowMs uint64, r signalclass.Result) {
	if r.Service == "sstv" {
		s.sstvUntilMs = nowMs + sstvStickyMs
		if r.ServiceLabel != "" {
			s.sstvLabel = r.ServiceLabel
		}
	}
	if s.structure == nil || s.sstvUntilMs == 0 || nowMs >= s.sstvUntilMs {
		return
	}
	if s.structure.Service == "sstv" {
		return
	}
	s.structure.Service = "sstv"
	if s.sstvLabel != "" {
		s.structure.ServiceLabel = s.sstvLabel
	}
}

// stickStation — копит сегменты RDS PS между разборами и держит имя stationStickyMs.
func (s *segment) stickStation(nowMs uint64, r signalclass.Result) {
	if name := signalclass.MergeRDSParts(&s.rdsParts, &s.rdsHave, r.RDSParts, r.RDSHave); name != "" {
		s.stationName = name
		s.stationUntilMs = nowMs + stationStickyMs
	}
	if r.Station != "" {
		s.stationUntilMs = nowMs + stationStickyMs
		s.stationName = r.Station
	}
	if s.structure == nil || s.stationName == "" {
		return
	}
	if s.stationUntilMs != 0 && nowMs >= s.stationUntilMs {
		return
	}
	if s.structure.Station == s.stationName {
		return
	}
	s.structure.Station = s.stationName
	if s.structure.Service == signalclass.ServiceBroadcast || s.structure.Service == "" {
		s.structure.Service = signalclass.ServiceBroadcast
		s.structure.ServiceLabel = signalclass.LabelBroadcast + ": " + s.stationName
		if s.structure.ServiceBasis == "" {
			s.structure.ServiceBasis = signalclass.BasisSignature
		}
	}
	if s.structure.Demod == "" {
		s.structure.Demod = "wfm"
	}
}

// vote — модуляция большинства последних ответов; при равенстве остаётся показанная,
// иначе свежая. Вид передачи сглаживается так же: один промах из трёх подпись не снимает.
func vote(votes []signalclass.Result, shown *signalclass.Result) *signalclass.Result {
	mod := majority(votes, shown, func(v signalclass.Result) string { return v.Modulation })
	var base *signalclass.Result
	for _, v := range slices.Backward(votes) {
		if v.Modulation == mod {
			r := v
			base = &r
			break
		}
	}
	if base == nil {
		return shown
	}
	svc := majorityService(votes, mod, shown)
	base.Service = svc.Service
	base.ServiceLabel = svc.ServiceLabel
	base.ServiceBasis = svc.ServiceBasis
	return base
}

func majority(votes []signalclass.Result, shown *signalclass.Result, key func(signalclass.Result) string) string {
	count := make(map[string]int, len(votes))
	top := 0
	for _, v := range votes {
		k := key(v)
		count[k]++
		top = max(top, count[k])
	}
	if shown != nil && count[key(*shown)] == top {
		return key(*shown)
	}
	for _, v := range slices.Backward(votes) {
		if count[key(v)] == top {
			return key(v)
		}
	}
	return ""
}

func majorityService(votes []signalclass.Result, mod string, shown *signalclass.Result) signalclass.Result {
	count := map[string]int{}
	top := 0
	var subset []signalclass.Result
	for _, v := range votes {
		if v.Modulation != mod {
			continue
		}
		subset = append(subset, v)
		count[v.Service]++
		top = max(top, count[v.Service])
	}
	want := ""
	if shown != nil && shown.Modulation == mod && count[shown.Service] == top {
		want = shown.Service
	} else {
		for _, v := range slices.Backward(subset) {
			if count[v.Service] == top {
				want = v.Service
				break
			}
		}
	}
	for _, v := range slices.Backward(subset) {
		if v.Service == want {
			return v
		}
	}
	return signalclass.Result{}
}

// applyOrigin замыкает подпись происхождением. КА снимает земной вид.
// ЧМ с речью и узкая цифра становятся радиопередатчиком. Космос или земля
// на подпись не пишутся: это положение участка.
func applyOrigin(r signalclass.Result, origin string) signalclass.Result {
	switch origin {
	case OriginSatellite, OriginSatelliteProbable:
		if r.Service == "sstv" || r.Service == "sstv_probable" {
			return r
		}
		q := origin == OriginSatelliteProbable
		switch r.Modulation {
		case signalclass.ModFMVoice:
			r.Service = signalclass.ServiceSatVoice
			r.ServiceLabel = satLabel(signalclass.LabelTransmitter, q)
			r.ServiceBasis = signalclass.BasisSignature
		case signalclass.Mod2FSK:
			r.Service = signalclass.ServiceSatTx
			r.ServiceLabel = satLabel(signalclass.LabelTransmitter, q)
			r.ServiceBasis = signalclass.BasisSignature
		default:
			if terrestrialService(r.Service) {
				r.Service, r.ServiceLabel, r.ServiceBasis = "", "", ""
			}
		}
	case OriginTerrestrial, OriginUnknown:
		if r.Service == signalclass.ServiceSatVoice || r.Service == signalclass.ServiceSatTx {
			r.Service, r.ServiceLabel, r.ServiceBasis = "", "", ""
		}
	}
	return r
}

func terrestrialService(id string) bool {
	switch id {
	case signalclass.ServiceBroadcast, signals.IDAmateur, signals.IDAmateurData, signals.IDISM, signals.IDLoRa, signals.IDGSM:
		return true
	default:
		return false
	}
}

func satLabel(base string, probable bool) string {
	if probable {
		return base + "?"
	}
	return base
}
