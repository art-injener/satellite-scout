//go:build linux

package usbdrivers

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
)

// ErrClosed — поток закрыт.
var ErrClosed = errors.New("usb stream closed")

// urbKernel — очередь асинхронных передач ядра: отправить передачу с номером i, отменить её,
// дождаться любой завершённой, отпустить память после последней. В тестах её заменяет
// поддельное ядро.
type urbKernel interface {
	submit(i int) error
	discard(i int) error
	reap() (i int, status int32, actual int, err error)
	release()
}

type reaped struct {
	idx    int
	status int32
	actual int
}

// Stream — непрерывное чтение с конечной точки: все буферы, кроме отданного читателю,
// стоят в очереди ядра, поэтому пока читатель разбирает один, остальные заполняются.
// Read вызывается из одной горутины; Close — из любой.
type Stream struct {
	k    urbKernel
	bufs [][]byte

	ready      chan reaped   // завершённые передачи по порядку завершения
	reaperDone chan struct{} // горутина сбора завершилась
	closeOnce  sync.Once

	mu       sync.Mutex
	cond     *sync.Cond
	inflight []bool
	count    int // передач в ядре
	held     int // буфер у читателя, -1 — нет
	closing  bool
	err      error // сбор передач прервался (устройство отключено)
}

// startStream ставит в очередь все буферы и запускает сбор завершённых передач.
func startStream(k urbKernel, bufs [][]byte) (*Stream, error) {
	s := &Stream{
		k: k, bufs: bufs,
		ready:      make(chan reaped, len(bufs)),
		reaperDone: make(chan struct{}),
		inflight:   make([]bool, len(bufs)),
		held:       -1,
	}
	s.cond = sync.NewCond(&s.mu)
	go s.reapLoop()

	for i := range bufs {
		s.mu.Lock()
		err := k.submit(i)
		if err == nil {
			s.inflight[i] = true
			s.count++
			s.cond.Signal()
		}
		s.mu.Unlock()
		if err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("usb submit urb: %w", err)
		}
	}
	return s, nil
}

func (s *Stream) reapLoop() {
	defer close(s.reaperDone)
	for {
		s.mu.Lock()
		for s.count == 0 && !s.closing {
			s.cond.Wait()
		}
		if s.count == 0 {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		i, status, actual, err := s.k.reap()
		s.mu.Lock()
		if err != nil {
			// Ядро больше не отдаёт передачи (устройство отключено): ждать нечего.
			s.err = err
			s.count = 0
			clear(s.inflight)
			s.mu.Unlock()
			return
		}
		s.inflight[i] = false
		s.count--
		closing := s.closing
		s.mu.Unlock()
		if !closing {
			s.ready <- reaped{idx: i, status: status, actual: actual}
		}
	}
}

// Read возвращает следующий заполненный буфер. Он действителен до следующего Read:
// тогда буфер возвращается в очередь ядра.
func (s *Stream) Read() ([]byte, error) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	if s.held >= 0 && s.err == nil {
		if err := s.k.submit(s.held); err != nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("usb submit urb: %w", err)
		}
		s.inflight[s.held] = true
		s.count++
		s.held = -1
		s.cond.Signal()
	}
	s.mu.Unlock()

	select {
	case r := <-s.ready:
		return s.take(r)
	case <-s.reaperDone:
		select {
		case r := <-s.ready:
			return s.take(r)
		default:
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closing {
			return nil, ErrClosed
		}
		return nil, fmt.Errorf("usb reap urb: %w", s.err)
	}
}

func (s *Stream) take(r reaped) ([]byte, error) {
	s.mu.Lock()
	s.held = r.idx
	closing := s.closing
	s.mu.Unlock()
	if closing {
		return nil, ErrClosed
	}
	if r.status != 0 {
		return nil, fmt.Errorf("usb urb: %w", syscall.Errno(-r.status))
	}
	return s.bufs[r.idx][:r.actual], nil
}

// Close отменяет передачи в ядре и ждёт, пока ядро вернёт все: только после этого
// буферы можно отпускать.
func (s *Stream) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		var pending []int
		for i, in := range s.inflight {
			if in {
				pending = append(pending, i)
			}
		}
		s.cond.Broadcast()
		s.mu.Unlock()
		for _, i := range pending {
			// Уже завершённую ядро не отменяет; её всё равно заберёт сбор.
			_ = s.k.discard(i)
		}
		<-s.reaperDone
		s.k.release()
	})
	return nil
}
