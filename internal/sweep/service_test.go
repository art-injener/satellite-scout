package sweep

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	testPath  = "radio_path_1"
	testRate  = 2_000_000
	testBand  = 1_600_000
	testDwell = 60 * time.Second
	waitLimit = time.Second
)

type fakeReceiver struct {
	mu    sync.Mutex
	tunes []uint64
}

func (r *fakeReceiver) Name() string                   { return "fake" }
func (r *fakeReceiver) Capabilities() sdr.Capabilities { return sdr.Capabilities{UsableFraction: 0.8} }
func (r *fakeReceiver) SetGain(float32) error          { return nil }
func (r *fakeReceiver) Status() sdr.ReceiverStatus     { return sdr.ReceiverStatus{} }
func (r *fakeReceiver) Stop() error                    { return nil }
func (r *fakeReceiver) StartStream(context.Context, sdr.StreamConfig) (<-chan sdr.IQFrame, error) {
	return nil, nil
}

func (r *fakeReceiver) Tune(hz uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tunes = append(r.tunes, hz)
	return nil
}

func (r *fakeReceiver) tuned() []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.tunes)
}

type fakeHub struct {
	rcv    *fakeReceiver
	frames chan sdr.IQFrame
	mu     sync.Mutex
	subs   int
	unsubs int
}

func newFakeHub() *fakeHub {
	return &fakeHub{rcv: &fakeReceiver{}, frames: make(chan sdr.IQFrame)}
}

func (h *fakeHub) Subscribe(string) (<-chan sdr.IQFrame, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs++
	return h.frames, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.unsubs++
	}, nil
}

func (h *fakeHub) Get(string) (sdr.Receiver, error)  { return h.rcv, nil }
func (h *fakeHub) UsableBand(string) (uint32, error) { return testBand, nil }

func (h *fakeHub) counts() (subs, unsubs int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subs, h.unsubs
}

// fakeClock: таймер стоянки и такт снимков срабатывают только по команде теста.
type fakeClock struct {
	now    time.Time
	afters chan chan time.Time
	tick   chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		now:    time.UnixMilli(1_700_000_000_000),
		afters: make(chan chan time.Time, 16),
		tick:   make(chan time.Time),
	}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.afters <- ch
	return ch
}

func (c *fakeClock) Ticker(time.Duration) (<-chan time.Time, func()) { return c.tick, func() {} }

func (c *fakeClock) fireDwell(t *testing.T) {
	t.Helper()
	select {
	case ch := <-c.afters:
		ch <- c.now
	case <-time.After(waitLimit):
		t.Fatal("dwell timer was not armed")
	}
}

func (c *fakeClock) tickOnce(t *testing.T) {
	t.Helper()
	select {
	case c.tick <- c.now:
	case <-time.After(waitLimit):
		t.Fatal("sweep loop did not take a snapshot tick")
	}
}

type harness struct {
	svc    *Service
	hub    *fakeHub
	clock  *fakeClock
	states chan State
}

func newHarness(t *testing.T, ranges ...Range) *harness {
	t.Helper()
	h := &harness{hub: newFakeHub(), clock: newFakeClock(), states: make(chan State, 64)}
	h.svc = NewService(h.hub, func(eventType string, data []byte) {
		if eventType != EventSweepState {
			return
		}
		var st State
		if err := json.Unmarshal(data, &st); err != nil {
			t.Errorf("bad sweep_state payload: %v", err)
			return
		}
		h.states <- st
	}).withClock(h.clock)
	if len(ranges) == 0 {
		ranges = []Range{{CenterHz: 145_900_000}, {CenterHz: 437_000_000}}
	}
	h.svc.Configure(testPath, ranges, testDwell)
	t.Cleanup(func() { h.svc.Stop(testPath) })
	return h
}

func (h *harness) nextState(t *testing.T) State {
	t.Helper()
	select {
	case st := <-h.states:
		return st
	case <-time.After(waitLimit):
		t.Fatal("no sweep_state event")
		return State{}
	}
}

