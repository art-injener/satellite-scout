package satnogs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockFetcher — мок HTTP-клиента для тестов сервиса.
type mockFetcher struct {
	mu       sync.Mutex
	calls    int32
	calledBy []int                 // NORAD'ы в порядке вызовов.
	results  map[int][]Transmitter // что возвращать на конкретный NORAD.
	errors   map[int]error         // ошибка вместо удачного ответа.
	delay    time.Duration         // задержка перед ответом (для тестов конкурентности).
	hook     func(noradID int)     // вызывается перед возвратом (для синхронизации).
}

func newMockFetcher() *mockFetcher {
	return &mockFetcher{
		results: make(map[int][]Transmitter),
		errors:  make(map[int]error),
	}
}

func (m *mockFetcher) FetchTransmitters(_ context.Context, noradID int) ([]Transmitter, error) {
	atomic.AddInt32(&m.calls, 1)
	m.mu.Lock()
	m.calledBy = append(m.calledBy, noradID)
	delay := m.delay
	hook := m.hook
	res, hasRes := m.results[noradID]
	err, hasErr := m.errors[noradID]
	m.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if hook != nil {
		hook(noradID)
	}
	if hasErr {
		return nil, err
	}
	if hasRes {
		return res, nil
	}
	return []Transmitter{}, nil
}

func (m *mockFetcher) callCount() int32 {
	return atomic.LoadInt32(&m.calls)
}

// runService — стартует Service.Run в горутине и возвращает stop-функцию.
func runService(svc *Service) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

// waitFor — повторяет cond с малыми интервалами до timeout.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func TestService_GetPrimary_CacheMissEnqueuesAndReturnsNil(t *testing.T) {
	mock := newMockFetcher()
	mock.results[25544] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_825_000), Mode: "FM"},
	}
	svc := NewService(mock)
	stop := runService(svc)
	defer stop()

	if got := svc.GetPrimaryTransmitter(25544); got != nil {
		t.Errorf("first call = %v, want nil (cache miss, async fetch enqueued)", got)
	}
	waitFor(t, 500*time.Millisecond, func() bool {
		return mock.callCount() == 1
	})
	waitFor(t, 500*time.Millisecond, func() bool {
		return svc.GetPrimaryTransmitter(25544) != nil
	})

	got := svc.GetPrimaryTransmitter(25544)
	if got == nil {
		t.Fatal("after fetch, GetPrimaryTransmitter() = nil")
	}
	if got.FreqMHz != "145.825" {
		t.Errorf("FreqMHz = %q, want 145.825", got.FreqMHz)
	}
}

func TestService_GetPrimary_CacheHitNoExtraFetch(t *testing.T) {
	mock := newMockFetcher()
	mock.results[1] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(437_500_000), Mode: "FM"},
	}
	svc := NewService(mock)
	stop := runService(svc)
	defer stop()

	svc.GetPrimaryTransmitter(1)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	// Несколько hot-вызовов — fetch не должен повторяться.
	for i := 0; i < 5; i++ {
		_ = svc.GetPrimaryTransmitter(1)
	}
	if got := mock.callCount(); got != 1 {
		t.Errorf("callCount = %d, want 1 (cache hit, no extra fetch)", got)
	}
}

func TestService_GetPrimary_TTLExpiryRefetches(t *testing.T) {
	mock := newMockFetcher()
	mock.results[1] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
	}
	svc := NewService(mock).WithCacheTTL(50 * time.Millisecond)
	stop := runService(svc)
	defer stop()

	svc.GetPrimaryTransmitter(1)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	// Ждём истечения TTL.
	time.Sleep(80 * time.Millisecond)

	// Запрос после TTL — должен поставить новый fetch в очередь.
	_ = svc.GetPrimaryTransmitter(1)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 2 })
}

func TestService_GetPrimary_StaleEntryReturnedWhileRefetching(t *testing.T) {
	mock := newMockFetcher()
	mock.results[1] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
	}
	svc := NewService(mock).WithCacheTTL(20 * time.Millisecond)
	stop := runService(svc)
	defer stop()

	svc.GetPrimaryTransmitter(1)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })
	time.Sleep(40 * time.Millisecond) // TTL истёк.

	// Ставим задержку на следующий fetch — пока он идёт, должны вернуть старое значение.
	mock.mu.Lock()
	mock.delay = 200 * time.Millisecond
	mock.mu.Unlock()

	got := svc.GetPrimaryTransmitter(1)
	if got == nil {
		t.Fatal("expected stale primary while refetching, got nil")
	}
	if got.FreqMHz != "145.800" {
		t.Errorf("stale FreqMHz = %q, want 145.800", got.FreqMHz)
	}
}

