package tracker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// makeTLELineTest добавляет корректную контрольную сумму к строке TLE (68 символов без checksum).
func makeTLELineTest(line68 string) string {
	if len(line68) != 68 {
		panic(fmt.Sprintf("line must be 68 chars, got %d", len(line68)))
	}
	checksum := calculateChecksum(line68)
	return line68 + strconv.Itoa(checksum)
}

// Тестовые TLE данные с корректными контрольными суммами.
var (
	testISSLine1    = makeTLELineTest("1 25544U 98067A   24001.50000000  .00016717  00000-0  10270-3 0  999")
	testISSLine2    = makeTLELineTest("2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.4981557142340")
	testMeteorLine1 = makeTLELineTest("1 40069U 14037A   24001.50000000  .00000123  00000-0  12345-4 0  999")
	testMeteorLine2 = makeTLELineTest("2 40069  98.5200  45.6789 0001234 123.4567 236.7890 14.2098765432109")

	testTLEData = "ISS (ZARYA)\n" + testISSLine1 + "\n" + testISSLine2 + "\n" +
		"METEOR-M2\n" + testMeteorLine1 + "\n" + testMeteorLine2
)

func TestNewTLEStore(t *testing.T) {
	cfg := DefaultTLEStoreConfig()
	store := NewTLEStore(cfg)

	if store == nil {
		t.Fatal("NewTLEStore returned nil")
	}
	if store.config != cfg {
		t.Error("config not set correctly")
	}
	if store.catalog == nil {
		t.Error("catalog map not initialized")
	}
	if store.byGroup == nil {
		t.Error("byGroup map not initialized")
	}
	if store.byName == nil {
		t.Error("byName map not initialized")
	}
}

func TestNewTLEStore_NilConfig(t *testing.T) {
	store := NewTLEStore(nil)

	if store == nil {
		t.Fatal("NewTLEStore returned nil")
	}
	if store.config == nil {
		t.Error("default config not applied")
	}
}

func TestTLEStore_AddAndGet(t *testing.T) {
	store := NewTLEStore(nil)

	tle := &TLE{
		NoradID: 25544,
		Name:    "ISS (ZARYA)",
		Epoch:   time.Now(),
	}

	store.Add(tle)

	got, ok := store.Get(25544)
	if !ok {
		t.Fatal("Get returned false for existing TLE")
	}
	if got.NoradID != tle.NoradID {
		t.Errorf("NoradID mismatch: got %d, want %d", got.NoradID, tle.NoradID)
	}
	if got.Name != tle.Name {
		t.Errorf("Name mismatch: got %s, want %s", got.Name, tle.Name)
	}
}

func TestTLEStore_GetNotFound(t *testing.T) {
	store := NewTLEStore(nil)

	_, ok := store.Get(99999)
	if ok {
		t.Error("Get returned true for non-existing TLE")
	}
}

func TestTLEStore_AddWithGroup(t *testing.T) {
	store := NewTLEStore(nil)

	tle := &TLE{
		NoradID: 25544,
		Name:    "ISS (ZARYA)",
	}

	store.AddWithGroup(tle, "stations")

	tles := store.GetByGroup("stations")
	if len(tles) != 1 {
		t.Fatalf("GetByGroup returned %d TLEs, want 1", len(tles))
	}
	if tles[0].NoradID != 25544 {
		t.Errorf("NoradID mismatch: got %d, want %d", tles[0].NoradID, 25544)
	}
}

func TestTLEStore_GetByGroup_CaseInsensitive(t *testing.T) {
	store := NewTLEStore(nil)

	tle := &TLE{NoradID: 25544, Name: "ISS"}
	store.AddWithGroup(tle, "STATIONS")

	// Должен находить независимо от регистра
	tles := store.GetByGroup("stations")
	if len(tles) != 1 {
		t.Errorf("GetByGroup(lowercase) returned %d TLEs, want 1", len(tles))
	}

	tles = store.GetByGroup("STATIONS")
	if len(tles) != 1 {
		t.Errorf("GetByGroup(uppercase) returned %d TLEs, want 1", len(tles))
	}
}

func TestTLEStore_GetByName(t *testing.T) {
	store := NewTLEStore(nil)

	store.Add(&TLE{NoradID: 25544, Name: "ISS (ZARYA)"})
	store.Add(&TLE{NoradID: 40069, Name: "METEOR-M 2"})

	// Точное совпадение по индексу (lowercase)
	tles := store.GetByName("iss (zarya)")
	if len(tles) != 1 {
		t.Errorf("GetByName(exact) returned %d TLEs, want 1", len(tles))
	}

	// Частичное совпадение
	tles = store.GetByName("meteor")
	if len(tles) != 1 {
		t.Errorf("GetByName(partial) returned %d TLEs, want 1", len(tles))
	}
}

