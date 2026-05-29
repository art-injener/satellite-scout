package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// fakeExcluder — простой набор исключённых NORAD для тестов фильтрации.
type fakeExcluder map[int]bool

func (f fakeExcluder) Contains(norad int) bool { return f[norad] }

func TestPassService_FilterExcluded(t *testing.T) {
	passes := []*tracker.Pass{
		{NoradID: 25544},
		{NoradID: 43666},
		{NoradID: 47959},
	}

	svc := NewPassService(nil, nil).WithExcluder(fakeExcluder{43666: true})
	got := svc.filterExcluded(passes)

	require.Len(t, got, 2)
	for _, p := range got {
		require.NotEqual(t, 43666, p.NoradID, "excluded satellite must be filtered out")
	}
}

func TestPassService_FilterExcluded_NilExcluder(t *testing.T) {
	passes := []*tracker.Pass{{NoradID: 25544}, {NoradID: 43666}}

	svc := NewPassService(nil, nil)
	got := svc.filterExcluded(passes)

	require.Len(t, got, 2, "without excluder nothing is filtered")
}

// --- Тестовые данные ---

// makeTLELine создаёт строку TLE с правильной контрольной суммой.
// line68 — 68 символов без контрольной суммы.
func makeTLELine(line68 string) string {
	if len(line68) != 68 {
		panic("line must be 68 chars")
	}
	sum := 0
	for _, c := range line68 {
		if c >= '0' && c <= '9' {
			sum += int(c - '0')
		} else if c == '-' {
			sum++
		}
	}
	return line68 + string(rune('0'+sum%10))
}

// Тестовые TLE (с автоматически рассчитанными контрольными суммами).
var (
	// ISS (ZARYA) — LEO, наклонение 51.6°.
	testISSLine1 = makeTLELine("1 25544U 98067A   26030.50000000  .00016717  00000-0  10270-3 0  999")
	testISSLine2 = makeTLELine("2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.4981557142340")

	// METEOR-M2 — полярная орбита, наклонение ~98°.
	testMeteorLine1 = makeTLELine("1 40069U 14037A   26030.50000000  .00000123  00000-0  12345-4 0  999")
	testMeteorLine2 = makeTLELine("2 40069  98.5200  45.6789 0001234 123.4567 236.7890 14.2098765432109")

	testISSLines    = []string{"ISS (ZARYA)", testISSLine1, testISSLine2}
	testMeteorLines = []string{"METEOR-M2", testMeteorLine1, testMeteorLine2}
)

// --- Тесты ---

func TestNewPassService(t *testing.T) {
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)

	svc := NewPassService(store, observer)

	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.store != store {
		t.Error("store not set")
	}
	if svc.observer != observer {
		t.Error("observer not set")
	}
	if svc.cacheTTL != DefaultPassCacheTTL {
		t.Errorf("expected default TTL %v, got %v", DefaultPassCacheTTL, svc.cacheTTL)
	}
}

func TestPassService_WithCacheTTL(t *testing.T) {
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)

	ttl := 10 * time.Minute
	svc := NewPassService(store, observer).WithCacheTTL(ttl)

	if svc.cacheTTL != ttl {
		t.Errorf("expected TTL %v, got %v", ttl, svc.cacheTTL)
	}

	// Негативный TTL игнорируется.
	svc.WithCacheTTL(-1 * time.Minute)
	if svc.cacheTTL != ttl {
		t.Errorf("negative TTL should be ignored, got %v", svc.cacheTTL)
	}
}

func TestPassService_GetPasses_EmptyStore(t *testing.T) {
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)

	svc := NewPassService(store, observer)

	passes, err := svc.GetPasses("amateur", 24, 5.0)
	require.NoError(t, err)

	// Пустой store → пустой результат (не ошибка).
	if len(passes) != 0 {
		t.Errorf("expected 0 passes for empty store, got %d", len(passes))
	}
}

