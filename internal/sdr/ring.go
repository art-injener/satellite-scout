package sdr

import "sync"

// DataRing — кольцевой буфер IQ-фреймов с fan-out подписчиками.
// Один writer, N reader. При переполнении подписчика — drop oldest (неблокирующий send).
type DataRing struct {
	mu          sync.Mutex
	subscribers []chan IQFrame
	capacity    int
	closed      bool
}

// NewDataRing создаёт кольцевой буфер. capacity — размер канала каждого подписчика.
func NewDataRing(capacity int) *DataRing {
	if capacity < 1 {
		capacity = 1
	}
	return &DataRing{capacity: capacity}
}

// Write отправляет фрейм всем подписчикам. При полном канале — сбрасывает oldest.
func (r *DataRing) Write(frame IQFrame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	for _, ch := range r.subscribers {
		select {
		case ch <- frame:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- frame:
			default:
			}
		}
	}
}

// Subscribe создаёт нового подписчика и возвращает канал для чтения фреймов.
func (r *DataRing) Subscribe() <-chan IQFrame {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan IQFrame, r.capacity)
	r.subscribers = append(r.subscribers, ch)
	return ch
}

// Close закрывает все каналы подписчиков.
func (r *DataRing) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for _, ch := range r.subscribers {
		close(ch)
	}
	r.subscribers = nil
}
