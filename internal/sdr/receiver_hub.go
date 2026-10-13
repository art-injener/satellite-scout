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

// PathConfig — настройки радиотракта из конфига, нужные хабу.
type PathConfig struct {
	// SampleRateHz — частота дискретизации из конфига; 0 — рекомендуемая драйвером.
	SampleRateHz uint32
}

type hubEntry struct {
	cfg      PathConfig
	factory  ReceiverFactory
	receiver Receiver // nil, пока Get ещё не вызывали

	// Под sessMu нельзя брать ReceiverHub.mu: CloseAll берёт их в порядке mu, затем sessMu.
	sessMu  sync.Mutex
	session *session // nil, пока нет зрителей
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

// Register привязывает фабрику и настройки к радиотракту (pathID). Приёмник не открывается до Get.
func (h *ReceiverHub) Register(pathID string, cfg PathConfig, factory ReceiverFactory) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[pathID] = &hubEntry{cfg: cfg, factory: factory}
}

// SampleRate возвращает частоту дискретизации тракта: из конфига или рекомендуемую драйвером,
// с проверкой по возможностям драйвера. Открывает приёмник, если он ещё не открыт.
func (h *ReceiverHub) SampleRate(pathID string) (uint32, error) {
	r, err := h.Get(pathID)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	cfg := h.entries[pathID].cfg
	h.mu.Unlock()

	rate, err := ResolveSampleRate(r.Capabilities(), cfg.SampleRateHz)
	if err != nil {
		return 0, fmt.Errorf("radio path %q: %w", pathID, err)
	}
	return rate, nil
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
		entry.sessMu.Lock()
		if entry.session != nil {
			entry.session.cancel()
			entry.session = nil
		}
		entry.sessMu.Unlock()

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