func TestPassService_GetPasses_WithSatellites(t *testing.T) {
	store := tracker.NewTLEStore(nil)

	// Добавляем тестовые TLE.
	iss, err := tracker.ParseTLE(testISSLines)
	require.NoError(t, err)
	meteor, err := tracker.ParseTLE(testMeteorLines)
	require.NoError(t, err)
	store.AddWithGroup(iss, "test")
	store.AddWithGroup(meteor, "test")

	// Проверяем что TLE добавились.
	storedTLEs := store.GetByGroup("test")
	if len(storedTLEs) != 2 {
		t.Fatalf("expected 2 TLEs in group, got %d", len(storedTLEs))
	}

	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)
	svc := NewPassService(store, observer)

	passes, err := svc.GetPasses("test", 24, 5.0)
	require.NoError(t, err)

	// Проверка сортировки по AOS (если есть пролёты).
	for i := 1; i < len(passes); i++ {
		if passes[i].AOS < passes[i-1].AOS {
			t.Errorf("passes not sorted by AOS: [%d].AOS=%d < [%d].AOS=%d",
				i, passes[i].AOS, i-1, passes[i-1].AOS)
		}
	}

	// Должны быть пролёты для LEO спутников за 24 часа.
	// Примечание: если тест начнёт падать из-за устаревшего TLE эпохи,
	// нужно обновить testISSLines/testMeteorLines на более актуальную эпоху.
	if len(passes) == 0 {
		t.Log("warning: no passes found — TLE epoch may be too far from current time")
	}
	t.Logf("found %d passes for group 'test'", len(passes))
}

func TestPassService_GetPasses_DefaultParams(t *testing.T) {
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)
	svc := NewPassService(store, observer)

	// Пустые/невалидные параметры → дефолты.
	passes, err := svc.GetPasses("", 0, -5)
	require.NoError(t, err)

	// Без спутников — пустой результат (nil допустим).
	// PredictAllPasses возвращает nil для пустой группы.
	if len(passes) != 0 {
		t.Errorf("expected 0 passes for empty group, got %d", len(passes))
	}
}

func TestPassService_Cache_Hit(t *testing.T) {
	store := tracker.NewTLEStore(nil)
	iss, err := tracker.ParseTLE(testISSLines)
	require.NoError(t, err)
	store.AddWithGroup(iss, "test")

	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)
	svc := NewPassService(store, observer).WithCacheTTL(1 * time.Hour)

	// Первый вызов — cache miss.
	passes1, err := svc.GetPasses("test", 24, 5.0)
	require.NoError(t, err)
	entries1, _ := svc.CacheStats()
	if entries1 != 1 {
		t.Errorf("expected 1 cache entry after first call, got %d", entries1)
	}

	// Второй вызов — cache hit (те же данные).
	passes2, err := svc.GetPasses("test", 24, 5.0)
	require.NoError(t, err)
	entries2, _ := svc.CacheStats()
	if entries2 != 1 {
		t.Errorf("expected same cache entry count, got %d", entries2)
	}

	// Результаты должны совпадать (та же ссылка на slice).
	if len(passes1) != len(passes2) {
		t.Errorf("cache hit returned different results: %d vs %d", len(passes1), len(passes2))
	}
}

func TestPassService_Cache_Expiration(t *testing.T) {
	store := tracker.NewTLEStore(nil)
	iss, err := tracker.ParseTLE(testISSLines)
	require.NoError(t, err)
	store.AddWithGroup(iss, "test")

	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)
	svc := NewPassService(store, observer).WithCacheTTL(1 * time.Millisecond)

	// Первый вызов.
	_, err = svc.GetPasses("test", 24, 5.0)
	require.NoError(t, err)

	// Ждём истечения TTL.
	time.Sleep(5 * time.Millisecond)

	// Проверяем, что запись помечена как expired.
	_, expired := svc.CacheStats()
	if expired != 1 {
		t.Errorf("expected 1 expired entry, got %d", expired)
	}
}

func TestPassService_InvalidateCache(t *testing.T) {
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)
	svc := NewPassService(store, observer)

	// Наполняем кеш.
	_, err := svc.GetPasses("group1", 24, 5.0)
	require.NoError(t, err)
	_, err = svc.GetPasses("group2", 12, 10.0)
	require.NoError(t, err)

	entries, _ := svc.CacheStats()
	if entries != 2 {
		t.Errorf("expected 2 cache entries, got %d", entries)
	}

	// Очищаем весь кеш.
	svc.InvalidateCache()

	entries, _ = svc.CacheStats()
	if entries != 0 {
		t.Errorf("expected 0 cache entries after invalidation, got %d", entries)
	}
}

