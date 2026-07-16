package sdr

import (
	"sync"
	"testing"
	"time"
)

func makeFrame(id int) IQFrame {
	return IQFrame{
		Samples:     []complex64{complex(float32(id), 0)},
		CenterHz:    uint64(id),
		SampleRate:  48000,
		TimestampMs: uint64(time.Now().UnixMilli()),
	}
}

func frameID(f IQFrame) int {
	return int(f.CenterHz)
}

func TestDataRing_WriteRead(t *testing.T) {
	ring := NewDataRing(8)
	defer ring.Close()
	ch := ring.Subscribe()

	for i := 1; i <= 3; i++ {
		ring.Write(makeFrame(i))
	}

	for i := 1; i <= 3; i++ {
		select {
		case f := <-ch:
			if got := frameID(f); got != i {
				t.Fatalf("frame %d: got id=%d", i, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for frame %d", i)
		}
	}
}

func TestDataRing_DropOldest(t *testing.T) {
	const ringCap = 4
	ring := NewDataRing(ringCap)
	defer ring.Close()
	ch := ring.Subscribe()

	for i := 1; i <= ringCap+2; i++ {
		ring.Write(makeFrame(i))
	}

	for i := 3; i <= ringCap+2; i++ {
		select {
		case f := <-ch:
			if got := frameID(f); got != i {
				t.Fatalf("expected id=%d, got %d", i, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for frame id=%d", i)
		}
	}
}

func TestDataRing_MultiReader(t *testing.T) {
	ring := NewDataRing(8)
	defer ring.Close()

	ch1 := ring.Subscribe()
	ch2 := ring.Subscribe()

	ring.Write(makeFrame(42))

	for _, ch := range []<-chan IQFrame{ch1, ch2} {
		select {
		case f := <-ch:
			if frameID(f) != 42 {
				t.Fatalf("got id=%d, want 42", frameID(f))
			}
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	}
}

func TestDataRing_SlowReader(t *testing.T) {
	const ringCap = 4
	ring := NewDataRing(ringCap)
	defer ring.Close()

	_ = ring.Subscribe()
	fast := ring.Subscribe()

	done := make(chan struct{})
	go func() {
		for i := range ringCap * 3 {
			ring.Write(makeFrame(i))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writer blocked by slow reader")
	}

	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("fast reader got nothing")
	}
}

func TestDataRing_Close(t *testing.T) {
	ring := NewDataRing(4)
	ch := ring.Subscribe()
	ring.Write(makeFrame(1))
	ring.Close()

	count := 0
	for range ch {
		count++
		if count > 10 {
			t.Fatal("channel not closing")
		}
	}
}

func TestDataRing_ConcurrentWriteRead(t *testing.T) {
	ring := NewDataRing(16)
	defer ring.Close()
	ch := ring.Subscribe()

	const n = 100
	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range n {
			ring.Write(makeFrame(i))
		}
	})

	wg.Go(func() {
		count := 0
		for count < n {
			select {
			case <-ch:
				count++
			case <-time.After(2 * time.Second):
				return
			}
		}
	})

	wg.Wait()
}
