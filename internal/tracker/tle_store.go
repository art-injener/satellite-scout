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

	// Источник и время загрузки по группам (ключ — имя в нижнем регистре)
	fetch map[string]groupFetch

	// Вызывается после каждой фоновой загрузки, см. SetOnRefresh
	onRefresh func()
	// Вызывается при смене прогресса пачек Space-Track, см. SetOnProgress
	onProgress func()

	// Последняя сверка пролётов со сторонним прогнозом, см. SetPassCheck
	passCheck *PassCheck

	// Прогресс текущего прохода Space-Track (под s.mu).
	loading   bool
	loadPhase string
	loadDone  int
	loadTotal int

	// Зависимости
	client      *CelestrakClient
	spacetrack  *SpaceTrackClient // nil — учётки нет, элементы только из Celestrak
	sonik       *SonikClient      // справочник sonik (имена, свежие запуски); nil — без sonik
	composition Composition       // NORAD из curated/HA/AMSAT; nil — только свежий sonik
	sink        CatalogSink       // получает итог каждого прохода каталога; nil — никто
	namer       Namer             // подставляет отображаемое имя КА; nil — имя из TLE
	config      *TLEStoreConfig
	logger      *slog.Logger

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

// Namer возвращает отображаемое имя КА по NORAD и имени из TLE.
type Namer func(norad int, tleName string) string

// WithNamer подставляет имя КА при добавлении элемента в хранилище: так одно имя видят
// пролёты, карта, группа и поиск. Изменённое имя пишется в копию, TLE вызывающего не меняется.
func WithNamer(namer Namer) TLEStoreOption {
	return func(s *TLEStore) {
		s.namer = namer
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
		fetch:    make(map[string]groupFetch),
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

// Start поднимает каталог из файлового кеша и запускает автообновление.
// Ответ сети не ждём: она идёт в фоне и только для кеша, которого нет
// или который старше UpdateInterval. Так HTTP-сервер стартует, даже если источник недоступен.
func (s *TLEStore) Start(ctx context.Context) error {
	s.logger.InfoContext(ctx, "starting TLEStore",
		"update_interval", s.config.UpdateInterval,
	)

	s.loadLegacyGroupCaches()
	s.loadCatalogCache()
	needNetwork := s.needsNetworkRefresh()
	s.logger.InfoContext(ctx, "TLE cache loaded",
		"count", s.Count(),
		"need_network", needNetwork,
	)
	if st := s.Status(time.Now()); st.Level != TLELevelOK {
		s.logger.WarnContext(ctx, "TLE cache is outdated, passes may be inaccurate",
			slog.String("level", string(st.Level)),
			slog.Any("reasons", st.Reasons),
			slog.Float64("median_age_h", st.MedianAgeHours),
		)
	}

	go s.startUpdater(ctx, needNetwork)

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

// loadLegacyGroupCaches поднимает с диска старые файлы групп Celestrak (stations.tle и т.п.).
// Только локальный посев; сеть по GROUP= не дергается.
func (s *TLEStore) loadLegacyGroupCaches() {
	meta, metaErr := s.loadCacheMeta()
	if metaErr != nil {
		meta = &CacheMeta{}
	}
	for _, group := range DefaultTLEGroups {
		tles, err := s.loadGroupFromCache(group)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				s.logger.Warn("failed to load legacy TLE cache",
					"group", group,
					slogKeyError, err,
				)
			}
			continue
		}
		if len(tles) == 0 {
			continue
		}
		s.mu.Lock()
		for _, tle := range tles {
			s.addInternal(tle, group)
		}
		s.setFetch(group, func(f *groupFetch) {
			f.source = TLESourceCache
			f.fetchedAt = meta.Groups[strings.ToLower(group)].UpdatedAt
		})
		s.mu.Unlock()
		s.logger.Info("loaded legacy TLE group from cache",
			"group", group,
			"count", len(tles),
		)
	}
}

// cacheWithinInterval сообщает, что кеш группы моложе интервала обновления.
func (s *TLEStore) cacheWithinInterval(meta *CacheMeta, group string) bool {
	if meta == nil || s.config.UpdateInterval <= 0 {
		return false
	}
	groupMeta, ok := meta.Groups[strings.ToLower(group)]
	if !ok || groupMeta.UpdatedAt.IsZero() || groupMeta.Count <= 0 {
		return false
	}
	return time.Since(groupMeta.UpdatedAt) < s.config.UpdateInterval
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
			slogKeyError, err,
		)
		fetchErr := err.Error()
		s.mu.Lock()
		s.setFetch(group, func(f *groupFetch) { f.lastErr = fetchErr })
		s.mu.Unlock()

		// Fallback на файловый кеш
		tles, err = s.loadGroupFromCache(group)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to load from cache",
				"group", group,
				slogKeyError, err,
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
				slogKeyError, saveErr,
			)
		}
	}

	var cachedAt time.Time
	if fromCache {
		if meta, metaErr := s.loadCacheMeta(); metaErr == nil {
			cachedAt = meta.Groups[strings.ToLower(group)].UpdatedAt
		}
	}

	// Добавляем TLE в хранилище
	s.mu.Lock()
	for _, tle := range tles {
		s.addInternal(tle, group)
	}
	s.setFetch(group, func(f *groupFetch) {
		if fromCache {
			f.source = TLESourceCache
			if f.fetchedAt.IsZero() {
				f.fetchedAt = cachedAt
			}
			return
		}
		f.source = TLESourceCelestrak
		f.fetchedAt = time.Now()
		f.lastErr = ""
	})
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
	if s.namer != nil {
		if name := s.namer(tle.NoradID, tle.Name); name != tle.Name {
			renamed := *tle
			renamed.Name = name
			tle = &renamed
		}
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

// startUpdater сначала добирает устаревшее, затем обновляет каталог по таймеру.
func (s *TLEStore) startUpdater(ctx context.Context, needNetwork bool) {
	defer close(s.doneCh)

	if needNetwork {
		s.logger.InfoContext(ctx, "refreshing TLE from network")
		s.refresh(ctx)
		s.notifyRefresh()
	}

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
			s.refresh(ctx)
			s.notifyRefresh()
		}
	}
}

