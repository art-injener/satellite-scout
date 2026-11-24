package sdr

import (
	"sync"
	"sync/atomic"
)

type ringSubscriber struct {
	ch      chan IQFrame
	dropped atomic.Uint64
}

// DataRing — кольцевой буфер IQ-фреймов с fan-out подписчиками.
// Один writer, N reader. При переполнении подписчика сбрасывается самый старый фрейм (неблокирующая запись).
type DataRing struct {
	mu          sync.Mutex
	subscribers []*ringSubscriber
	capacity    int
	closed      bool
	dropped     atomic.Uint64
}

// NewDataRing создаёт кольцевой буфер. capacity — размер канала каждого подписчика.
func NewDataRing(capacity int) *DataRing {
	if capacity < 1 {
		capacity = 1
	}
	return &DataRing{capacity: capacity}
}

// Write отправляет фрейм всем подписчикам. При полном канале сбрасывает самый старый фрейм
// и увеличивает счётчики пропусков.
func (r *DataRing) Write(frame IQFrame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	for _, sub := range r.subscribers {
		select {
		case sub.ch <- frame:
		default:
			select {
			case <-sub.ch:
				sub.dropped.Add(1)
				r.dropped.Add(1)
			default:
			}
			select {
			case sub.ch <- frame:
			default:
				sub.dropped.Add(1)
				r.dropped.Add(1)
			}
		}
	}
}

// Dropped возвращает суммарное число пропущенных фреймов по всем подписчикам кольца.
func (r *DataRing) Dropped() uint64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

// DroppedFor возвращает число пропущенных фреймов конкретного подписчика.
func (r *DataRing) DroppedFor(sub <-chan IQFrame) uint64 {
	if r == nil || sub == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.subscribers {
		if s.ch == sub {
			return s.dropped.Load()
		}
	}
	return 0
}

// Subscribe создаёт нового подписчика и возвращает канал для чтения фреймов.
// После Close возвращает уже закрытый канал.
func (r *DataRing) Subscribe() <-chan IQFrame {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan IQFrame, r.capacity)
	if r.closed {
		close(ch)
		return ch
	}
	r.subscribers = append(r.subscribers, &ringSubscriber{ch: ch})
	return ch
}

// Unsubscribe убирает подписчика и закрывает его канал. Повторный вызов и вызов после Close безопасны.
func (r *DataRing) Unsubscribe(sub <-chan IQFrame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.subscribers {
		if s.ch == sub {
			close(s.ch)
			r.subscribers = append(r.subscribers[:i], r.subscribers[i+1:]...)
			return
		}
	}
}

// Close закрывает все каналы подписчиков.
func (r *DataRing) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for _, s := range r.subscribers {
		close(s.ch)
	}
	r.subscribers = nil
}
