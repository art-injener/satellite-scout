package sdr

import (
	"errors"
	"fmt"
	"sync"
)

// ErrUnknownRadioPath — радиотракт не зарегистрирован в хабе.
var ErrUnknownRadioPath = errors.New("unknown radio path")

// ErrDriverNotImplemented — SDR ещё не поддерживается
var ErrDriverNotImplemented = errors.New("driver not yet implemented")

// ReceiverFactory — создаёт экземпляр Receiver для радиотракта.
type ReceiverFactory func() (Receiver, error)

type hubEntry struct {
	factory  ReceiverFactory
	receiver Receiver // nil, пока Get ещё не вызывали
}

// ReceiverHub — один приёмник на каждый радиотракт из конфига.
// Register только запоминает, как создавать; Get открывает при первом обращении.
type ReceiverHub struct {
	mu      sync.Mutex
	entries map[string]*hubEntry
}

// NewReceiverHub создаёт пустой хаб приёмников.
func NewReceiverHub() *ReceiverHub {
	return &ReceiverHub{
		entries: make(map[string]*hubEntry),
	}
}

// Register привязывает фабрику к радиотракту (pathID). Приёмник не открывается до Get.
func (h *ReceiverHub) Register(pathID string, factory ReceiverFactory) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[pathID] = &hubEntry{factory: factory}
}

// Get возвращает Receiver для радиотракта pathID. При первом вызове создаёт через фабрику.
// Ошибка фабрики не кешируется — следующий Get повторит попытку.
func (h *ReceiverHub) Get(pathID string) (Receiver, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.entries[pathID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownRadioPath, pathID)
	}
	if entry.receiver != nil {
		return entry.receiver, nil
	}

	r, err := entry.factory()
	if err != nil {
		return nil, fmt.Errorf("receiver [%q] error: %w", pathID, err)
	}
	entry.receiver = r
	return r, nil
}

// CloseAll останавливает все открытые Receiver. Ошибки агрегируются.
func (h *ReceiverHub) CloseAll() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var errs []error
	for id, entry := range h.entries {
		if entry.receiver != nil {
			if err := entry.receiver.Stop(); err != nil {
				errs = append(errs, fmt.Errorf("receiver [%q] stopping error: %w", id, err))
			}
			entry.receiver = nil
		}
	}
	return errors.Join(errs...)
}

// List возвращает id всех зарегистрированных радиотрактов.
func (h *ReceiverHub) List() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	ids := make([]string, 0, len(h.entries))
	for id := range h.entries {
		ids = append(ids, id)
	}
	return ids
}
