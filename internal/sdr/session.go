package sdr

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// sessionRingCapacity — сколько блоков ждёт медленного зрителя, прежде чем старые начнут выбрасываться.
const sessionRingCapacity = 16

// session — один запущенный поток устройства, раздаваемый всем зрителям тракта.
type session struct {
	ring    *DataRing
	cancel  context.CancelFunc
	viewers int
}

// Subscribe подключает зрителя к потоку IQ тракта и возвращает канал кадров и функцию отписки.
// Поток устройства один на тракт: первый зритель запускает приёмник, последний отписавшийся
// останавливает. Медленный зритель теряет старые кадры, не задерживая остальных.
// Канал закрывается при отписке, при остановке хаба и когда устройство само завершило поток.
func (h *ReceiverHub) Subscribe(pathID string) (<-chan IQFrame, func(), error) {
	receiver, err := h.Get(pathID)
	if err != nil {
		return nil, nil, err
	}
	rate, err := h.SampleRate(pathID)
	if err != nil {
		return nil, nil, err
	}
	h.mu.Lock()
	entry := h.entries[pathID]
	h.mu.Unlock()

	entry.sessMu.Lock()
	defer entry.sessMu.Unlock()

	var frames <-chan IQFrame
	s := entry.session
	if s == nil {
		ctx, cancel := context.WithCancel(context.Background())
		in, startErr := receiver.StartStream(ctx, StreamConfig{SampleRate: rate, BlockSize: DefaultBlockSize})
		if startErr != nil {
			cancel()
			return nil, nil, fmt.Errorf("radio path %q: %w", pathID, startErr)
		}
		s = &session{ring: NewDataRing(sessionRingCapacity), cancel: cancel}
		entry.session = s
		// Подписка до запуска переноса: иначе первые кадры уйдут мимо первого зрителя.
		frames = s.ring.Subscribe()
		go pumpSession(entry, receiver, s, in)
	} else {
		frames = s.ring.Subscribe()
	}
	s.viewers++

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() { leaveSession(entry, receiver, s, frames) })
	}
	return frames, unsubscribe, nil
}

// pumpSession переносит кадры устройства в кольцо зрителей. Когда устройство завершило поток само,
// сеанс убирается из тракта до закрытия каналов: переподключившийся зритель запустит новый.
func pumpSession(entry *hubEntry, receiver Receiver, s *session, in <-chan IQFrame) {
	for frame := range in {
		s.ring.Write(frame)
	}
	entry.sessMu.Lock()
	if entry.session == s {
		entry.session = nil
		s.cancel()
		if err := receiver.Stop(); err != nil {
			slog.Warn("sdr: receiver stop after stream end", "receiver", receiver.Name(), "error", err)
		}
	}
	entry.sessMu.Unlock()
	s.ring.Close()
}

// leaveSession отписывает зрителя; последний зритель текущего сеанса останавливает приёмник.
func leaveSession(entry *hubEntry, receiver Receiver, s *session, frames <-chan IQFrame) {
	s.ring.Unsubscribe(frames)

	entry.sessMu.Lock()
	defer entry.sessMu.Unlock()
	s.viewers--
	if s.viewers > 0 || entry.session != s {
		return
	}
	entry.session = nil
	s.cancel()
	if err := receiver.Stop(); err != nil {
		slog.Warn("sdr: receiver stop after last viewer", "receiver", receiver.Name(), "error", err)
	}
}
