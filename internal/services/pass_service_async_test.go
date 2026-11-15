package services

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// controlledCompute — подменный расчёт пролётов: считает вызовы и ждёт команды на завершение.
type controlledCompute struct {
	calls   atomic.Int32
	release chan struct{}
	result  atomic.Int32 // NoradID единственного пролёта в ответе
}

func newControlledCompute() *controlledCompute {
	c := &controlledCompute{release: make(chan struct{})}
	c.result.Store(25544)
	return c
}

func (c *controlledCompute) fn(_ int, _ float64) ([]*tracker.Pass, error) {
	c.calls.Add(1)
	<-c.release
	return []*tracker.Pass{{NoradID: int(c.result.Load())}}, nil
}

// newAsyncTestService — сервис с непустым каталогом (пустой не кешируется) и подменным расчётом.
func newAsyncTestService(t *testing.T, c *controlledCompute) *PassService {
	t.Helper()
	store := tracker.NewTLEStore(nil)
	iss, err := tracker.ParseTLE(testISSLines)
	if err != nil {
		t.Fatalf("ParseTLE: %v", err)
	}
	store.Add(iss)
	svc := NewPassService(store, tracker.NewObserver(47.3157, 39.7885, 0.05))
	svc.computeAll = c.fn
	return svc
}

func waitCalls(t *testing.T, c *controlledCompute, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for c.calls.Load() < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := c.calls.Load(); got != want {
		t.Fatalf("compute calls = %d, want %d", got, want)
	}
}

// fillCache делает первый расчёт и дожидается его.
func fillCache(t *testing.T, svc *PassService, c *controlledCompute) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := svc.GetAllGroupsPasses(1, 5); err != nil {
			t.Errorf("GetAllGroupsPasses: %v", err)
		}
	}()
	waitCalls(t, c, 1)
	c.release <- struct{}{}
	<-done
}

func TestPassService_AllGroups_ConcurrentMissComputesOnce(t *testing.T) {
	c := newControlledCompute()
	svc := newAsyncTestService(t, c)

	var wg sync.WaitGroup
	results := make([][]*tracker.Pass, 5)
	for i := range results {
		wg.Go(func() {
			passes, err := svc.GetAllGroupsPasses(1, 5)
			if err != nil {
				t.Errorf("GetAllGroupsPasses: %v", err)
			}
			results[i] = passes
		})
	}
	waitCalls(t, c, 1)
	time.Sleep(20 * time.Millisecond)
	close(c.release)
	wg.Wait()

	if n := c.calls.Load(); n != 1 {
		t.Fatalf("compute calls = %d, want 1 shared computation", n)
	}
	for i, r := range results {
		if len(r) != 1 || r[0].NoradID != 25544 {
			t.Fatalf("caller %d got %v, want shared result", i, r)
		}
	}
}

func TestPassService_AllGroups_ExpiredCacheServedWithoutWaiting(t *testing.T) {
	c := newControlledCompute()
	svc := newAsyncTestService(t, c).WithCacheTTL(time.Millisecond)
	fillCache(t, svc, c)
	time.Sleep(5 * time.Millisecond)
	c.result.Store(40069)

	started := time.Now()
	passes, err := svc.GetAllGroupsPasses(1, 5)
	if err != nil {
		t.Fatalf("GetAllGroupsPasses: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("expired cache blocked caller for %s", elapsed)
	}
	if len(passes) != 1 || passes[0].NoradID != 25544 {
		t.Fatalf("got %v, want previous passes while recomputing", passes)
	}
	waitCalls(t, c, 2)

	svc.cacheTTL = time.Hour
	c.release <- struct{}{}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		passes, _ = svc.GetAllGroupsPasses(1, 5)
		if len(passes) == 1 && passes[0].NoradID == 40069 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("background result never replaced cache, last %v", passes)
}

func TestPassService_AllGroups_MarkStaleRecomputesInBackground(t *testing.T) {
	c := newControlledCompute()
	svc := newAsyncTestService(t, c).WithCacheTTL(time.Hour)
	fillCache(t, svc, c)

	svc.MarkStale()
	passes, err := svc.GetAllGroupsPasses(1, 5)
	if err != nil || len(passes) != 1 || passes[0].NoradID != 25544 {
		t.Fatalf("got %v, %v; want previous passes right after MarkStale", passes, err)
	}
	waitCalls(t, c, 2)
	close(c.release)
}

func TestPassService_AllGroups_InvalidateDropsInFlightResult(t *testing.T) {
	c := newControlledCompute()
	svc := newAsyncTestService(t, c).WithCacheTTL(time.Hour)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.GetAllGroupsPasses(1, 5)
	}()
	waitCalls(t, c, 1)
	svc.InvalidateCache()
	c.release <- struct{}{}
	<-done

	if entries, _ := svc.CacheStats(); entries != 0 {
		t.Fatalf("cache entries = %d, want 0: result for old observer must not be stored", entries)
	}
	close(c.release)
}