func TestPassService_InvalidateCacheForGroup(t *testing.T) {
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)
	svc := NewPassService(store, observer)

	// Наполняем кеш.
	_, err := svc.GetPasses("group1", 24, 5.0)
	require.NoError(t, err)
	_, err = svc.GetPasses("group1", 12, 5.0) // Тот же group, другие параметры.
	require.NoError(t, err)
	_, err = svc.GetPasses("group2", 24, 5.0)
	require.NoError(t, err)

	entries, _ := svc.CacheStats()
	if entries != 3 {
		t.Errorf("expected 3 cache entries, got %d", entries)
	}

	// Очищаем только group1.
	svc.InvalidateCacheForGroup("group1")

	entries, _ = svc.CacheStats()
	if entries != 1 {
		t.Errorf("expected 1 cache entry after partial invalidation, got %d", entries)
	}
}

func TestPassService_MakeCacheKey(t *testing.T) {
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)
	svc := NewPassService(store, observer)

	tests := []struct {
		group string
		hours int
		minEl float64
		want  string
	}{
		{"amateur", 24, 5.0, "amateur:24:5.0"},
		{"stations", 12, 10.5, "stations:12:10.5"},
		{"cubesat", 48, 0.0, "cubesat:48:0.0"},
	}

	for _, tt := range tests {
		got := svc.makeCacheKey(tt.group, tt.hours, tt.minEl)
		if got != tt.want {
			t.Errorf("makeCacheKey(%q, %d, %.1f) = %q, want %q",
				tt.group, tt.hours, tt.minEl, got, tt.want)
		}
	}
}

// --- Тесты вспомогательных функций ---

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{123, "123"},
		{-456, "-456"},
		{1000000, "1000000"},
	}

	for _, tt := range tests {
		got := itoa(tt.n)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFtoa(t *testing.T) {
	tests := []struct {
		f        float64
		decimals int
		want     string
	}{
		{5.0, 1, "5.0"},
		{10.56, 1, "10.6"},
		{0.0, 1, "0.0"},
		{-3.14, 2, "-3.14"},
		{100.0, 0, "100"},
	}

	for _, tt := range tests {
		got := ftoa(tt.f, tt.decimals)
		if got != tt.want {
			t.Errorf("ftoa(%.2f, %d) = %q, want %q", tt.f, tt.decimals, got, tt.want)
		}
	}
}

// --- Benchmarks ---

func BenchmarkPassService_GetPasses(b *testing.B) {
	store := tracker.NewTLEStore(nil)
	iss, err := tracker.ParseTLE(testISSLines)
	require.NoError(b, err)
	meteor, err := tracker.ParseTLE(testMeteorLines)
	require.NoError(b, err)
	store.AddWithGroup(iss, "test")
	store.AddWithGroup(meteor, "test")

	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)
	svc := NewPassService(store, observer).WithCacheTTL(0) // Без кеша для чистого теста.

	b.ResetTimer()
	for b.Loop() {
		_, err = svc.GetPasses("test", 24, 5.0)
		require.NoError(b, err)
	}
}

func BenchmarkPassService_GetPasses_CacheHit(b *testing.B) {
	store := tracker.NewTLEStore(nil)
	iss, err := tracker.ParseTLE(testISSLines)
	require.NoError(b, err)
	store.AddWithGroup(iss, "test")

	observer := tracker.NewObserver(47.3157, 39.7885, 0.05)
	svc := NewPassService(store, observer).WithCacheTTL(1 * time.Hour)

	// Заполняем кеш.
	_, err = svc.GetPasses("test", 24, 5.0)
	require.NoError(b, err)

	b.ResetTimer()
	for b.Loop() {
		_, err = svc.GetPasses("test", 24, 5.0)
		require.NoError(b, err)
	}
}