// notifyRefresh зовёт колбэк SetOnRefresh, если он задан.
func (s *TLEStore) notifyRefresh() {
	if s.onRefresh != nil {
		s.onRefresh()
	}
}

// notifyProgress зовёт колбэк SetOnProgress, если он задан.
func (s *TLEStore) notifyProgress() {
	if s.onProgress != nil {
		s.onProgress()
	}
}

// beginLoad включает индикатор загрузки фазы (пачки 0 из total).
func (s *TLEStore) beginLoad(phase string, total int) {
	s.mu.Lock()
	s.loading = true
	s.loadPhase = phase
	s.loadDone = 0
	s.loadTotal = total
	s.mu.Unlock()
	s.notifyProgress()
}

// reportLoadProgress обновляет счётчик завершённых пачек.
func (s *TLEStore) reportLoadProgress(done, total int) {
	s.mu.Lock()
	s.loading = true
	s.loadDone = done
	s.loadTotal = total
	s.mu.Unlock()
	s.notifyProgress()
}

// endLoad снимает индикатор загрузки.
func (s *TLEStore) endLoad() {
	s.mu.Lock()
	s.loading = false
	s.loadPhase = ""
	s.loadDone = 0
	s.loadTotal = 0
	s.mu.Unlock()
	s.notifyProgress()
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
		s.logger.Warn("failed to load cache meta", slogKeyError, err)
		meta = &CacheMeta{Groups: make(map[string]CacheGroupMeta)}
	}

	meta.Groups[strings.ToLower(group)] = CacheGroupMeta{
		UpdatedAt: time.Now(),
		Count:     len(tles),
	}

	err = s.saveCacheMeta(meta)
	if err != nil {
		s.logger.Warn("failed to save cache meta", slogKeyError, err)
	}

	return nil
}
