package sdr

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// streamStub — приёмник, поток которого тест наполняет вручную через send и завершает через end.
type streamStub struct {
	mu       sync.Mutex
	starts   int
	stops    int
	cfg      StreamConfig
	out      chan IQFrame
	startErr error
}

func (s *streamStub) Name() string               { return "stream-stub" }
func (s *streamStub) Capabilities() Capabilities { return rtlLikeCaps() }
func (s *streamStub) Tune(uint64) error          { return nil }
func (s *streamStub) SetGain(float32) error      { return nil }
func (s *streamStub) Status() ReceiverStatus     { return ReceiverStatus{} }

func (s *streamStub) StartStream(_ context.Context, cfg StreamConfig) (<-chan IQFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startErr != nil {
		return nil, s.startErr
	}
	s.starts++
	s.cfg = cfg
	s.out = make(chan IQFrame, 4)
	return s.out, nil
}

func (s *streamStub) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stops++
	s.closeOut()
	return nil
}

func (s *streamStub) send(f IQFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out <- f
}

// end завершает поток со стороны устройства (конец файла, отключение USB).
func (s *streamStub) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeOut()
}

func (s *streamStub) closeOut() {
	if s.out != nil {
		close(s.out)
		s.out = nil
	}
}

// counts returns how many times the stream was started and stopped.
func (s *streamStub) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starts, s.stops
}

func hubWithStub(cfg PathConfig) (*ReceiverHub, *streamStub) {
	stub := &streamStub{}
	hub := NewReceiverHub()
	hub.Register("p", cfg, func() (Receiver, error) { return stub, nil })
	return hub, stub
}

func recvFrame(t *testing.T, ch <-chan IQFrame) IQFrame {
	t.Helper()
	select {
	case f, ok := <-ch:
		if !ok {
			t.Fatal("channel closed, want frame")
		}
		return f
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for frame")
	}
	return IQFrame{}
}

func waitClosed(t *testing.T, ch <-chan IQFrame) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("channel not closed")
		}
	}
}

func TestSession_FirstViewerStartsStream(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{SampleRateHz: 1_024_000})
	frames, unsubscribe, err := hub.Subscribe("p")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()

	if starts, _ := stub.counts(); starts != 1 {
		t.Fatalf("starts=%d, want 1", starts)
	}
	if stub.cfg.SampleRate != 1_024_000 {
		t.Errorf("stream SampleRate=%d, want path rate 1024000", stub.cfg.SampleRate)
	}
	if stub.cfg.BlockSize != DefaultBlockSize {
		t.Errorf("stream BlockSize=%d, want %d", stub.cfg.BlockSize, DefaultBlockSize)
	}

	stub.send(makeFrame(5))
	if id := frameID(recvFrame(t, frames)); id != 5 {
		t.Errorf("got id=%d, want 5", id)
	}
}

// prefilledReceiver отдаёт заранее готовые кадры и сразу завершает поток, как короткий файл.
type prefilledReceiver struct {
	stubReceiver

	frames []IQFrame
}

func (p *prefilledReceiver) Capabilities() Capabilities { return rtlLikeCaps() }
func (p *prefilledReceiver) StartStream(context.Context, StreamConfig) (<-chan IQFrame, error) {
	ch := make(chan IQFrame, len(p.frames))
	for _, f := range p.frames {
		ch <- f
	}
	close(ch)
	return ch, nil
}

func TestSession_FirstViewerGetsEarliestFrames(t *testing.T) {
	for range 50 {
		hub := NewReceiverHub()
		hub.Register("p", PathConfig{}, func() (Receiver, error) {
			return &prefilledReceiver{frames: []IQFrame{makeFrame(1), makeFrame(2), makeFrame(3)}}, nil
		})
		frames, unsubscribe, err := hub.Subscribe("p")
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		for want := 1; want <= 3; want++ {
			if id := frameID(recvFrame(t, frames)); id != want {
				t.Fatalf("got id=%d, want %d", id, want)
			}
		}
		unsubscribe()
	}
}