func TestService_GetPrimary_GracefulDegradationOnError(t *testing.T) {
	mock := newMockFetcher()
	mock.errors[42] = errors.New("network down")
	svc := NewService(mock).WithRetryAfter(100 * time.Millisecond)
	stop := runService(svc)
	defer stop()

	if got := svc.GetPrimaryTransmitter(42); got != nil {
		t.Errorf("first call = %v, want nil", got)
	}
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	// Сразу после ошибки — повторно fetch не вызывается (ждём retryAfter).
	for i := 0; i < 3; i++ {
		_ = svc.GetPrimaryTransmitter(42)
	}
	time.Sleep(20 * time.Millisecond)
	if got := mock.callCount(); got != 1 {
		t.Errorf("callCount = %d, want 1 (no retry within retryAfter)", got)
	}

	// После retryAfter — следующая попытка разрешена.
	time.Sleep(120 * time.Millisecond)
	_ = svc.GetPrimaryTransmitter(42)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 2 })
}

func TestService_RequestFetch_BatchEnqueue(t *testing.T) {
	mock := newMockFetcher()
	for _, id := range []int{1, 2, 3, 4} {
		mock.results[id] = []Transmitter{
			{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
		}
	}
	svc := NewService(mock)
	stop := runService(svc)
	defer stop()

	svc.RequestFetch([]int{1, 2, 3, 4})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 4 })
}

func TestService_RequestFetch_DedupsConcurrentRequests(t *testing.T) {
	mock := newMockFetcher()
	mock.results[1] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
	}
	mock.delay = 50 * time.Millisecond
	svc := NewService(mock)
	stop := runService(svc)
	defer stop()

	// Десять параллельных запросов на тот же NORAD.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.GetPrimaryTransmitter(1)
		}()
	}
	wg.Wait()

	// Первая постановка fetch-а блокирует остальные через флаг fetching.
	waitFor(t, 1*time.Second, func() bool { return mock.callCount() >= 1 })
	if got := mock.callCount(); got != 1 {
		t.Errorf("callCount = %d, want 1 (dedup via fetching flag)", got)
	}
}

