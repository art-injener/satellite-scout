//go:build linux

package usbdrivers

import (
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"
)

type completion struct {
	idx    int
	status int32
	actual int
	err    error
}

// fakeKernel stands in for usbfs: submitted URBs wait until the test completes them,
// a discarded URB comes back with -ENOENT, like the kernel does.
type fakeKernel struct {
	mu        sync.Mutex
	bufs      [][]byte
	inflight  map[int]bool
	submits   []int
	discards  []int
	done      chan completion
	submitErr error
	gone      bool
}

func newFakeKernel(n, size int) (*fakeKernel, [][]byte) {
	bufs := make([][]byte, n)
	for i := range bufs {
		bufs[i] = make([]byte, size)
	}
	return &fakeKernel{bufs: bufs, inflight: map[int]bool{}, done: make(chan completion, 4*n)}, bufs
}

func (k *fakeKernel) submit(i int) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.submitErr != nil {
		return k.submitErr
	}
	if k.inflight[i] {
		panic("URB submitted twice")
	}
	k.inflight[i] = true
	k.submits = append(k.submits, i)
	return nil
}

func (k *fakeKernel) discard(i int) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.discards = append(k.discards, i)
	if !k.inflight[i] {
		return syscall.EINVAL
	}
	k.done <- completion{idx: i, status: -int32(syscall.ENOENT)}
	return nil
}

func (k *fakeKernel) release() {
	k.mu.Lock()
	defer k.mu.Unlock()
	// The kernel writes into user memory only inside REAPURB, so after the device is gone
	// the URBs it still lists are harmless.
	if len(k.inflight) != 0 && !k.gone {
		panic("memory released while the kernel still holds URBs")
	}
}

func (k *fakeKernel) reap() (int, int32, int, error) {
	c := <-k.done
	if c.err != nil {
		k.mu.Lock()
		k.gone = true
		k.mu.Unlock()
		return 0, 0, 0, c.err
	}
	k.mu.Lock()
	delete(k.inflight, c.idx)
	k.mu.Unlock()
	return c.idx, c.status, c.actual, nil
}

// complete fills URB i with its own number, as if the device sent n bytes.
func (k *fakeKernel) complete(i, n int) {
	for j := 0; j < n; j++ {
		k.bufs[i][j] = byte(i)
	}
	k.done <- completion{idx: i, actual: n}
}

func (k *fakeKernel) inflightCount() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.inflight)
}

func TestStream_DeliversInCompletionOrderAndResubmits(t *testing.T) {
	k, bufs := newFakeKernel(3, 16)
	s, err := startStream(k, bufs)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if k.inflightCount() != 3 {
		t.Fatalf("in flight=%d after start, want all 3", k.inflightCount())
	}

	k.complete(2, 10)
	k.complete(0, 16)
	got, err := s.Read()
	if err != nil || len(got) != 10 || got[0] != 2 {
		t.Fatalf("first Read=%v len %d, %v; want URB 2 with 10 bytes", got[:1], len(got), err)
	}
	got, err = s.Read()
	if err != nil || len(got) != 16 || got[0] != 0 {
		t.Fatalf("second Read=%v len %d, %v; want URB 0 with 16 bytes", got[:1], len(got), err)
	}
	// The buffer given by the first Read went back to the kernel on the second one.
	if want := []int{0, 1, 2, 2}; !equalInts(k.submits, want) {
		t.Errorf("submits=%v, want %v", k.submits, want)
	}
	if k.inflightCount() != 2 {
		t.Errorf("in flight=%d, want 2: the last buffer is still with the reader", k.inflightCount())
	}
}

func TestStream_CloseDiscardsAndReapsEverything(t *testing.T) {
	k, bufs := newFakeKernel(4, 8)
	s, err := startStream(k, bufs)
	if err != nil {
		t.Fatal(err)
	}
	k.complete(1, 8)
	if _, err := s.Read(); err != nil {
		t.Fatal(err)
	}

	done := make(chan error)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
	if k.inflightCount() != 0 {
		t.Errorf("in flight=%d after Close, want 0: memory is freed only when the kernel is done", k.inflightCount())
	}
	if _, err := s.Read(); !errors.Is(err, ErrClosed) {
		t.Errorf("Read after Close=%v, want ErrClosed", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close=%v, want nil", err)
	}
}

func TestStream_CloseUnblocksRead(t *testing.T) {
	k, bufs := newFakeKernel(2, 8)
	s, err := startStream(k, bufs)
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan error)
	go func() {
		_, err := s.Read()
		read <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-read:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("blocked Read=%v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read stayed blocked after Close")
	}
}

func TestStream_URBErrorIsReportedAndStreamGoesOn(t *testing.T) {
	k, bufs := newFakeKernel(2, 8)
	s, err := startStream(k, bufs)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	k.done <- completion{idx: 0, status: -int32(syscall.EOVERFLOW)}
	if _, err := s.Read(); !errors.Is(err, syscall.EOVERFLOW) {
		t.Fatalf("Read=%v, want EOVERFLOW", err)
	}
	k.complete(1, 4)
	if got, err := s.Read(); err != nil || len(got) != 4 {
		t.Fatalf("Read after an URB error=%d bytes, %v; want 4 bytes", len(got), err)
	}
	if k.inflightCount() != 1 {
		t.Errorf("in flight=%d, want 1: the failed URB was resubmitted", k.inflightCount())
	}
}

func TestStream_DeviceGoneEndsTheStream(t *testing.T) {
	k, bufs := newFakeKernel(2, 8)
	s, err := startStream(k, bufs)
	if err != nil {
		t.Fatal(err)
	}
	k.done <- completion{err: syscall.ENODEV}
	if _, err := s.Read(); !errors.Is(err, syscall.ENODEV) {
		t.Fatalf("Read=%v, want ENODEV", err)
	}
	if _, err := s.Read(); !errors.Is(err, syscall.ENODEV) {
		t.Errorf("next Read=%v, want ENODEV again", err)
	}
	done := make(chan error)
	go func() { done <- s.Close() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung on a gone device")
	}
}

func TestStream_SubmitFailureOnStartIsReturned(t *testing.T) {
	k, bufs := newFakeKernel(3, 8)
	k.submitErr = syscall.ENOMEM
	if _, err := startStream(k, bufs); !errors.Is(err, syscall.ENOMEM) {
		t.Fatalf("startStream=%v, want ENOMEM", err)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
