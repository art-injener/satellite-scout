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
	calls    atomic.Int32
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
	m.calls.Add(1)
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
	return m.calls.Load()
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

func TestService_GetPrimary_DoesNotEnqueue(t *testing.T) {
	mock := newMockFetcher()
	mock.results[25544] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_825_000), Mode: "FM"},
	}
	svc := NewService(mock)
	stop := runService(svc)
	defer stop()

	if got := svc.GetPrimaryTransmitter(25544); got != nil {
		t.Errorf("first call = %v, want nil (cache miss, no auto-fetch)", got)
	}
	time.Sleep(50 * time.Millisecond)
	if n := mock.callCount(); n != 0 {
		t.Fatalf("network calls = %d, want 0 without RequestFetch", n)
	}

	svc.RequestFetch([]int{25544})
	waitFor(t, 500*time.Millisecond, func() bool {
		return svc.GetPrimaryTransmitter(25544) != nil
	})
	got := svc.GetPrimaryTransmitter(25544)
	if got == nil {
		t.Fatal("after RequestFetch, GetPrimaryTransmitter() = nil")
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

	svc.RequestFetch([]int{1})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	for range 5 {
		_ = svc.GetPrimaryTransmitter(1)
	}
	if got := mock.callCount(); got != 1 {
		t.Errorf("callCount = %d, want 1 (cache hit, no extra fetch)", got)
	}
}

func TestService_GetPrimary_TTLExpiryNeedsRequestFetch(t *testing.T) {
	mock := newMockFetcher()
	mock.results[1] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
	}
	svc := NewService(mock).WithCacheTTL(50 * time.Millisecond)
	stop := runService(svc)
	defer stop()

	svc.RequestFetch([]int{1})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	time.Sleep(80 * time.Millisecond)
	_ = svc.GetPrimaryTransmitter(1)
	time.Sleep(30 * time.Millisecond)
	if got := mock.callCount(); got != 1 {
		t.Fatalf("Get after TTL triggered fetch: callCount=%d, want 1", got)
	}

	svc.RequestFetch([]int{1})
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

	svc.RequestFetch([]int{1})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })
	time.Sleep(40 * time.Millisecond)

	mock.mu.Lock()
	mock.delay = 200 * time.Millisecond
	mock.mu.Unlock()

	svc.RequestFetch([]int{1})
	got := svc.GetPrimaryTransmitter(1)
	if got == nil {
		t.Fatal("expected stale primary while refetching, got nil")
	}
	if got.FreqMHz != "145.800" {
		t.Errorf("stale FreqMHz = %q, want 145.800", got.FreqMHz)
	}
}

func TestService_RequestFetch_GracefulDegradationOnError(t *testing.T) {
	mock := newMockFetcher()
	mock.errors[42] = errors.New("network down")
	svc := NewService(mock).WithRetryAfter(100 * time.Millisecond)
	stop := runService(svc)
	defer stop()

	svc.RequestFetch([]int{42})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	svc.RequestFetch([]int{42})
	time.Sleep(20 * time.Millisecond)
	if got := mock.callCount(); got != 1 {
		t.Errorf("callCount = %d, want 1 (no retry within retryAfter)", got)
	}

	time.Sleep(120 * time.Millisecond)
	svc.RequestFetch([]int{42})
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

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			svc.RequestFetch([]int{1})
		})
	}
	wg.Wait()

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
	mock.delay = 80 * time.Millisecond

	svc := NewService(mock).WithWorkers(4)
	stop := runService(svc)
	defer stop()

	start := time.Now()
	svc.RequestFetch([]int{1, 2, 3, 4})
	waitFor(t, 1*time.Second, func() bool { return mock.callCount() == 4 })
	elapsed := time.Since(start)

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
	svc.RequestFetch([]int{1})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	all := svc.GetAllTransmitters(1)
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}
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

	svc.RequestFetch([]int{42})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })

	time.Sleep(baseRetry + 20*time.Millisecond)
	svc.RequestFetch([]int{42})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 2 })

	time.Sleep(baseRetry + 10*time.Millisecond)
	svc.RequestFetch([]int{42})
	time.Sleep(20 * time.Millisecond)
	if got := mock.callCount(); got != 2 {
		t.Errorf("callCount = %d, want 2 (should wait for progressive delay)", got)
	}

	time.Sleep(baseRetry + 20*time.Millisecond)
	svc.RequestFetch([]int{42})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 3 })
}

func TestService_FailCountResetsOnSuccess(t *testing.T) {
	mock := newMockFetcher()
	mock.errors[42] = errors.New("api unreachable")

	baseRetry := 30 * time.Millisecond
	svc := NewService(mock).WithRetryAfter(baseRetry)
	stop := runService(svc)
	defer stop()

	svc.RequestFetch([]int{42})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 1 })
	time.Sleep(baseRetry + 10*time.Millisecond)
	svc.RequestFetch([]int{42})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 2 })

	mock.mu.Lock()
	delete(mock.errors, 42)
	mock.results[42] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
	}
	mock.mu.Unlock()

	time.Sleep(2*baseRetry + 20*time.Millisecond)
	svc.RequestFetch([]int{42})
	waitFor(t, 500*time.Millisecond, func() bool { return mock.callCount() == 3 })

	waitFor(t, 500*time.Millisecond, func() bool {
		return svc.GetPrimaryTransmitter(42) != nil
	})
}

func TestService_CircuitOpensAfterTimeouts(t *testing.T) {
	mock := newMockFetcher()
	for _, id := range []int{1, 2, 3} {
		mock.errors[id] = context.DeadlineExceeded
	}
	svc := NewService(mock).
		WithRetryAfter(time.Hour).
		WithCircuitBreaker(3, 200*time.Millisecond)
	stop := runService(svc)
	defer stop()

	svc.RequestFetch([]int{1, 2, 3})
	waitFor(t, time.Second, func() bool { return mock.callCount() == 3 })
	waitFor(t, time.Second, func() bool { return svc.CircuitOpen() })

	svc.RequestFetch([]int{4})
	time.Sleep(50 * time.Millisecond)
	if got := mock.callCount(); got != 3 {
		t.Fatalf("callCount = %d, want 3 while circuit open", got)
	}

	waitFor(t, time.Second, func() bool { return !svc.CircuitOpen() })
	mock.mu.Lock()
	mock.errors[4] = nil
	mock.results[4] = []Transmitter{
		{Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
	}
	mock.mu.Unlock()
	svc.RequestFetch([]int{4})
	waitFor(t, time.Second, func() bool { return mock.callCount() == 4 })
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