func TestTLEStore_GetAll(t *testing.T) {
	store := NewTLEStore(nil)

	store.Add(&TLE{NoradID: 25544, Name: "ISS"})
	store.Add(&TLE{NoradID: 40069, Name: "METEOR-M 2"})

	tles := store.GetAll()
	if len(tles) != 2 {
		t.Errorf("GetAll returned %d TLEs, want 2", len(tles))
	}
}

func TestTLEStore_Count(t *testing.T) {
	store := NewTLEStore(nil)

	if store.Count() != 0 {
		t.Errorf("Count() on empty store: got %d, want 0", store.Count())
	}

	store.Add(&TLE{NoradID: 25544})
	store.Add(&TLE{NoradID: 40069})

	if store.Count() != 2 {
		t.Errorf("Count() after 2 adds: got %d, want 2", store.Count())
	}
}

func TestTLEStore_StaleCount(t *testing.T) {
	cfg := DefaultTLEStoreConfig()
	cfg.MaxTLEAgeDays = 7
	store := NewTLEStore(cfg)

	// Свежий TLE
	store.Add(&TLE{NoradID: 25544, Epoch: time.Now()})

	// Устаревший TLE (10 дней назад)
	store.Add(&TLE{NoradID: 40069, Epoch: time.Now().Add(-10 * 24 * time.Hour)})

	if store.StaleCount() != 1 {
		t.Errorf("StaleCount(): got %d, want 1", store.StaleCount())
	}
}

func TestTLEStore_Groups(t *testing.T) {
	store := NewTLEStore(nil)

	store.AddWithGroup(&TLE{NoradID: 25544}, "stations")
	store.AddWithGroup(&TLE{NoradID: 40069}, "weather")

	groups := store.Groups()
	if len(groups) != 2 {
		t.Errorf("Groups() returned %d groups, want 2", len(groups))
	}
}

func TestTLEStore_GroupCount(t *testing.T) {
	store := NewTLEStore(nil)

	store.AddWithGroup(&TLE{NoradID: 25544}, "stations")
	store.AddWithGroup(&TLE{NoradID: 25545}, "stations")
	store.AddWithGroup(&TLE{NoradID: 40069}, "weather")

	if store.GroupCount("stations") != 2 {
		t.Errorf("GroupCount(stations): got %d, want 2", store.GroupCount("stations"))
	}
	if store.GroupCount("weather") != 1 {
		t.Errorf("GroupCount(weather): got %d, want 1", store.GroupCount("weather"))
	}
	if store.GroupCount("nonexistent") != 0 {
		t.Errorf("GroupCount(nonexistent): got %d, want 0", store.GroupCount("nonexistent"))
	}
}

func TestTLEStore_IndexNoDuplicates(t *testing.T) {
	store := NewTLEStore(nil)

	// Добавляем один и тот же TLE дважды в одну группу
	tle := &TLE{NoradID: 25544, Name: "ISS"}
	store.AddWithGroup(tle, "stations")
	store.AddWithGroup(tle, "stations")

	if store.GroupCount("stations") != 1 {
		t.Errorf("Index should not have duplicates: got %d, want 1", store.GroupCount("stations"))
	}
}

// ============================================================================
// Тесты файлового кеша
// ============================================================================

func TestTLEStore_SaveAndLoadCache(t *testing.T) {
	tempDir := t.TempDir()

	cfg := DefaultTLEStoreConfig()
	cfg.CacheDir = tempDir
	store := NewTLEStore(cfg)

	// Парсим тестовые TLE
	tles, err := ParseTLEBatch(testTLEData)
	if err != nil {
		t.Fatalf("Failed to parse test TLE: %v", err)
	}

	// Сохраняем в кеш
	err = store.saveGroupToCache("test_group", tles)
	if err != nil {
		t.Fatalf("saveGroupToCache failed: %v", err)
	}

	// Проверяем, что файл создан
	cachePath := filepath.Join(tempDir, "test_group.tle")
	_, err = os.Stat(cachePath)
	if os.IsNotExist(err) {
		t.Error("Cache file not created")
	}

	// Проверяем метаданные
	metaPath := filepath.Join(tempDir, "cache_meta.json")
	_, err = os.Stat(metaPath)
	if os.IsNotExist(err) {
		t.Error("Cache meta file not created")
	}

	// Загружаем из кеша
	loadedTLEs, err := store.loadGroupFromCache("test_group")
	if err != nil {
		t.Fatalf("loadGroupFromCache failed: %v", err)
	}

	if len(loadedTLEs) != len(tles) {
		t.Errorf("Loaded TLE count mismatch: got %d, want %d", len(loadedTLEs), len(tles))
	}
}