func TestService_WorkersProcessConcurrently(t *testing.T) {
	mock := newMockFetcher()
	for _, id := range []int{1, 2, 3, 4} {
		mock.results[id] = []Transmitter{
			{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
		}
	}
	// Каждый запрос «висит» 80мс. При одном воркере 4 запроса заняли бы ~320мс,
	// при пуле из 4 воркеров они выполняются параллельно (~80мс).
	mock.delay = 80 * time.Millisecond

	svc := NewService(mock).WithWorkers(4)
	stop := runService(svc)
	defer stop()

	start := time.Now()
	svc.RequestFetch([]int{1, 2, 3, 4})
	waitFor(t, 1*time.Second, func() bool { return mock.callCount() == 4 })
	elapsed := time.Since(start)

	// Порог с запасом: явно меньше последовательного исполнения (320мс).
	if elapsed > 250*time.Millisecond {
		t.Errorf("elapsed = %v, want < 250ms (workers must run in parallel)", elapsed)
	}
}

func TestService_GetAllTransmitters(t *testing.T) {
	mock := newMockFetcher()
	mock.results[1] = []Transmitter{
		{UUID: "a", Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
		{UUID: "b", Alive: true, Status: "active", DownlinkLow: ptrInt64(437_500_000), Mode: "BPSK"},
	}
	svc := NewService(mock)
	stop := runService(svc)
	defer stop()

	if got := svc.GetAllTransmitters(1); got != nil {
		t.Errorf("first call = %v, want nil (miss)", got)
	}
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	all := svc.GetAllTransmitters(1)
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}
	// Возвращаемое значение должно быть копией (мутация не влияет на кеш).
	all[0].UUID = "MUTATED"
	all2 := svc.GetAllTransmitters(1)
	if all2[0].UUID == "MUTATED" {
		t.Error("GetAllTransmitters() returned aliased slice (mutation leaked into cache)")
	}
}

func TestService_GetPrimary_InvalidNoradID(t *testing.T) {
	mock := newMockFetcher()
	svc := NewService(mock)
	if got := svc.GetPrimaryTransmitter(0); got != nil {
		t.Errorf("GetPrimaryTransmitter(0) = %v, want nil", got)
	}
	if got := svc.GetPrimaryTransmitter(-1); got != nil {
		t.Errorf("GetPrimaryTransmitter(-1) = %v, want nil", got)
	}
}

func TestService_RunStopsOnContextCancel(t *testing.T) {
	mock := newMockFetcher()
	svc := NewService(mock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// OK
	case <-time.After(1 * time.Second):
		t.Error("Run() did not return after ctx cancel")
	}
}

func TestService_ProgressiveBackoffOnRepeatedFailures(t *testing.T) {
	mock := newMockFetcher()
	mock.errors[42] = errors.New("api unreachable")

	baseRetry := 50 * time.Millisecond
	svc := NewService(mock).WithRetryAfter(baseRetry)
	stop := runService(svc)
	defer stop()

	// Первый fetch — failCount=1, retry delay = baseRetry (50ms).
	_ = svc.GetPrimaryTransmitter(42)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	// Ждём чуть больше baseRetry (50ms) — вторая попытка разрешена (failCount=1).
	time.Sleep(baseRetry + 20*time.Millisecond)
	_ = svc.GetPrimaryTransmitter(42)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 2 })

	// Теперь failCount=2 → delay = 2 * baseRetry = 100ms.
	// Через 50ms — retry ещё не разрешён.
	time.Sleep(baseRetry + 10*time.Millisecond)
	_ = svc.GetPrimaryTransmitter(42)
	time.Sleep(20 * time.Millisecond)
	if got := mock.callCount(); got != 2 {
		t.Errorf("callCount = %d, want 2 (should wait for progressive delay)", got)
	}

	// Дожидаемся оставшейся части (100ms - уже прошло ~60ms, ждём ещё ~60ms).
	time.Sleep(baseRetry + 20*time.Millisecond)
	_ = svc.GetPrimaryTransmitter(42)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 3 })
}

func TestService_FailCountResetsOnSuccess(t *testing.T) {
	mock := newMockFetcher()
	mock.errors[42] = errors.New("api unreachable")

	baseRetry := 30 * time.Millisecond
	svc := NewService(mock).WithRetryAfter(baseRetry)
	stop := runService(svc)
	defer stop()

	// Два неудачных fetch — failCount = 2.
	_ = svc.GetPrimaryTransmitter(42)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })
	time.Sleep(baseRetry + 10*time.Millisecond)
	_ = svc.GetPrimaryTransmitter(42)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 2 })

	// Теперь «чиним» API — следующий fetch успешен.
	mock.mu.Lock()
	delete(mock.errors, 42)
	mock.results[42] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
	}
	mock.mu.Unlock()

	// Ждём прогрессивный delay (2*baseRetry = 60ms).
	time.Sleep(2*baseRetry + 20*time.Millisecond)
	_ = svc.GetPrimaryTransmitter(42)
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 3 })

	// Проверяем что failCount сбросился — primary доступен.
	waitFor(t, 500*time.Millisecond, func() bool {
		return svc.GetPrimaryTransmitter(42) != nil
	})
}

func TestRetryDelay(t *testing.T) {
	base := 5 * time.Minute

	tests := []struct {
		failCount int
		want      time.Duration
	}{
		{0, base},
		{1, base},
		{2, 10 * time.Minute},
		{3, 20 * time.Minute},
		{6, maxRetryDelay},
		{100, maxRetryDelay},
	}
	for _, tt := range tests {
		got := retryDelay(base, tt.failCount)
		if got != tt.want {
			t.Errorf("retryDelay(%v, %d) = %v, want %v", base, tt.failCount, got, tt.want)
		}
	}
}

func TestService_CacheSize(t *testing.T) {
	mock := newMockFetcher()
	for _, id := range []int{1, 2, 3} {
		mock.results[id] = []Transmitter{
			{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_000_000), Mode: "FM"},
		}
	}
	svc := NewService(mock)
	stop := runService(svc)
	defer stop()

	svc.RequestFetch([]int{1, 2, 3})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 3 })

	if got := svc.CacheSize(); got != 3 {
		t.Errorf("CacheSize = %d, want 3", got)
	}
}
