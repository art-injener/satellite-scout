package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	cacheMetaFilename = "cache_meta.json"
	tleCacheExtension = ".tle"
)

// ErrLoadGroupFailed ошибка при загрузке группы TLE.
var ErrLoadGroupFailed = errors.New("failed to load TLE group")

// TLEStore реализует in-memory хранилище TLE с индексами и кешированием.
type TLEStore struct {
	mu sync.RWMutex

	// Основное хранилище: NORAD ID -> TLE
	catalog map[int]*TLE

	// Индекс по группам: group name -> []NORAD ID
	byGroup map[string][]int

	// Индекс по именам (lowercase): name -> []NORAD ID
	byName map[string][]int

	// Метаданные спутников (опционально, из SatNOGS)
	metadata map[int]*SatelliteMetadata

	// Зависимости
	client *CelestrakClient
	config *TLEStoreConfig
	logger *slog.Logger

	// Background updater control
	stopCh chan struct{}
	doneCh chan struct{}
}

// SatelliteMetadata содержит дополнительную информацию о спутнике.
// Может быть загружена из SatNOGS DB.
type SatelliteMetadata struct {
	NoradID   int
	Name      string
	AltNames  []string // Альтернативные названия
	Status    string   // alive, dead, re-entered
	Mode      string   // FM, CW, AFSK, и т.д.
	Uplinks   []Frequency
	Downlinks []Frequency
}

// CacheMeta содержит метаданные файлового кеша.
type CacheMeta struct {
	Groups map[string]CacheGroupMeta `json:"groups"`
}

// CacheGroupMeta метаданные группы в кеше.
type CacheGroupMeta struct {
	UpdatedAt time.Time `json:"updated_at"`
	Count     int       `json:"count"`
}

// Frequency представляет частоту передатчика/приёмника.
type Frequency struct {
	FreqHz   float64 // Частота в Hz
	Mode     string  // FM, CW, BPSK, etc.
	Baudrate int     // Скорость передачи (если применимо)
}

// TLEStoreOption функция настройки TLEStore.
type TLEStoreOption func(*TLEStore)

// WithLogger логгер для TLEStore.
func WithLogger(logger *slog.Logger) TLEStoreOption {
	return func(s *TLEStore) {
		s.logger = logger
	}
}

// WithCelestrakClient устанавливает клиент Celestrak.
func WithCelestrakClient(client *CelestrakClient) TLEStoreOption {
	return func(s *TLEStore) {
		s.client = client
	}
}

// NewTLEStore создаёт новый TLEStore с указанной конфигурацией.
func NewTLEStore(cfg *TLEStoreConfig, opts ...TLEStoreOption) *TLEStore {
	if cfg == nil {
		cfg = DefaultTLEStoreConfig()
	}

	s := &TLEStore{
		catalog:  make(map[int]*TLE),
		byGroup:  make(map[string][]int),
		byName:   make(map[string][]int),
		metadata: make(map[int]*SatelliteMetadata),
		config:   cfg,
		client:   NewCelestrakClient(),
		logger:   slog.Default(),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Start запускает TLEStore: загружает TLE и запускает автообновление.
func (s *TLEStore) Start(ctx context.Context) error {
	s.logger.InfoContext(ctx, "starting TLEStore",
		"groups", s.config.Groups,
		"update_interval", s.config.UpdateInterval,
	)

	// Загрузка всех настроенных групп
	if err := s.LoadAllGroups(ctx); err != nil {
		s.logger.WarnContext(ctx, "initial TLE load had errors", "error", err)
		// Не возвращаем ошибку — можем работать с частичными данными
	}

	// Запуск фонового обновления
	go s.startUpdater(ctx)

	return nil
}

// Stop останавливает фоновое обновление и освобождает ресурсы.
func (s *TLEStore) Stop() {
	s.logger.Info("stopping TLEStore")
	close(s.stopCh)
	<-s.doneCh
	s.logger.Info("TLEStore stopped")
}

// Get возвращает TLE по NORAD ID.
func (s *TLEStore) Get(noradID int) (*TLE, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tle, ok := s.catalog[noradID]
	return tle, ok
}

// GetByGroup возвращает все TLE указанной группы.
func (s *TLEStore) GetByGroup(group string) []*TLE {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids, ok := s.byGroup[strings.ToLower(group)]
	if !ok {
		return nil
	}

	tles := make([]*TLE, 0, len(ids))
	for _, id := range ids {
		if tle, exists := s.catalog[id]; exists {
			tles = append(tles, tle)
		}
	}
	return tles
}

// GetByName возвращает TLE по имени (case-insensitive, частичное совпадение).
func (s *TLEStore) GetByName(name string) []*TLE {
	s.mu.RLock()
	defer s.mu.RUnlock()

	lowerName := strings.ToLower(name)
	var tles []*TLE

	// Точное совпадение по индексу
	if ids, ok := s.byName[lowerName]; ok {
		for _, id := range ids {
			if tle, exists := s.catalog[id]; exists {
				tles = append(tles, tle)
			}
		}
		return tles
	}

	// Частичное совпадение (поиск по всем именам)
	for _, tle := range s.catalog {
		if strings.Contains(strings.ToLower(tle.Name), lowerName) {
			tles = append(tles, tle)
		}
	}
	return tles
}

// GetAll возвращает все TLE в хранилище.
func (s *TLEStore) GetAll() []*TLE {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return slices.Collect(maps.Values(s.catalog))
}

// Add добавляет TLE в хранилище и обновляет индексы.
// Если TLE с таким NORAD ID уже существует, он будет обновлён.
func (s *TLEStore) Add(tle *TLE) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.addInternal(tle, "")
}

// AddWithGroup добавляет TLE с указанием группы.
func (s *TLEStore) AddWithGroup(tle *TLE, group string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.addInternal(tle, group)
}

// Count возвращает количество TLE в хранилище.
func (s *TLEStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.catalog)
}

