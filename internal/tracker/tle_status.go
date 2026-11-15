package tracker

import (
	"slices"
	"strings"
	"time"
)

// TLELevel — уровень актуальности каталога TLE для шапки UI.
type TLELevel string

// Уровни актуальности TLE.
const (
	TLELevelOK    TLELevel = "ok"
	TLELevelWarn  TLELevel = "warn"
	TLELevelStale TLELevel = "stale"
)

// Причины, по которым уровень ниже ok.
const (
	TLEReasonEmpty     = "empty"      // каталог пуст
	TLEReasonMedianAge = "median_age" // у большинства КА старый epoch
	TLEReasonISSAge    = "iss_age"    // старый epoch МКС
	TLEReasonNetwork   = "network"    // давно нет успешной загрузки из сети
	TLEReasonPassDrift = "pass_drift" // наши пролёты расходятся со сторонним прогнозом
)

// Откуда взяты TLE группы.
const (
	TLESourceSpaceTrack = "spacetrack"
	TLESourceCelestrak  = "celestrak"
	TLESourceCache      = "cache"
)

const (
	issNoradID = 25544

	// leoWarnAge — медиана epoch старше двух суток: свежий Celestrak даёт 0,5–2 сут.
	leoWarnAge = 48 * time.Hour
	// У МКС орбита меняется быстрее (торможение, коррекции), пороги строже.
	issWarnAge  = 24 * time.Hour
	issStaleAge = 72 * time.Hour

	// networkLagIntervals — сколько интервалов обновления можно пропустить без предупреждения.
	networkLagIntervals = 2

	// Пролёты нужны с точностью до минуты; сдвиг в 5 мин уже съедает заметную часть пролёта.
	passDriftWarnS  = 60.0
	passDriftStaleS = 300.0
	// minPassPairs — меньше пар не дают надёжной медианы (МКС оценивается отдельно).
	minPassPairs = 3
)

// Фазы фоновой загрузки каталога с Space-Track (индикатор в шапке).
const (
	TLELoadPhaseGP        = "gp"
	TLELoadPhaseSatcat    = "satcat"
	TLELoadPhaseCelestrak = "celestrak"
)

// TLEStatus — сводка актуальности каталога TLE.
// Уровень считается по медиане epoch и по МКС: хвост заброшенных КА со старым epoch
// не должен держать шапку красной.
type TLEStatus struct {
	Level          TLELevel  `json:"level"`
	Reasons        []string  `json:"reasons"`
	Count          int       `json:"count"`
	StaleCount     int       `json:"stale_count"` // epoch старше max_tle_age_days
	MedianAgeHours float64   `json:"median_age_h"`
	ISSAgeHours    *float64  `json:"iss_age_h,omitempty"`
	Source         string    `json:"source,omitempty"`
	FetchedAt      time.Time `json:"fetched_at,omitzero"` // самая старая успешная загрузка из сети среди групп
	LastError      string    `json:"last_error,omitempty"`
	// Сверка со сторонним прогнозом пролётов; нет без сети и свежего кеша эталона.
	PassCheck *PassCheck `json:"pass_check,omitempty"`
	// Прогресс текущего прохода Space-Track: пачки GP или SATCAT.
	Loading   bool   `json:"loading,omitempty"`
	LoadPhase string `json:"load_phase,omitempty"`
	LoadDone  int    `json:"load_done,omitempty"`
	LoadTotal int    `json:"load_total,omitempty"`
}

// groupFetch — откуда и когда получены TLE группы.
type groupFetch struct {
	source    string
	fetchedAt time.Time // последняя успешная загрузка из сети; для кеша — метка из cache_meta
	lastErr   string    // ошибка последней попытки сети; пусто после успеха
}

// SetOnRefresh задаёт колбэк после каждой фоновой загрузки, удачной или нет.
// Вызывать до Start.
func (s *TLEStore) SetOnRefresh(fn func()) {
	s.onRefresh = fn
}

// SetOnProgress задаёт колбэк при обновлении прогресса пачек Space-Track (для SSE tle_status).
// Вызывать до Start.
func (s *TLEStore) SetOnProgress(fn func()) {
	s.onProgress = fn
}