func (h *harness) waitTunes(t *testing.T, n int) []uint64 {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for time.Now().Before(deadline) {
		if got := h.hub.rcv.tuned(); len(got) >= n {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("expected %d tunes, got %v", n, h.hub.rcv.tuned())
	return nil
}

// tone — блоки IQ с тоном offsetHz на частоте окна centerHz.
func tone(centerHz uint64, offsetHz float64, samples int) []sdr.IQFrame {
	const block = 16384
	var out []sdr.IQFrame
	var phase float64
	step := 2 * math.Pi * offsetHz / testRate
	for done := 0; done < samples; done += block {
		buf := make([]complex64, min(block, samples-done))
		for i := range buf {
			buf[i] = complex(float32(math.Cos(phase)), float32(math.Sin(phase)))
			phase += step
		}
		out = append(out, sdr.IQFrame{Samples: buf, CenterHz: centerHz, SampleRate: testRate})
	}
	return out
}

func (h *harness) push(t *testing.T, frames []sdr.IQFrame) {
	t.Helper()
	for _, f := range frames {
		select {
		case h.hub.frames <- f:
		case <-time.After(waitLimit):
			t.Fatal("sweep loop does not read frames")
		}
	}
}

func TestService_StartTunesFirstWindowAndHoldsStream(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(testPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := h.waitTunes(t, 1); got[0] != 145_900_000 {
		t.Errorf("first tune %d, want lowest window 145900000", got[0])
	}
	if subs, _ := h.hub.counts(); subs != 1 {
		t.Errorf("sweep must hold the path stream, subscriptions %d", subs)
	}
	st := h.nextState(t)
	if !st.Running || st.Index != 0 || st.Cycle != 1 || st.DwellS != 60 {
		t.Errorf("state after start %+v", st)
	}
	if st.WindowEndMs-st.WindowStartMs != testDwell.Milliseconds() {
		t.Errorf("window end must be one dwell after start, got %+v", st)
	}
}

func TestService_OnDwellEndReportsFinishedWindow(t *testing.T) {
	h := newHarness(t)
	var gotPath string
	var got Window
	var gotStart, gotEnd int64
	h.svc.OnDwellEnd(func(pathID string, w Window, startMs, endMs int64) {
		gotPath, got, gotStart, gotEnd = pathID, w, startMs, endMs
	})
	if err := h.svc.Start(testPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st := h.nextState(t)
	h.clock.fireDwell(t)
	h.nextState(t) // advance после стоянки
	if gotPath != testPath || got.CenterHz != 145_900_000 {
		t.Fatalf("dwell end path=%q window=%+v, want %s @ 145.9 MHz", gotPath, got, testPath)
	}
	if gotStart != st.WindowStartMs || gotEnd < gotStart {
		t.Errorf("times start=%d end=%d, want start %d", gotStart, gotEnd, st.WindowStartMs)
	}
}

func TestService_DwellAdvancesAndWraps(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(testPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.nextState(t)

	h.clock.fireDwell(t)
	if st := h.nextState(t); st.Index != 1 || st.Cycle != 1 {
		t.Errorf("after first dwell %+v, want index 1 cycle 1", st)
	}
	h.clock.fireDwell(t)
	if st := h.nextState(t); st.Index != 0 || st.Cycle != 2 {
		t.Errorf("after second dwell %+v, want index 0 cycle 2", st)
	}
	want := []uint64{145_900_000, 437_000_000, 145_900_000}
	if got := h.waitTunes(t, 3); !slices.Equal(got, want) {
		t.Errorf("tunes %v, want %v", got, want)
	}
}

func TestService_StopReleasesStreamAndKeepsFrequency(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(testPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.nextState(t)

	if !h.svc.Stop(testPath) {
		t.Error("Stop of a running sweep must report true")
	}
	if st := h.nextState(t); st.Running {
		t.Errorf("state after stop %+v", st)
	}
	if _, unsubs := h.hub.counts(); unsubs != 1 {
		t.Errorf("stop must release the stream, unsubscriptions %d", unsubs)
	}
	if got := h.hub.rcv.tuned(); len(got) != 1 {
		t.Errorf("stop must not retune, tunes %v", got)
	}
	if h.svc.Stop(testPath) {
		t.Error("second Stop must report false")
	}
	if st, _ := h.svc.State(testPath); st.Running {
		t.Error("State must show the sweep stopped")
	}
}

func TestService_StreamEndStopsSweep(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(testPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.nextState(t)

	close(h.hub.frames)
	if st := h.nextState(t); st.Running {
		t.Errorf("state after stream end %+v", st)
	}
}

func TestService_SnapshotOfCurrentWindow(t *testing.T) {
	h := newHarness(t)
	_, updates, cancel, err := h.svc.Watch(testPath)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer cancel()
	if err := h.svc.Start(testPath); err != nil {
		t.Fatalf("Start: %v", err)
	}

	const offset = 200_000.0
	h.push(t, tone(145_900_000, offset, 200_000))
	h.clock.tickOnce(t)

	var snap Snapshot
	select {
	case snap = <-updates:
	case <-time.After(waitLimit):
		t.Fatal("no snapshot after tick")
	}
	if snap.CenterHz != 145_900_000 {
		t.Errorf("snapshot center %d", snap.CenterHz)
	}
	if math.Abs(float64(snap.SpanHz)-testBand) > testBand*0.01 {
		t.Errorf("snapshot span %d, want usable band %d", snap.SpanHz, testBand)
	}
	if snap.TimeMs != h.clock.now.UnixMilli() {
		t.Errorf("snapshot time %d", snap.TimeMs)
	}
	peak := slices.Index(snap.DBFS, slices.Max(snap.DBFS))
	binHz := float64(snap.SpanHz) / float64(len(snap.DBFS))
	gotHz := (float64(peak) - float64(len(snap.DBFS))/2) * binHz
	if math.Abs(gotHz-offset) > 2*binHz {
		t.Errorf("tone at %+.0f Hz, want %+.0f", gotHz, offset)
	}
}

func TestService_SnapshotSkipsSettleAndOtherCenter(t *testing.T) {
	h := newHarness(t)
	_, updates, cancel, err := h.svc.Watch(testPath)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer cancel()
	if err := h.svc.Start(testPath); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Блоки прежней частоты и первые 50 мс после перестройки в снимок не идут.
	h.push(t, tone(437_000_000, 0, 200_000))
	h.push(t, tone(145_900_000, 0, testRate/20))
	h.clock.tickOnce(t)

	select {
	case snap := <-updates:
		t.Errorf("unexpected snapshot %d Hz from stale or settling samples", snap.CenterHz)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestService_WatchGivesStoredSnapshots(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Start(testPath); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.push(t, tone(145_900_000, 0, 200_000))
	h.clock.tickOnce(t)
	// Итог окна при уходе с него тоже сохраняется.
	h.clock.fireDwell(t)
	h.waitTunes(t, 2)
	h.svc.Stop(testPath)

	snaps, _, cancel, err := h.svc.Watch(testPath)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer cancel()
	if len(snaps) != 1 || snaps[0].CenterHz != 145_900_000 || len(snaps[0].DBFS) == 0 {
		t.Errorf("stored snapshots %+v, want one of window 145.9 MHz", snaps)
	}
}

func TestService_StartNotConfigured(t *testing.T) {
	svc := NewService(newFakeHub(), func(string, []byte) {})
	if err := svc.Start(testPath); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Start without ranges: %v, want ErrNotConfigured", err)
	}
	if _, err := svc.State(testPath); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("State without ranges: %v, want ErrNotConfigured", err)
	}
}

func TestService_StateBeforeStartListsWindows(t *testing.T) {
	h := newHarness(t, Range{LoHz: 435_000_000, HiHz: 438_000_000})
	st, err := h.svc.State(testPath)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	want := []Window{
		{CenterHz: 435_220_000, SpanHz: testBand},
		{CenterHz: 436_500_000, SpanHz: testBand},
		{CenterHz: 437_780_000, SpanHz: testBand},
	}
	if st.Running || !slices.Equal(st.Windows, want) || st.RadioPath != testPath {
		t.Errorf("state %+v, want stopped with windows %v", st, want)
	}
}
