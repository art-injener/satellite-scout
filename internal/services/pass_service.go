package services

import (
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// Константы PassService.
const (
	// Время жизни кеша пролётов по умолчанию.
	DefaultPassCacheTTL = 5 * time.Minute

	// Горизонт прогноза по умолчанию (часы).
	DefaultPredictionHours = 24

	// Минимальный угол места по умолчанию (градусы).
	DefaultMinElevation = 5.0

	// Группа спутников по умолчанию.
	DefaultSatelliteGroup = "amateur"

	// Ключ кеша для всех групп.
	allGroupsCacheKey = "all"
)

// cacheEntry — запись в кеше пролётов.
type cacheEntry struct {
	passes    []*tracker.Pass
	createdAt time.Time
	group     string
	hours     int
	minEl     float64
}

// isExpired проверяет, истёк ли TTL записи.
func (e *cacheEntry) isExpired(ttl time.Duration) bool {
	return time.Since(e.createdAt) > ttl
}

// PassService предоставляет расчёт пролётов спутников с кешированием.
type PassService struct {
	store    *tracker.TLEStore
	observer *tracker.Observer
	cacheTTL time.Duration

	mu    sync.RWMutex
	cache map[string]*cacheEntry // ключ: "group:hours:minEl"
}

// NewPassService создаёт новый сервис пролётов.
func NewPassService(store *tracker.TLEStore, observer *tracker.Observer) *PassService {
	return &PassService{
		store:    store,
		observer: observer,
		cacheTTL: DefaultPassCacheTTL,
		cache:    make(map[string]*cacheEntry),
	}
}

// WithCacheTTL устанавливает время жизни кеша.
func (s *PassService) WithCacheTTL(ttl time.Duration) *PassService {
	if ttl > 0 {
		s.cacheTTL = ttl
	}
	return s
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
// Результат кешируется на время cacheTTL.
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

	// Проверка кеша.
	s.mu.RLock()
	entry, ok := s.cache[cacheKey]
	s.mu.RUnlock()

	if ok && !entry.isExpired(s.cacheTTL) {
		slog.Debug("all groups pass cache hit",
			"hours", hours,
			"min_el", minEl,
			"passes", len(entry.passes),
		)
		return entry.passes, nil
	}

	// Расчёт пролётов для всех спутников.
	passes, err := s.computeAllPasses(hours, minEl)
	if err != nil {
		return nil, err
	}

	// Сохранение в кеш.
	s.mu.Lock()
	s.cache[cacheKey] = &cacheEntry{
		passes:    passes,
		createdAt: time.Now(),
		group:     allGroupsCacheKey,
		hours:     hours,
		minEl:     minEl,
	}
	s.mu.Unlock()

	slog.Debug("all groups pass cache miss, computed",
		"hours", hours,
		"min_el", minEl,
		"passes", len(passes),
	)

	return passes, nil
}

// computeAllPasses рассчитывает пролёты для всех спутников в хранилище.
func (s *PassService) computeAllPasses(hours int, minEl float64) ([]*tracker.Pass, error) {
	now := time.Now().UTC()
	end := now.Add(time.Duration(hours) * time.Hour)

	passes, err := tracker.PredictPassesForAll(s.store, s.observer, now, end, minEl)
	if err != nil {
		return nil, err
	}

	return passes, nil
}

// InvalidateCache очищает весь кеш пролётов.
func (s *PassService) InvalidateCache() {
	s.mu.Lock()
	s.cache = make(map[string]*cacheEntry)
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
func (s *PassService) CacheStats() (entries int, expired int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

	passes, err := tracker.PredictAllPasses(s.store, s.observer, group, now, end, minEl)
	if err != nil {
		return nil, err
	}

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
	for i := 0; i < decimals; i++ {
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