func TestTLEStore_CacheMeta(t *testing.T) {
	tempDir := t.TempDir()

	cfg := DefaultTLEStoreConfig()
	cfg.CacheDir = tempDir
	store := NewTLEStore(cfg)

	// Начальное состояние — пустые метаданные
	meta, err := store.loadCacheMeta()
	if err != nil {
		t.Fatalf("loadCacheMeta failed: %v", err)
	}
	if len(meta.Groups) != 0 {
		t.Errorf("Initial meta should be empty, got %d groups", len(meta.Groups))
	}

	// Сохраняем метаданные
	meta.Groups["test"] = CacheGroupMeta{
		UpdatedAt: time.Now(),
		Count:     10,
	}
	err = store.saveCacheMeta(meta)
	if err != nil {
		t.Fatalf("saveCacheMeta failed: %v", err)
	}

	// Загружаем и проверяем
	loaded, err := store.loadCacheMeta()
	if err != nil {
		t.Fatalf("loadCacheMeta after save failed: %v", err)
	}
	if loaded.Groups["test"].Count != 10 {
		t.Errorf("Count mismatch: got %d, want 10", loaded.Groups["test"].Count)
	}
}

func TestTLEStore_IsCacheFresh(t *testing.T) {
	cfg := DefaultTLEStoreConfig()
	cfg.MaxTLEAgeDays = 7
	store := NewTLEStore(cfg)

	meta := &CacheMeta{Groups: make(map[string]CacheGroupMeta)}

	// Свежий кеш
	meta.Groups["fresh"] = CacheGroupMeta{
		UpdatedAt: time.Now().Add(-1 * time.Hour),
		Count:     10,
	}
	if !store.isCacheFresh(meta, "fresh") {
		t.Error("Cache updated 1 hour ago should be fresh")
	}

	// Устаревший кеш
	meta.Groups["stale"] = CacheGroupMeta{
		UpdatedAt: time.Now().Add(-10 * 24 * time.Hour),
		Count:     10,
	}
	if store.isCacheFresh(meta, "stale") {
		t.Error("Cache updated 10 days ago should not be fresh")
	}

	// Несуществующая группа
	if store.isCacheFresh(meta, "nonexistent") {
		t.Error("Non-existent group should not be fresh")
	}
}

// ============================================================================
// Тесты с mock-сервером
// ============================================================================

func TestTLEStore_LoadGroup_FromCelestrak(t *testing.T) {
	// Mock Celestrak сервер
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testTLEData))
	}))
	defer srv.Close()

	tempDir := t.TempDir()

	cfg := DefaultTLEStoreConfig()
	cfg.CacheDir = tempDir

	client := NewCelestrakClient(WithBaseURL(srv.URL), WithRateLimit(0))
	store := NewTLEStore(cfg, WithCelestrakClient(client))

	ctx := context.Background()
	err := store.LoadGroup(ctx, "test")
	if err != nil {
		t.Fatalf("LoadGroup failed: %v", err)
	}

	if store.Count() != 2 {
		t.Errorf("Count after LoadGroup: got %d, want 2", store.Count())
	}

	// Проверяем, что данные сохранены в кеш
	cachePath := filepath.Join(tempDir, "test.tle")
	_, err = os.Stat(cachePath)
	if os.IsNotExist(err) {
		t.Error("Cache file should be created after successful load")
	}
}

func TestTLEStore_LoadGroup_FallbackToCache(t *testing.T) {
	tempDir := t.TempDir()

	// Создаём кеш-файл вручную
	cachePath := filepath.Join(tempDir, "test.tle")
	err := os.WriteFile(cachePath, []byte(testTLEData), 0644)
	if err != nil {
		t.Fatalf("Failed to create cache file: %v", err)
	}

	// Mock сервер, который возвращает ошибку
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := DefaultTLEStoreConfig()
	cfg.CacheDir = tempDir

	client := NewCelestrakClient(WithBaseURL(srv.URL), WithRateLimit(0), WithMaxRetries(0))
	store := NewTLEStore(cfg, WithCelestrakClient(client))

	ctx := context.Background()
	err = store.LoadGroup(ctx, "test")
	if err != nil {
		t.Fatalf("LoadGroup should succeed with cache fallback: %v", err)
	}

	if store.Count() != 2 {
		t.Errorf("Count after fallback: got %d, want 2", store.Count())
	}
}