// StaleCount возвращает количество устаревших TLE.
func (s *TLEStore) StaleCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, tle := range s.catalog {
		if tle.IsStale(s.config.MaxTLEAgeDays) {
			count++
		}
	}
	return count
}

// Groups возвращает список всех групп в хранилище.
func (s *TLEStore) Groups() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return slices.Collect(maps.Keys(s.byGroup))
}

// GroupCount возвращает количество TLE в указанной группе.
func (s *TLEStore) GroupCount(group string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.byGroup[strings.ToLower(group)])
}

// LoadAllGroups загружает все настроенные группы TLE.
func (s *TLEStore) LoadAllGroups(ctx context.Context) error {
	var lastErr error

	for _, group := range s.config.Groups {
		if err := s.LoadGroup(ctx, group); err != nil {
			s.logger.WarnContext(ctx, "failed to load group",
				"group", group,
				"error", err,
			)
			lastErr = err
		}
	}

	s.logger.InfoContext(ctx, "loaded TLE groups",
		"total_count", s.Count(),
		"groups", s.Groups(),
	)

	return lastErr
}

// LoadGroup загружает TLE для указанной группы.
// Стратегия: сначала Celestrak (свежие данные), при ошибке — fallback на кеш.
// После успешной загрузки с Celestrak — сохраняем в кеш.
func (s *TLEStore) LoadGroup(ctx context.Context, group string) error {
	s.logger.DebugContext(ctx, "loading TLE group", "group", group)

	var tles []*TLE
	var err error
	fromCache := false

	// Пробуем загрузить с Celestrak
	tles, err = s.client.FetchGroup(ctx, SatelliteGroup(group))
	if err != nil {
		s.logger.WarnContext(ctx, "failed to fetch from Celestrak, trying cache",
			"group", group,
			"error", err,
		)

		// Fallback на файловый кеш
		tles, err = s.loadGroupFromCache(group)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to load from cache",
				"group", group,
				"error", err,
			)
			return fmt.Errorf("%w: %s (celestrak and cache both failed)", ErrLoadGroupFailed, group)
		}
		fromCache = true
		s.logger.InfoContext(ctx, "loaded TLE group from cache",
			"group", group,
			"count", len(tles),
		)
	} else {
		// Успешно загрузили с Celestrak — сохраняем в кеш
		if saveErr := s.saveGroupToCache(group, tles); saveErr != nil {
			s.logger.WarnContext(ctx, "failed to save to cache",
				"group", group,
				"error", saveErr,
			)
		}
	}

	// Добавляем TLE в хранилище
	s.mu.Lock()
	for _, tle := range tles {
		s.addInternal(tle, group)
	}
	s.mu.Unlock()

	if !fromCache {
		s.logger.InfoContext(ctx, "loaded TLE group from Celestrak",
			"group", group,
			"count", len(tles),
		)
	}

	return nil
}

// addInternal добавляет TLE без блокировки
func (s *TLEStore) addInternal(tle *TLE, group string) {
	if tle == nil {
		return
	}

	// Добавляем/обновляем в каталоге
	s.catalog[tle.NoradID] = tle

	// Обновляем индекс по группе
	if group != "" {
		lowerGroup := strings.ToLower(group)
		s.addToIndex(s.byGroup, lowerGroup, tle.NoradID)
	}

	// Обновляем индекс по имени
	if tle.Name != "" {
		lowerName := strings.ToLower(tle.Name)
		s.addToIndex(s.byName, lowerName, tle.NoradID)
	}
}

