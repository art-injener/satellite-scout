package sdr

import (
	"context"
	"errors"
	"testing"
)

type stubReceiver struct {
	name    string
	stopped bool
}

func (s *stubReceiver) Name() string               { return s.name }
func (s *stubReceiver) Capabilities() Capabilities { return Capabilities{} }
func (s *stubReceiver) Tune(uint64) error          { return nil }
func (s *stubReceiver) StartStream(context.Context, StreamConfig) (<-chan IQFrame, error) {
	// Закрытый канал: stub не стримит, но не возвращает (nil, nil).
	ch := make(chan IQFrame)
	close(ch)
	return ch, nil
}
func (s *stubReceiver) Stop() error { s.stopped = true; return nil }

func TestReceiverHub_RegisterGet(t *testing.T) {
	hub := NewReceiverHub()
	hub.Register("path-1", func() (Receiver, error) {
		return &stubReceiver{name: "test-1"}, nil
	})

	r, err := hub.Get("path-1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if r.Name() != "test-1" {
		t.Fatalf("Name()=%q, want %q", r.Name(), "test-1")
	}
}

func TestReceiverHub_LazyOpen(t *testing.T) {
	calls := 0
	hub := NewReceiverHub()
	hub.Register("lazy", func() (Receiver, error) {
		calls++
		return &stubReceiver{name: "lazy"}, nil
	})

	if calls != 0 {
		t.Fatalf("factory called at Register time: %d", calls)
	}
	hub.Get("lazy")
	if calls != 1 {
		t.Fatalf("factory call count=%d after Get, want 1", calls)
	}
}

func TestReceiverHub_GetCached(t *testing.T) {
	hub := NewReceiverHub()
	hub.Register("cached", func() (Receiver, error) {
		return &stubReceiver{name: "cached"}, nil
	})

	r1, _ := hub.Get("cached")
	r2, _ := hub.Get("cached")
	if r1 != r2 {
		t.Fatal("second Get returned different instance")
	}
}

func TestReceiverHub_FactoryError_NotCached(t *testing.T) {
	attempt := 0
	hub := NewReceiverHub()
	hub.Register("flaky", func() (Receiver, error) {
		attempt++
		if attempt == 1 {
			return nil, errors.New("device busy")
		}
		return &stubReceiver{name: "flaky"}, nil
	})

	_, err := hub.Get("flaky")
	if err == nil {
		t.Fatal("first Get should fail")
	}

	r, err := hub.Get("flaky")
	if err != nil {
		t.Fatalf("second Get error: %v", err)
	}
	if r.Name() != "flaky" {
		t.Fatalf("Name()=%q, want %q", r.Name(), "flaky")
	}
}

func TestReceiverHub_CloseAll(t *testing.T) {
	s1 := &stubReceiver{name: "s1"}
	s2 := &stubReceiver{name: "s2"}
	hub := NewReceiverHub()
	hub.Register("p1", func() (Receiver, error) { return s1, nil })
	hub.Register("p2", func() (Receiver, error) { return s2, nil })

	hub.Get("p1")
	hub.Get("p2")

	if err := hub.CloseAll(); err != nil {
		t.Fatalf("CloseAll error: %v", err)
	}
	if !s1.stopped || !s2.stopped {
		t.Fatalf("not all receivers stopped: s1=%v s2=%v", s1.stopped, s2.stopped)
	}
}

func TestReceiverHub_UnknownPath(t *testing.T) {
	hub := NewReceiverHub()
	_, err := hub.Get("nonexistent")
	if err == nil {
		t.Fatal("Get unknown path should return error")
	}
}

func TestReceiverHub_List(t *testing.T) {
	hub := NewReceiverHub()
	hub.Register("a", func() (Receiver, error) { return &stubReceiver{}, nil })
	hub.Register("b", func() (Receiver, error) { return &stubReceiver{}, nil })

	ids := hub.List()
	if len(ids) != 2 {
		t.Fatalf("List() len=%d, want 2", len(ids))
	}
}
