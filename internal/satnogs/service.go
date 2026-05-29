package satnogs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Константы сервиса.
const (
	// DefaultCacheTTL — время жизни записи в кеше передатчиков.
	// Сутки — компромисс между свежестью данных SatNOGS (обновляются редко)
	// и нагрузкой на их API (десятки уникальных NORAD за день).
	DefaultCacheTTL = 24 * time.Hour

	// DefaultRetryAfter — базовая задержка перед повторной попыткой после ошибки fetch.
	// Для первой ошибки — 5 минут; при повторных подряд — прогрессивный backoff:
	// 5 мин → 30 мин → 2 ч (cap). См. retryDelay().
	DefaultRetryAfter = 5 * time.Minute

	// maxRetryDelay — верхняя граница прогрессивного backoff после серии ошибок.
	maxRetryDelay = 2 * time.Hour

	// fetchQueueSize — буфер канала запросов на фоновую загрузку.
	// 256 — запас на случай добавления большой группы (10–30 КА) одним вызовом.
	fetchQueueSize = 256
)

// fetcher — узкий интерфейс HTTP-клиента, нужный сервису.
// Позволяет в тестах подменить настоящий *Client моком.
type fetcher interface {
	FetchTransmitters(ctx context.Context, noradID int) ([]Transmitter, error)
}

// cacheEntry — запись в in-memory-кеше передатчиков одного спутника.
type cacheEntry struct {
	transmitters []Transmitter       // Полный список ответа SatNOGS.
	primary      *TransmitterSummary // Выбранный «главный» передатчик (или nil).
	fetchedAt    time.Time           // Время последней удачной/неудачной попытки.
	fetching     bool                // Запрос на загрузку поставлен в очередь / в работе.
	lastErr      error               // Последняя ошибка fetch (nil при успехе).
	failCount    int                 // Счётчик последовательных неудач (сбрасывается при успехе).
}

// isFresh — запись свежая по основному TTL (для успешных загрузок).
func (e *cacheEntry) isFresh(ttl time.Duration) bool {
	return e != nil && e.lastErr == nil && time.Since(e.fetchedAt) < ttl
}

// canRetry — после ошибки прошло достаточно времени для новой попытки.
// Учитывает прогрессивный backoff: чем больше failCount, тем дольше ждём.
func (e *cacheEntry) canRetry(retryAfter time.Duration) bool {
	if e == nil {
		return true
	}
	if e.lastErr == nil {
		return false
	}
	return time.Since(e.fetchedAt) >= retryDelay(retryAfter, e.failCount)
}

// retryDelay рассчитывает задержку с экспоненциальным ростом:
// base * 2^(failCount-1), capped по maxRetryDelay.
// failCount=1 → base (5 мин), failCount=2 → 10 мин, …, cap → 2 часа.
func retryDelay(base time.Duration, failCount int) time.Duration {
	if failCount <= 1 {
		return base
	}
	shift := failCount - 1
	if shift > 10 {
		shift = 10
	}
	d := base * time.Duration(1<<uint(shift))
	if d > maxRetryDelay {
		d = maxRetryDelay
	}
	return d
}

// Service — координатор асинхронной загрузки и кеширования передатчиков SatNOGS.
//
// Принцип работы:
//   - GetPrimaryTransmitter / GetAllTransmitters — неблокирующие, читают только из кеша.
//   - При промахе кеша или истечении TTL вызов автоматически ставит NORAD в очередь fetch.
//   - Run() — фоновая горутина-воркер, читает очередь и вызывает HTTP-клиент.
//   - Graceful degradation: при ошибке API лог-warning, кеш помечается lastErr,
//     повторная попытка не раньше DefaultRetryAfter.
type Service struct {
	client     fetcher
	cacheTTL   time.Duration
	retryAfter time.Duration

	mu    sync.RWMutex
	cache map[int]*cacheEntry

	fetchQueue chan int

	logger *slog.Logger
}

// NewService создаёт сервис SatNOGS.
// client — реализация HTTP-клиента (обычно *Client из этого же пакета).
func NewService(client fetcher) *Service {
	return &Service{
		client:     client,
		cacheTTL:   DefaultCacheTTL,
		retryAfter: DefaultRetryAfter,
		cache:      make(map[int]*cacheEntry),
		fetchQueue: make(chan int, fetchQueueSize),
		logger:     slog.Default(),
	}
}

// WithCacheTTL переопределяет TTL кеша.
func (s *Service) WithCacheTTL(ttl time.Duration) *Service {
	if ttl > 0 {
		s.cacheTTL = ttl
	}
	return s
}

// WithRetryAfter переопределяет задержку повторной попытки после ошибки.
func (s *Service) WithRetryAfter(d time.Duration) *Service {
	if d > 0 {
		s.retryAfter = d
	}
	return s
}

// WithLogger подключает кастомный slog-логгер.
func (s *Service) WithLogger(logger *slog.Logger) *Service {
	if logger != nil {
		s.logger = logger
	}
	return s
}

// Run запускает фоновую горутину-воркер.
// Горутина читает fetchQueue и для каждого NORAD вызывает client.FetchTransmitters,
// после чего обновляет кеш. Завершается при отмене ctx.
func (s *Service) Run(ctx context.Context) {
	s.logger.InfoContext(ctx, "satnogs service started",
		slog.Duration("cache_ttl", s.cacheTTL),
		slog.Duration("retry_after", s.retryAfter),
	)

	for {
		select {
		case <-ctx.Done():
			s.logger.InfoContext(ctx, "satnogs service stopped")
			return
		case noradID := <-s.fetchQueue:
			s.fetchOne(ctx, noradID)
		}
	}
}

