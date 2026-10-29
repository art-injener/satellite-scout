package sdr

import (
	"context"
	"errors"
	"slices"
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

func waitStops(t *testing.T, stub *streamStub, want int) {
	t.Helper()
	deadline := time.Now().Add(sessionIdleGrace + time.Second)
	for time.Now().Before(deadline) {
		if _, stops := stub.counts(); stops >= want {
			if stops != want {
				t.Fatalf("stops=%d, want %d", stops, want)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, stops := stub.counts()
	t.Fatalf("stops=%d, want %d", stops, want)
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
	if _, stops := stub.counts(); stops != 0 {
		t.Fatalf("stops=%d right after the last viewer left, want 0 during grace", stops)
	}
	waitStops(t, stub, 1)

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

func TestHub_SampleRateDefaultsAndChoices(t *testing.T) {
	hub, _ := hubWithStub(PathConfig{})
	if rate, err := hub.SampleRate("p"); err != nil || rate != 2_048_000 {
		t.Fatalf("SampleRate=%d, %v; want recommended 2048000", rate, err)
	}
	choices, err := hub.SampleRateChoices("p")
	if err != nil {
		t.Fatalf("SampleRateChoices: %v", err)
	}
	if want := []uint32{2_048_000, 1_024_000, 256_000}; !slices.Equal(choices, want) {
		t.Errorf("choices=%v, want %v", choices, want)
	}
}

func TestHub_SetSampleRateRejectsOutsideChoices(t *testing.T) {
	hub, _ := hubWithStub(PathConfig{})
	if err := hub.SetSampleRate("p", 512_000); !errors.Is(err, ErrSampleRateUnsupported) {
		t.Fatalf("err=%v, want ErrSampleRateUnsupported", err)
	}
	if rate, _ := hub.SampleRate("p"); rate != 2_048_000 {
		t.Errorf("SampleRate=%d after rejected set, want 2048000", rate)
	}
	if err := hub.SetSampleRate("nope", 256_000); !errors.Is(err, ErrUnknownRadioPath) {
		t.Errorf("err=%v, want ErrUnknownRadioPath", err)
	}
}

func TestHub_SetSampleRateBeforeStreamUsedByNextSession(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	if err := hub.SetSampleRate("p", 256_000); err != nil {
		t.Fatalf("SetSampleRate: %v", err)
	}
	if starts, _ := stub.counts(); starts != 0 {
		t.Fatalf("starts=%d, want no stream without viewers", starts)
	}
	_, unsubscribe, err := hub.Subscribe("p")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()
	if stub.cfg.SampleRate != 256_000 {
		t.Errorf("stream SampleRate=%d, want selected 256000", stub.cfg.SampleRate)
	}
}

// Смена частоты при идущем потоке перезапускает поток устройства, а зрители остаются
// на своих каналах: кадры новой частоты приходят туда же.
func TestHub_SetSampleRateRestartsStreamKeepingViewers(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	frames, unsubscribe, _ := hub.Subscribe("p")
	defer unsubscribe()

	if err := hub.SetSampleRate("p", 256_000); err != nil {
		t.Fatalf("SetSampleRate: %v", err)
	}
	if starts, stops := stub.counts(); starts != 2 || stops != 1 {
		t.Fatalf("starts=%d stops=%d, want the device stream restarted once", starts, stops)
	}
	if stub.cfg.SampleRate != 256_000 {
		t.Errorf("restarted stream SampleRate=%d, want 256000", stub.cfg.SampleRate)
	}
	stub.send(makeFrame(21))
	if id := frameID(recvFrame(t, frames)); id != 21 {
		t.Errorf("got id=%d, want 21 from the restarted stream", id)
	}

	// Конец потока по вине устройства по-прежнему закрывает зрителей
	stub.end()
	waitClosed(t, frames)
}

// tapRecorder collects the streams handed to the hub stream tap.
func tapRecorder(hub *ReceiverHub) <-chan (<-chan IQFrame) {
	taps := make(chan (<-chan IQFrame), 4)
	hub.SetStreamTap(func(pathID string, frames <-chan IQFrame) {
		if pathID == "p" {
			taps <- frames
		}
	})
	return taps
}

func recvTap(t *testing.T, taps <-chan (<-chan IQFrame)) <-chan IQFrame {
	t.Helper()
	select {
	case ch := <-taps:
		return ch
	case <-time.After(time.Second):
		t.Fatal("stream tap not called")
	}
	return nil
}

// A spectrum mode change drops the only viewer and subscribes again at once.
// The device stream and the demod tap stay up across that gap.
func TestSession_ResubscribeDuringGraceKeepsDeviceAndTap(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	taps := tapRecorder(hub)
	_, unsubscribe, _ := hub.Subscribe("p")
	tap := recvTap(t, taps)

	unsubscribe()
	frames, unsubscribe2, err := hub.Subscribe("p")
	if err != nil {
		t.Fatalf("Subscribe during grace: %v", err)
	}
	defer unsubscribe2()

	if starts, stops := stub.counts(); starts != 1 || stops != 0 {
		t.Fatalf("starts=%d stops=%d, want the same device stream", starts, stops)
	}
	select {
	case <-taps:
		t.Fatal("tap restarted for a viewer returning during grace")
	default:
	}

	stub.send(makeFrame(51))
	if id := frameID(recvFrame(t, frames)); id != 51 {
		t.Errorf("viewer got id=%d, want 51", id)
	}
	if id := frameID(recvFrame(t, tap)); id != 51 {
		t.Errorf("tap got id=%d, want 51 from the same stream", id)
	}

	time.Sleep(sessionIdleGrace + 200*time.Millisecond)
	if _, stops := stub.counts(); stops != 0 {
		t.Fatalf("stops=%d, grace must not stop a stream that has a viewer again", stops)
	}
}

// The tap rides along with viewers: it gets the same frames, does not keep the device
// stream running and is closed after the grace that follows the last viewer.
func TestHub_StreamTapRidesAlongWithViewers(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	taps := tapRecorder(hub)
	frames, unsubscribe, _ := hub.Subscribe("p")
	tap := recvTap(t, taps)

	_, unsubscribe2, _ := hub.Subscribe("p")
	unsubscribe2()
	select {
	case <-taps:
		t.Fatal("tap called again for a viewer joining a running stream")
	default:
	}

	stub.send(makeFrame(31))
	if id := frameID(recvFrame(t, frames)); id != 31 {
		t.Errorf("viewer got id=%d, want 31", id)
	}
	if id := frameID(recvFrame(t, tap)); id != 31 {
		t.Errorf("tap got id=%d, want 31", id)
	}

	unsubscribe()
	if _, stops := stub.counts(); stops != 0 {
		t.Errorf("stops=%d right after the last viewer left, want 0 during grace", stops)
	}
	waitStops(t, stub, 1)
	waitClosed(t, tap)

	// The next session gets a fresh tap.
	_, unsubscribe3, _ := hub.Subscribe("p")
	defer unsubscribe3()
	recvTap(t, taps)
}

func TestHub_StreamTapKeptAcrossSampleRateChange(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	taps := tapRecorder(hub)
	_, unsubscribe, _ := hub.Subscribe("p")
	defer unsubscribe()
	tap := recvTap(t, taps)

	if err := hub.SetSampleRate("p", 256_000); err != nil {
		t.Fatalf("SetSampleRate: %v", err)
	}
	stub.send(makeFrame(41))
	if id := frameID(recvFrame(t, tap)); id != 41 {
		t.Errorf("tap got id=%d, want 41 from the restarted stream", id)
	}
}

func TestHub_SetSampleRateSameRateKeepsStream(t *testing.T) {
	hub, stub := hubWithStub(PathConfig{})
	_, unsubscribe, _ := hub.Subscribe("p")
	defer unsubscribe()
	if err := hub.SetSampleRate("p", 2_048_000); err != nil {
		t.Fatalf("SetSampleRate: %v", err)
	}
	if starts, stops := stub.counts(); starts != 1 || stops != 0 {
		t.Errorf("starts=%d stops=%d, want the stream untouched", starts, stops)
	}
}

func TestHub_SetChannelValidatedBySelectedRate(t *testing.T) {
	hub, _ := hubWithStub(PathConfig{})
	if err := hub.SetSampleRate("p", 256_000); err != nil {
		t.Fatalf("SetSampleRate: %v", err)
	}
	c := DefaultChannelSettings()
	c.OffsetHz = 200_000 // внутри 2,048 МГц, за краем 256 кГц
	if err := hub.SetChannel("p", c); !errors.Is(err, ErrInvalidChannel) {
		t.Errorf("err=%v, want ErrInvalidChannel at 256 kHz", err)
	}
}