// TestTLEStore_Start_FreshCacheSkipsNetwork: свежий кеш отдаётся сразу, HTTP не вызывается.
func TestTLEStore_Start_FreshCacheSkipsNetwork(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testTLEData))
	}))
	t.Cleanup(srv.Close)

	store := newCachedStore(t, srv.URL, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		store.Stop()
	})

	started := time.Now()
	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Start blocked for %s", elapsed)
	}
	if store.Count() != 2 {
		t.Fatalf("Count = %d, want 2 from cache", store.Count())
	}

	time.Sleep(200 * time.Millisecond)
	if n := hits.Load(); n != 0 {
		t.Fatalf("network hits = %d, want 0", n)
	}
}

// TestTLEStore_Start_StaleCacheServesThenRefreshes: устаревший кеш уже в памяти, сеть идёт следом.
func TestTLEStore_Start_StaleCacheServesThenRefreshes(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-r.Context().Done():
			timer.Stop()
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	store := newCachedStore(t, srv.URL, -2*time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		store.Stop()
	})

	started := time.Now()
	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Start blocked for %s", elapsed)
	}
	if store.Count() != 2 {
		t.Fatalf("Count = %d, want cached 2 before network", store.Count())
	}

	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Fatal("expected background refresh of stale cache")
	}
}

// TestTLEStore_Start_MissingCacheDoesNotBlock: без кеша Start не ждёт ответ Celestrak.
func TestTLEStore_Start_MissingCacheDoesNotBlock(t *testing.T) {
	entered := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultTLEStoreConfig()
	cfg.CacheDir = t.TempDir()
	cfg.Groups = []string{"amateur"}
	cfg.UpdateInterval = time.Hour
	client := NewCelestrakClient(WithBaseURL(srv.URL), WithRateLimit(0), WithMaxRetries(0))
	store := NewTLEStore(cfg, WithCelestrakClient(client))

	ctx, cancel := context.WithCancel(context.Background())
	startedOK := false
	t.Cleanup(func() {
		cancel()
		if startedOK {
			store.Stop()
		}
	})

	started := time.Now()
	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	startedOK = true
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Start blocked for %s", elapsed)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("expected background fetch when cache is missing")
	}
}

// newCachedStore готовит хранилище с файловым кешем. age сдвигает метку свежести.
func newCachedStore(t *testing.T, baseURL string, age time.Duration) *TLEStore {
	t.Helper()
	cfg := DefaultTLEStoreConfig()
	cfg.CacheDir = t.TempDir()
	cfg.Groups = []string{"amateur"}
	cfg.UpdateInterval = time.Hour
	client := NewCelestrakClient(WithBaseURL(baseURL), WithRateLimit(0), WithMaxRetries(0))
	store := NewTLEStore(cfg, WithCelestrakClient(client))

	tles, err := ParseTLEBatch(testTLEData)
	if err != nil {
		t.Fatalf("ParseTLEBatch: %v", err)
	}
	if err := store.saveGroupToCache("amateur", tles); err != nil {
		t.Fatalf("saveGroupToCache: %v", err)
	}
	if age != 0 {
		meta, err := store.loadCacheMeta()
		if err != nil {
			t.Fatalf("loadCacheMeta: %v", err)
		}
		meta.Groups["amateur"] = CacheGroupMeta{
			UpdatedAt: time.Now().Add(age),
			Count:     len(tles),
		}
		if err := store.saveCacheMeta(meta); err != nil {
			t.Fatalf("saveCacheMeta: %v", err)
		}
	}
	return store
}

// ============================================================================
// Benchmark
// ============================================================================

func BenchmarkTLEStore_Get(b *testing.B) {
	store := NewTLEStore(nil)

	// Добавляем 1000 TLE
	for i := range 1000 {
		store.Add(&TLE{NoradID: i, Name: "SAT-" + string(rune('A'+i%26))})
	}

	b.ResetTimer()
	for b.Loop() {
		store.Get(500)
	}
}

func BenchmarkTLEStore_GetByGroup(b *testing.B) {
	store := NewTLEStore(nil)

	// Добавляем 1000 TLE в группу
	for i := range 1000 {
		store.AddWithGroup(&TLE{NoradID: i}, "test_group")
	}

	b.ResetTimer()
	for b.Loop() {
		store.GetByGroup("test_group")
	}
}

func BenchmarkTLEStore_Add(b *testing.B) {
	store := NewTLEStore(nil)

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		store.Add(&TLE{NoradID: i, Name: "SAT"})
	}
}