// GetPrimaryTransmitter возвращает primary-передатчик из кеша.
// Никогда не блокирует: на промахе ставит NORAD в очередь и возвращает nil.
// Когда фоновая загрузка завершится, следующий вызов вернёт данные.
func (s *Service) GetPrimaryTransmitter(noradID int) *TransmitterSummary {
	if noradID <= 0 {
		return nil
	}

	s.mu.RLock()
	entry := s.cache[noradID]
	s.mu.RUnlock()

	if entry != nil && entry.isFresh(s.cacheTTL) {
		return entry.primary
	}

	s.maybeEnqueue(noradID, entry)

	if entry != nil {
		// Возвращаем последний известный primary, даже если запись устарела.
		// Это даёт «sticky» поведение: пока новый fetch в работе, UI показывает старое значение,
		// а не пустую заглушку.
		return entry.primary
	}
	return nil
}

// GetAllTransmitters возвращает копию полного списка передатчиков из кеша.
// Поведение аналогично GetPrimaryTransmitter — неблокирующее.
func (s *Service) GetAllTransmitters(noradID int) []Transmitter {
	if noradID <= 0 {
		return nil
	}

	s.mu.RLock()
	entry := s.cache[noradID]
	s.mu.RUnlock()

	if entry == nil {
		s.maybeEnqueue(noradID, nil)
		return nil
	}
	if !entry.isFresh(s.cacheTTL) {
		s.maybeEnqueue(noradID, entry)
	}
	if len(entry.transmitters) == 0 {
		return nil
	}
	out := make([]Transmitter, len(entry.transmitters))
	copy(out, entry.transmitters)
	return out
}

// RequestFetch ставит несколько NORAD в очередь на фоновую загрузку.
// Дубликаты и уже-актуальные записи не запрашиваются повторно.
func (s *Service) RequestFetch(noradIDs []int) {
	for _, id := range noradIDs {
		if id <= 0 {
			continue
		}
		s.mu.RLock()
		entry := s.cache[id]
		s.mu.RUnlock()
		s.maybeEnqueue(id, entry)
	}
}

// maybeEnqueue — общая точка решения «нужно ли просить fetch для этого NORAD».
//
// Условия для постановки в очередь:
//   - запись отсутствует в кеше; или
//   - запись устарела (старше cacheTTL) и не помечена ошибкой; или
//   - запись с ошибкой и retryAfter уже прошёл.
//
// При этом проверяем флаг fetching, чтобы не дублировать запросы.
func (s *Service) maybeEnqueue(noradID int, entry *cacheEntry) {
	if noradID <= 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Перечитываем под write-lock — состояние могло измениться.
	cur, ok := s.cache[noradID]
	if !ok {
		cur = nil
	}

	// Уже стоит в очереди или активно загружается.
	if cur != nil && cur.fetching {
		return
	}

	// Свежие данные без ошибки — fetch не нужен.
	if cur != nil && cur.lastErr == nil && time.Since(cur.fetchedAt) < s.cacheTTL {
		return
	}

	// После ошибки ждём DefaultRetryAfter.
	if cur != nil && cur.lastErr != nil && !cur.canRetry(s.retryAfter) {
		return
	}

	if cur == nil {
		cur = &cacheEntry{}
		s.cache[noradID] = cur
	}
	cur.fetching = true

	select {
	case s.fetchQueue <- noradID:
	default:
		// Очередь переполнена — снимаем флаг, попробуем снова при следующем вызове.
		cur.fetching = false
		s.logger.Warn("satnogs: fetch queue is full, dropping request",
			slog.Int("norad_id", noradID),
		)
	}
}

// fetchOne делает один HTTP-запрос и обновляет кеш.
// Не возвращает ошибку — все ошибки логируются и сохраняются в cacheEntry.lastErr.
func (s *Service) fetchOne(ctx context.Context, noradID int) {
	transmitters, err := s.client.FetchTransmitters(ctx, noradID)
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.cache[noradID]
	if !ok {
		entry = &cacheEntry{}
		s.cache[noradID] = entry
	}
	entry.fetching = false
	entry.fetchedAt = now

	if err != nil {
		entry.lastErr = err
		entry.failCount++

		s.logFetchError(noradID, err, entry.failCount)
		return
	}

	entry.lastErr = nil
	entry.failCount = 0
	entry.transmitters = transmitters
	entry.primary = SelectPrimary(transmitters)
	s.logger.Debug("satnogs: fetched transmitters",
		slog.Int("norad_id", noradID),
		slog.Int("count", len(transmitters)),
		slog.Bool("has_primary", entry.primary != nil),
	)
}

// logFetchError выбирает уровень лога в зависимости от типа ошибки.
// Таймауты и отмены контекста → Debug (шум при медленном API).
// 404, 400 → Debug (ожидаемые состояния).
// Прочие → Warn (инфраструктурные проблемы).
func (s *Service) logFetchError(noradID int, err error, failCount int) {
	attrs := []any{
		slog.Int("norad_id", noradID),
		slog.Any("error", err),
		slog.Int("fail_count", failCount),
	}

	switch {
	case errors.Is(err, ErrSatNOGSNotFound):
		s.logger.Debug("satnogs: no transmitters for satellite",
			slog.Int("norad_id", noradID),
		)
	case errors.Is(err, ErrSatNOGSBadRequest),
		errors.Is(err, ErrSatNOGSClientError):
		s.logger.Debug("satnogs: client error (no retry)", attrs...)
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		s.logger.Debug("satnogs: fetch timeout/canceled", attrs...)
	default:
		s.logger.Warn("satnogs: fetch failed", attrs...)
	}
}

// CacheSize возвращает количество записей в кеше (для метрик / отладки).
func (s *Service) CacheSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cache)
}