// addToIndex добавляет ID в индекс, избегая дубликатов.
func (s *TLEStore) addToIndex(index map[string][]int, key string, id int) {
	ids := index[key]
	if slices.Contains(ids, id) {
		return // Уже есть
	}
	index[key] = append(ids, id)
}

// startUpdater запускает фоновое обновление TLE.
func (s *TLEStore) startUpdater(ctx context.Context) {
	defer close(s.doneCh)

	ticker := NewSafeTicker(s.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.InfoContext(ctx, "updater stopped by context")
			return
		case <-s.stopCh:
			s.logger.InfoContext(ctx, "updater stopped by stop signal")
			return
		case <-ticker.C:
			s.logger.InfoContext(ctx, "starting scheduled TLE update")
			if err := s.LoadAllGroups(ctx); err != nil {
				s.logger.WarnContext(ctx, "scheduled TLE update had errors", "error", err)
			}
		}
	}
}

// SafeTicker обёртка над time.Ticker для безопасного использования.
type SafeTicker struct {
	*time.Ticker

	C <-chan time.Time
}

// NewSafeTicker создаёт новый SafeTicker.
func NewSafeTicker(d time.Duration) *SafeTicker {
	t := time.NewTicker(d)
	return &SafeTicker{Ticker: t, C: t.C}
}

// loadCacheMeta загружает метаданные кеша.
func (s *TLEStore) loadCacheMeta() (*CacheMeta, error) {
	metaPath := filepath.Join(s.config.CacheDir, cacheMetaFilename)

	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &CacheMeta{Groups: make(map[string]CacheGroupMeta)}, nil
		}
		return nil, fmt.Errorf("reading cache meta: %w", err)
	}

	var meta CacheMeta
	err = json.Unmarshal(data, &meta)
	if err != nil {
		return nil, fmt.Errorf("parsing cache meta: %w", err)
	}

	if meta.Groups == nil {
		meta.Groups = make(map[string]CacheGroupMeta)
	}

	return &meta, nil
}

// saveCacheMeta сохраняет метаданные кеша.
func (s *TLEStore) saveCacheMeta(meta *CacheMeta) error {
	// Создаём директорию кеша если не существует
	if err := os.MkdirAll(s.config.CacheDir, 0750); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	metaPath := filepath.Join(s.config.CacheDir, cacheMetaFilename)

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling cache meta: %w", err)
	}

	err = os.WriteFile(metaPath, data, 0600)
	if err != nil {
		return fmt.Errorf("writing cache meta: %w", err)
	}

	return nil
}

// isCacheFresh проверяет, свежий ли кеш для группы.
func (s *TLEStore) isCacheFresh(meta *CacheMeta, group string) bool {
	groupMeta, ok := meta.Groups[strings.ToLower(group)]
	if !ok {
		return false
	}

	age := time.Since(groupMeta.UpdatedAt)
	maxAge := time.Duration(s.config.MaxTLEAgeDays * 24 * float64(time.Hour))

	return age < maxAge
}

// loadGroupFromCache загружает TLE группы из файлового кеша.
func (s *TLEStore) loadGroupFromCache(group string) ([]*TLE, error) {
	cachePath := filepath.Join(s.config.CacheDir, strings.ToLower(group)+tleCacheExtension)

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("reading cache file: %w", err)
	}

	tles, err := ParseTLEBatch(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing cached TLE: %w", err)
	}

	return tles, nil
}

// saveGroupToCache сохраняет TLE группы в файловый кеш.
func (s *TLEStore) saveGroupToCache(group string, tles []*TLE) error {
	// Создаём директорию кеша если не существует
	if err := os.MkdirAll(s.config.CacheDir, 0750); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	cachePath := filepath.Join(s.config.CacheDir, strings.ToLower(group)+tleCacheExtension)

	// Формируем содержимое файла в 3-line формате
	var builder strings.Builder
	for _, tle := range tles {
		builder.WriteString(tle.String())
		builder.WriteString("\n")
	}

	if err := os.WriteFile(cachePath, []byte(builder.String()), 0600); err != nil {
		return fmt.Errorf("writing cache file: %w", err)
	}

	// Обновляем метаданные
	meta, err := s.loadCacheMeta()
	if err != nil {
		s.logger.Warn("failed to load cache meta", "error", err)
		meta = &CacheMeta{Groups: make(map[string]CacheGroupMeta)}
	}

	meta.Groups[strings.ToLower(group)] = CacheGroupMeta{
		UpdatedAt: time.Now(),
		Count:     len(tles),
	}

	err = s.saveCacheMeta(meta)
	if err != nil {
		s.logger.Warn("failed to save cache meta", "error", err)
	}

	return nil
}
