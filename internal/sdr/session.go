package sdr

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
)

// sessionRingCapacity — сколько блоков ждёт медленного зрителя, прежде чем старые начнут выбрасываться.
const sessionRingCapacity = 16

// session — один запущенный поток устройства, раздаваемый всем зрителям тракта.
type session struct {
	ring    *DataRing
	cancel  context.CancelFunc // отменяет текущий поток устройства
	viewers int
	// gen — поколение потока устройства: растёт при перезапуске на другой частоте, чтобы
	// перенос кадров заменённого потока не дописал в кольцо прежнюю частоту и не закрыл его.
	// Меняется под sessMu, читается и без него.
	gen atomic.Int64
}

// Subscribe подключает зрителя к потоку IQ тракта и возвращает канал кадров и функцию отписки.
// Поток устройства один на тракт: первый зритель запускает приёмник, последний отписавшийся
// останавливает. Медленный зритель теряет старые кадры, не задерживая остальных.
// Канал закрывается при отписке, при остановке хаба и когда устройство само завершило поток.
func (h *ReceiverHub) Subscribe(pathID string) (<-chan IQFrame, func(), error) {
	receiver, entry, base, err := h.baseRate(pathID)
	if err != nil {
		return nil, nil, err
	}
	tap := h.streamTap()

	entry.sessMu.Lock()
	defer entry.sessMu.Unlock()

	var frames <-chan IQFrame
	s := entry.session
	if s == nil {
		in, cancel, startErr := startDeviceStream(receiver, entry.currentRate(base))
		if startErr != nil {
			return nil, nil, fmt.Errorf("radio path %q: %w", pathID, startErr)
		}
		s = &session{ring: NewDataRing(sessionRingCapacity), cancel: cancel}
		entry.session = s
		// Подписка до запуска переноса: иначе первые кадры уйдут мимо первого зрителя.
		frames = s.ring.Subscribe()
		if tap != nil {
			go tap(pathID, s.ring.Subscribe())
		}
		go pumpSession(entry, receiver, s, in, s.gen.Load())
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

// SetSampleRate переключает частоту дискретизации тракта на одну из SampleRateChoices.
// Идущий поток устройства перезапускается на новой частоте, зрители остаются подписаны.
// Выбор хранится в памяти до перезапуска сервера.
func (h *ReceiverHub) SetSampleRate(pathID string, rate uint32) error {
	receiver, entry, base, err := h.baseRate(pathID)
	if err != nil {
		return err
	}
	if !slices.Contains(SampleRateChoices(receiver.Capabilities(), base), rate) {
		return fmt.Errorf("radio path %q: %w: %d Hz", pathID, ErrSampleRateUnsupported, rate)
	}

	entry.sessMu.Lock()
	defer entry.sessMu.Unlock()

	prev := entry.currentRate(base)
	entry.rate.Store(rate)
	s := entry.session
	if s == nil || rate == prev {
		return nil
	}
	if err := restartDeviceStream(entry, receiver, s, rate); err != nil {
		entry.rate.Store(prev)
		// Не поднялся и прежний поток: перенос кадров старого поколения закроет сеанс сам.
		if backErr := restartDeviceStream(entry, receiver, s, prev); backErr != nil {
			slog.Warn("sdr: receiver restart at previous sample rate failed",
				slog.String("radio_path", pathID), slog.Any("error", backErr))
		}
		return fmt.Errorf("radio path %q: %w", pathID, err)
	}
	return nil
}

// startDeviceStream запускает поток устройства на частоте rate; контекст живёт дольше запросов.
func startDeviceStream(receiver Receiver, rate uint32) (<-chan IQFrame, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())
	in, err := receiver.StartStream(ctx, StreamConfig{SampleRate: rate, BlockSize: DefaultBlockSize})
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return in, cancel, nil
}

// restartDeviceStream останавливает поток устройства и запускает новый на частоте rate,
// направляя его в то же кольцо. Вызывается под entry.sessMu.
func restartDeviceStream(entry *hubEntry, receiver Receiver, s *session, rate uint32) error {
	s.cancel()
	if err := receiver.Stop(); err != nil {
		slog.Warn("sdr: receiver stop before sample rate change", "receiver", receiver.Name(), "error", err)
	}
	in, cancel, err := startDeviceStream(receiver, rate)
	if err != nil {
		return err
	}
	s.cancel = cancel
	go pumpSession(entry, receiver, s, in, s.gen.Add(1))
	return nil
}

// pumpSession переносит кадры устройства в кольцо зрителей. Когда устройство завершило поток само,
// сеанс убирается из тракта до закрытия каналов: переподключившийся зритель запустит новый.
// Поток, заменённый при смене частоты (gen устарело), кольцо не трогает.
func pumpSession(entry *hubEntry, receiver Receiver, s *session, in <-chan IQFrame, gen int64) {
	for frame := range in {
		if s.gen.Load() == gen {
			s.ring.Write(frame)
		}
	}
	entry.sessMu.Lock()
	if s.gen.Load() != gen {
		entry.sessMu.Unlock()
		return
	}
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