// Status возвращает сводку актуальности каталога на момент now.
func (s *TLEStore) Status(now time.Time) TLEStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st := TLEStatus{
		Level: TLELevelOK, Reasons: []string{}, Count: len(s.catalog),
		Loading: s.loading, LoadPhase: s.loadPhase, LoadDone: s.loadDone, LoadTotal: s.loadTotal,
	}
	maxAge := time.Duration(s.config.MaxTLEAgeDays * 24 * float64(time.Hour))

	ages := make([]time.Duration, 0, len(s.catalog))
	for id, tle := range s.catalog {
		age := now.Sub(tle.Epoch)
		ages = append(ages, age)
		if age > maxAge {
			st.StaleCount++
		}
		if id == issNoradID {
			h := age.Hours()
			st.ISSAgeHours = &h
			switch {
			case age > issStaleAge:
				st.raise(TLELevelStale, TLEReasonISSAge)
			case age > issWarnAge:
				st.raise(TLELevelWarn, TLEReasonISSAge)
			}
		}
	}

	if len(ages) == 0 {
		st.raise(TLELevelStale, TLEReasonEmpty)
	} else {
		slices.Sort(ages)
		median := ages[len(ages)/2]
		st.MedianAgeHours = median.Hours()
		switch {
		case median > maxAge:
			st.raise(TLELevelStale, TLEReasonMedianAge)
		case median > leoWarnAge:
			st.raise(TLELevelWarn, TLEReasonMedianAge)
		}
	}

	s.fillFetchStatus(&st, now)
	s.fillPassCheck(&st)
	return st
}

// SetPassCheck запоминает итог последней сверки пролётов; nil — сверять было не с чем.
func (s *TLEStore) SetPassCheck(pc *PassCheck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pc == nil {
		s.passCheck = nil
		return
	}
	c := *pc
	s.passCheck = &c
}

// fillPassCheck добавляет сверку пролётов и понижает уровень по сдвигу. Вызывать под s.mu.
func (s *TLEStore) fillPassCheck(st *TLEStatus) {
	if s.passCheck == nil {
		return
	}
	c := *s.passCheck
	st.PassCheck = &c
	if c.Matched >= minPassPairs {
		switch {
		case c.MedianDriftS > passDriftStaleS:
			st.raise(TLELevelStale, TLEReasonPassDrift)
		case c.MedianDriftS > passDriftWarnS:
			st.raise(TLELevelWarn, TLEReasonPassDrift)
		}
	}
	if c.ISSDriftS != nil && *c.ISSDriftS > passDriftWarnS {
		st.raise(TLELevelWarn, TLEReasonPassDrift)
	}
}

// fillFetchStatus сводит источник и время последней загрузки. Вызывать под s.mu.
// С Space-Track сводка по каталогу; без него — Celestrak по NORAD или наследие файлов групп.
func (s *TLEStore) fillFetchStatus(st *TLEStatus, now time.Time) {
	networkLag := networkLagIntervals * s.config.UpdateInterval
	if f, ok := s.fetch[catalogCacheName]; ok && s.spacetrack != nil {
		st.addFetch(f, now, networkLag)
		return
	}
	if f, ok := s.fetch[celestrakFetchName]; ok {
		st.addFetch(f, now, networkLag)
		return
	}
	if f, ok := s.fetch[catalogCacheName]; ok {
		st.addFetch(f, now, networkLag)
		return
	}
	for _, g := range DefaultTLEGroups {
		if f, ok := s.fetch[strings.ToLower(g)]; ok {
			st.addFetch(f, now, networkLag)
		}
	}
}

// addFetch добавляет в сводку сведения об одной загрузке: источник «кеш» перекрывает сеть,
// время — самое старое, ошибка — первая встреченная.
func (st *TLEStatus) addFetch(f groupFetch, now time.Time, networkLag time.Duration) {
	if st.Source == "" || f.source == TLESourceCache {
		st.Source = f.source
	}
	if st.LastError == "" {
		st.LastError = f.lastErr
	}
	if !f.fetchedAt.IsZero() && (st.FetchedAt.IsZero() || f.fetchedAt.Before(st.FetchedAt)) {
		st.FetchedAt = f.fetchedAt
	}
	if f.fetchedAt.IsZero() || now.Sub(f.fetchedAt) > networkLag {
		st.raise(TLELevelWarn, TLEReasonNetwork)
	}
}

// setFetch обновляет сведения о загрузке группы. Вызывать под s.mu.
func (s *TLEStore) setFetch(group string, update func(f *groupFetch)) {
	key := strings.ToLower(group)
	f := s.fetch[key]
	update(&f)
	s.fetch[key] = f
}

// raise понижает уровень до level, если он хуже текущего, и добавляет причину.
func (st *TLEStatus) raise(level TLELevel, reason string) {
	if levelRank(level) > levelRank(st.Level) {
		st.Level = level
	}
	if !slices.Contains(st.Reasons, reason) {
		st.Reasons = append(st.Reasons, reason)
	}
}

func levelRank(l TLELevel) int {
	switch l {
	case TLELevelOK:
		return 0
	case TLELevelWarn:
		return 1
	case TLELevelStale:
		return 2
	default:
		return 0
	}
}