func TestSession_ViewersShareOneStream(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	a, unsubA, err := hub.Subscribe("p")
	if err != nil {
		t.Fatalf("Subscribe a: %v", err)
	}
	defer unsubA()
	b, unsubB, err := hub.Subscribe("p")
	if err != nil {
		t.Fatalf("Subscribe b: %v", err)
	}
	defer unsubB()

	if starts, _ := stub.counts(); starts != 1 {
		t.Fatalf("starts=%d, want 1 for two viewers", starts)
	}
	stub.send(makeFrame(9))
	for _, ch := range []<-chan IQFrame{a, b} {
		if id := frameID(recvFrame(t, ch)); id != 9 {
			t.Errorf("got id=%d, want 9", id)
		}
	}
}

func TestSession_LastViewerStopsStream(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	a, unsubA, _ := hub.Subscribe("p")
	_, unsubB, _ := hub.Subscribe("p")

	unsubA()
	waitClosed(t, a)
	if _, stops := stub.counts(); stops != 0 {
		t.Fatalf("stops=%d after first viewer left, want 0", stops)
	}

	unsubB()
	if _, stops := stub.counts(); stops != 1 {
		t.Fatalf("stops=%d after last viewer left, want 1", stops)
	}

	_, unsubC, err := hub.Subscribe("p")
	if err != nil {
		t.Fatalf("Subscribe after stop: %v", err)
	}
	defer unsubC()
	if starts, _ := stub.counts(); starts != 2 {
		t.Errorf("starts=%d, want new session", starts)
	}
}

func TestSession_UnsubscribeIsIdempotent(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	_, unsubA, _ := hub.Subscribe("p")
	b, unsubB, _ := hub.Subscribe("p")
	defer unsubB()

	unsubA()
	unsubA()
	if _, stops := stub.counts(); stops != 0 {
		t.Fatalf("repeated unsubscribe stopped the stream, stops=%d", stops)
	}
	stub.send(makeFrame(3))
	recvFrame(t, b)
}

func TestSession_StartErrorNotCached(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	stub.startErr = errors.New("device busy")
	if _, _, err := hub.Subscribe("p"); err == nil {
		t.Fatal("Subscribe must fail when stream does not start")
	}

	stub.startErr = nil
	_, unsubscribe, err := hub.Subscribe("p")
	if err != nil {
		t.Fatalf("retry Subscribe: %v", err)
	}
	unsubscribe()
}

func TestSession_UnsupportedSampleRate(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{SampleRateHz: 500_000})
	if _, _, err := hub.Subscribe("p"); !errors.Is(err, ErrSampleRateUnsupported) {
		t.Fatalf("err=%v, want ErrSampleRateUnsupported", err)
	}
	if starts, _ := stub.counts(); starts != 0 {
		t.Errorf("starts=%d, stream must not start", starts)
	}
}

func TestSession_UnknownPath(t *testing.T) {
	if _, _, err := NewReceiverHub().Subscribe("nope"); !errors.Is(err, ErrUnknownRadioPath) {
		t.Errorf("err=%v, want ErrUnknownRadioPath", err)
	}
}

func TestSession_DeviceStreamEndClosesViewers(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	frames, unsubscribe, _ := hub.Subscribe("p")

	stub.end()
	waitClosed(t, frames)
	unsubscribe()

	next, unsubNext, err := hub.Subscribe("p")
	if err != nil {
		t.Fatalf("Subscribe after stream end: %v", err)
	}
	defer unsubNext()
	if starts, _ := stub.counts(); starts != 2 {
		t.Fatalf("starts=%d, want new session after stream end", starts)
	}
	stub.send(makeFrame(11))
	recvFrame(t, next)
}

func TestSession_CloseAllClosesViewers(t *testing.T) {
	hub, _ := hubWithStub(PathConfig{})
	frames, unsubscribe, _ := hub.Subscribe("p")
	defer unsubscribe()

	if err := hub.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	waitClosed(t, frames)
}
