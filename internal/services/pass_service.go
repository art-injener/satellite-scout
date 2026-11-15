package services

import (
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// Константы PassService.
const (
	// Время жизни кеша пролётов по умолчанию.
	DefaultPassCacheTTL = 5 * time.Minute

	// Горизонт прогноза по умолчанию (часы).
	DefaultPredictionHours = 1

	// Минимальный угол места по умолчанию (градусы).
	DefaultMinElevation = 5.0

	// Группа спутников по умолчанию.
	DefaultSatelliteGroup = "amateur"

	// Ключ кеша для всех групп.
	allGroupsCacheKey = "all"
)

// Excluder сообщает, исключён ли спутник из группы и списка пролётов.
type Excluder interface {
	Contains(norad int) bool
}

// cacheEntry — запись в кеше пролётов.
type cacheEntry struct {
	passes    []*tracker.Pass
	createdAt time.Time
	group     string
	hours     int
	minEl     float64
	stale     bool // орбиты обновились: запись отдаётся, пока идёт пересчёт
}

// passCall — расчёт пролётов в работе; все, кто ждёт этот ключ, получают один результат.
type passCall struct {
	done   chan struct{}
	passes []*tracker.Pass
	err    error
}

// isExpired проверяет, истёк ли TTL записи.
func (e *cacheEntry) isExpired(ttl time.Duration) bool {
	return time.Since(e.createdAt) > ttl
}

// PassService предоставляет расчёт пролётов спутников с кешированием.
//
// observer хранится в atomic.Pointer для безопасной замены координат на лету
// (hot-reload из единого конфига): счётчики тиков и обработчики читают точку
// наблюдения через Load(), а UI настроек подменяет её через SetObserver().
type PassService struct {
	store    *tracker.TLEStore
	observer atomic.Pointer[tracker.Observer]
	cacheTTL time.Duration
	excluder Excluder

	mu       sync.RWMutex
	cache    map[string]*cacheEntry // ключ: "group:hours:minEl"
	inflight map[string]*passCall   // фоновые расчёты всех групп, ключ как у cache
	gen      uint64                 // растёт при сбросе кеша: результат старого поколения не сохраняется

	// computeAll считает пролёты всех КА; в тестах подменяется управляемой функцией.
	computeAll func(hours int, minEl float64) ([]*tracker.Pass, error)
}

// NewPassService создаёт новый сервис пролётов.
func NewPassService(store *tracker.TLEStore, observer *tracker.Observer) *PassService {
	s := &PassService{
		store:    store,
		cacheTTL: DefaultPassCacheTTL,
		cache:    make(map[string]*cacheEntry),
		inflight: make(map[string]*passCall),
	}
	s.computeAll = s.computeAllPasses
	s.observer.Store(observer)
	return s
}

// Observer возвращает текущую точку наблюдения. Указатель безопасно читать —
// внутри значения не изменяются, при hot-reload подменяется весь указатель.
func (s *PassService) Observer() *tracker.Observer {
	return s.observer.Load()
}

// SetObserver атомарно заменяет точку наблюдения и сбрасывает кеш пролётов —
// все ранее посчитанные значения опираются на старые координаты и недопустимы.
// nil игнорируется, чтобы случайная сериализация не «потеряла» observer.
func (s *PassService) SetObserver(o *tracker.Observer) {
	if o == nil {
		return
	}
	s.observer.Store(o)
	s.InvalidateCache()
}

// WithCacheTTL устанавливает время жизни кеша.
func (s *PassService) WithCacheTTL(ttl time.Duration) *PassService {
	if ttl > 0 {
		s.cacheTTL = ttl
	}
	return s
}

// WithExcluder подключает набор исключённых NORAD ID.
func (s *PassService) WithExcluder(e Excluder) *PassService {
	s.excluder = e
	return s
}

// filterExcluded убирает из списка пролёты исключённых спутников.
func (s *PassService) filterExcluded(passes []*tracker.Pass) []*tracker.Pass {
	if s.excluder == nil || len(passes) == 0 {
		return passes
	}
	out := make([]*tracker.Pass, 0, len(passes))
	for _, p := range passes {
		if s.excluder.Contains(p.NoradID) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// GetPasses возвращает список пролётов для указанной группы спутников.
// Результат кешируется на время cacheTTL.
// Пролёты отсортированы по времени AOS (ближайшие первыми).
func (s *PassService) GetPasses(group string, hours int, minEl float64) ([]*tracker.Pass, error) {
	// Нормализация параметров.
	if group == "" {
		group = DefaultSatelliteGroup
	}
	if hours <= 0 {
		hours = DefaultPredictionHours
	}
	if minEl < 0 {
		minEl = DefaultMinElevation
	}

	cacheKey := s.makeCacheKey(group, hours, minEl)

	// Проверка кеша.
	s.mu.RLock()
	entry, ok := s.cache[cacheKey]
	s.mu.RUnlock()

	if ok && !entry.isExpired(s.cacheTTL) {
		slog.Debug("pass cache hit",
			"group", group,
			"hours", hours,
			"min_el", minEl,
			"passes", len(entry.passes),
		)
		return entry.passes, nil
	}

	// Расчёт пролётов.
	passes, err := s.computePasses(group, hours, minEl)
	if err != nil {
		return nil, err
	}

	// Сохранение в кеш.
	s.mu.Lock()
	s.cache[cacheKey] = &cacheEntry{
		passes:    passes,
		createdAt: time.Now(),
		group:     group,
		hours:     hours,
		minEl:     minEl,
	}
	s.mu.Unlock()

	slog.Debug("pass cache miss, computed",
		"group", group,
		"hours", hours,
		"min_el", minEl,
		"passes", len(passes),
	)

	return passes, nil
}

// GetAllGroupsPasses возвращает список пролётов для ВСЕХ загруженных спутников.
// Объединяет пролёты всех групп (stations, amateur, cubesat и др.).
// Результат кешируется на время cacheTTL. Устаревшая запись отдаётся сразу,
// а пересчёт идёт в фоне; ждать приходится только первый расчёт для ключа,
// и все одновременные вызовы ждут один общий расчёт.
// Пролёты отсортированы по времени AOS (ближайшие первыми).
func (s *PassService) GetAllGroupsPasses(hours int, minEl float64) ([]*tracker.Pass, error) {
	// Нормализация параметров.
	if hours <= 0 {
		hours = DefaultPredictionHours
	}
	if minEl < 0 {
		minEl = DefaultMinElevation
	}

	cacheKey := s.makeCacheKey(allGroupsCacheKey, hours, minEl)

	s.mu.RLock()
	entry, ok := s.cache[cacheKey]
	fresh := ok && !entry.stale && !entry.isExpired(s.cacheTTL)
	s.mu.RUnlock()
	if fresh {
		return entry.passes, nil
	}

	s.mu.Lock()
	entry, ok = s.cache[cacheKey]
	if ok {
		if entry.stale || entry.isExpired(s.cacheTTL) {
			s.startComputeAllLocked(cacheKey, hours, minEl)
		}
		s.mu.Unlock()
		return entry.passes, nil
	}
	call := s.startComputeAllLocked(cacheKey, hours, minEl)
	s.mu.Unlock()

	<-call.done
	return call.passes, call.err
}

// startComputeAllLocked запускает фоновый расчёт пролётов всех КА, если для ключа он ещё не идёт.
// Вызывать под s.mu.
func (s *PassService) startComputeAllLocked(cacheKey string, hours int, minEl float64) *passCall {
	if call, ok := s.inflight[cacheKey]; ok {
		return call
	}
	call := &passCall{done: make(chan struct{})}
	s.inflight[cacheKey] = call
	gen := s.gen

	go func() {
		started := time.Now()
		passes, err := s.computeAll(hours, minEl)
		// Каталог ещё пуст (TLE не успели прочитаться). Пустой ответ не запоминаем,
		// иначе после загрузки орбит пролёты не появятся до конца TTL.
		catalogReady := s.store == nil || s.store.Count() > 0

		s.mu.Lock()
		if s.inflight[cacheKey] == call {
			delete(s.inflight, cacheKey)
		}
		if err == nil && catalogReady && gen == s.gen {
			s.cache[cacheKey] = &cacheEntry{
				passes:    passes,
				createdAt: time.Now(),
				group:     allGroupsCacheKey,
				hours:     hours,
				minEl:     minEl,
			}
		}
		s.mu.Unlock()

		call.passes, call.err = passes, err
		close(call.done)

		slog.Debug("all groups passes computed",
			"hours", hours,
			"min_el", minEl,
			"passes", len(passes),
			"took", time.Since(started).Round(time.Millisecond),
		)
	}()
	return call
}

// MarkStale помечает кеш устаревшим после обновления орбит: до конца фонового
// пересчёта отдаются прежние пролёты. Смена станции требует InvalidateCache.
func (s *PassService) MarkStale() {
	s.mu.Lock()
	for _, entry := range s.cache {
		entry.stale = true
	}
	s.mu.Unlock()
}

// computeAllPasses рассчитывает пролёты для всех спутников в хранилище.
func (s *PassService) computeAllPasses(hours int, minEl float64) ([]*tracker.Pass, error) {
	now := time.Now().UTC()
	end := now.Add(time.Duration(hours) * time.Hour)

	passes, err := tracker.PredictPassesForAll(s.store, s.observer.Load(), now, end, minEl)
	if err != nil {
		return nil, err
	}

	return s.filterExcluded(passes), nil
}

// InvalidateCache очищает весь кеш пролётов. Расчёты, начатые до сброса,
// в кеш не попадут: следующий вызов запустит новый.
func (s *PassService) InvalidateCache() {
	s.mu.Lock()
	s.cache = make(map[string]*cacheEntry)
	s.inflight = make(map[string]*passCall)
	s.gen++
	s.mu.Unlock()

	slog.Info("pass cache invalidated")
}

// InvalidateCacheForGroup очищает кеш для конкретной группы.
func (s *PassService) InvalidateCacheForGroup(group string) {
	s.mu.Lock()
	for key, entry := range s.cache {
		if entry.group == group {
			delete(s.cache, key)
		}
	}
	s.mu.Unlock()

	slog.Info("pass cache invalidated for group", "group", group)
}

// CacheStats возвращает статистику кеша.
func (s *PassService) CacheStats() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var entries, expired int
	for _, entry := range s.cache {
		entries++
		if entry.isExpired(s.cacheTTL) {
			expired++
		}
	}
	return entries, expired
}

// computePasses рассчитывает пролёты для группы спутников.
func (s *PassService) computePasses(group string, hours int, minEl float64) ([]*tracker.Pass, error) {
	now := time.Now().UTC()
	end := now.Add(time.Duration(hours) * time.Hour)

	passes, err := tracker.PredictAllPasses(s.store, s.observer.Load(), group, now, end, minEl)
	if err != nil {
		return nil, err
	}

	passes = s.filterExcluded(passes)

	// Сортировка по AOS (ближайшие первыми).
	sort.Slice(passes, func(i, j int) bool {
		return passes[i].AOS < passes[j].AOS
	})

	return passes, nil
}

// makeCacheKey генерирует ключ кеша из параметров запроса.
func (s *PassService) makeCacheKey(group string, hours int, minEl float64) string {
	// Простой формат: "group:hours:minEl"
	// minEl округляется до 1 знака для уменьшения количества ключей.
	return group + ":" + itoa(hours) + ":" + ftoa(minEl, 1)
}

// itoa — простое преобразование int в string без зависимости от strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	// Разворот.
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return sign + string(buf)
}

// ftoa — простое преобразование float64 в string с фиксированной точностью.
func ftoa(f float64, decimals int) string {
	sign := ""
	if f < 0 {
		sign = "-"
		f = -f
	}
	// Умножаем для получения нужного количества знаков после запятой.
	mul := 1
	for range decimals {
		mul *= 10
	}
	rounded := int(f*float64(mul) + 0.5)
	intPart := rounded / mul
	fracPart := rounded % mul

	if decimals == 0 {
		return sign + itoa(intPart)
	}

	// Форматируем дробную часть с ведущими нулями.
	fracStr := itoa(fracPart)
	for len(fracStr) < decimals {
		fracStr = "0" + fracStr
	}
	return sign + itoa(intPart) + "." + fracStr
}
