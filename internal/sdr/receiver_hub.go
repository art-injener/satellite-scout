package sdr

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
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
	channel  ChannelSettings
	// rate — частота дискретизации, выбранная оператором; 0 — из конфига или рекомендуемая.
	// Атомарная: читается под sessMu, где ReceiverHub.mu брать нельзя.
	rate atomic.Uint32

	// Под sessMu нельзя брать ReceiverHub.mu: CloseAll берёт их в порядке mu, затем sessMu.
	sessMu  sync.Mutex
	session *session // nil, пока нет зрителей
}

// ReceiverHub — один приёмник на каждый радиотракт из конфига.
// Register только запоминает, как создавать; Get открывает при первом обращении.
type ReceiverHub struct {
	mu      sync.Mutex
	entries map[string]*hubEntry
	tap     StreamTap
}

// StreamTap — попутный получатель потока тракта (демодулятор). Вызывается в отдельной горутине
// при запуске каждого потока устройства. Зрителем не считается: поток не держит, его канал
// закрывается вместе с потоком. Короткую паузу без зрителей и смену частоты дискретизации переживает.
type StreamTap func(pathID string, frames <-chan IQFrame)

// SetStreamTap задаёт попутного получателя для потоков, запущенных после вызова.
func (h *ReceiverHub) SetStreamTap(tap StreamTap) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tap = tap
}

func (h *ReceiverHub) streamTap() StreamTap {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tap
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
	h.entries[pathID] = &hubEntry{cfg: cfg, factory: factory, channel: DefaultChannelSettings()}
}

// Channel возвращает настройки демодулятора тракта.
func (h *ReceiverHub) Channel(pathID string) (ChannelSettings, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.entries[pathID]
	if !ok {
		return ChannelSettings{}, fmt.Errorf("%w: %q", ErrUnknownRadioPath, pathID)
	}
	return entry.channel, nil
}

// GainDB возвращает усиление приёмника тракта; отрицательное — автоусиление.
func (h *ReceiverHub) GainDB(pathID string) (float32, error) {
	r, err := h.Get(pathID)
	if err != nil {
		return 0, err
	}
	return r.Status().GainDB, nil
}

// SetChannel проверяет настройки демодулятора по полосе приёмника и сохраняет их.
// Неподходящие не сохраняются, у тракта остаются прежние.
func (h *ReceiverHub) SetChannel(pathID string, c ChannelSettings) error {
	rate, err := h.SampleRate(pathID)
	if err != nil {
		return err
	}
	if err := c.Validate(rate); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[pathID].channel = c
	return nil
}

// SetDemodOff включает или останавливает демодулятор тракта, не трогая сдвиг, полосу, порог и профиль.
func (h *ReceiverHub) SetDemodOff(pathID string, off bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.entries[pathID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownRadioPath, pathID)
	}
	entry.channel.DemodOff = off
	return nil
}

// SampleRate возвращает частоту дискретизации тракта: выбранную оператором, иначе из конфига
// или рекомендуемую драйвером, с проверкой по возможностям драйвера.
// Открывает приёмник, если он ещё не открыт.
func (h *ReceiverHub) SampleRate(pathID string) (uint32, error) {
	_, entry, base, err := h.baseRate(pathID)
	if err != nil {
		return 0, err
	}
	return entry.currentRate(base), nil
}

// SampleRateChoices возвращает частоты, на которые оператор может переключить тракт.
func (h *ReceiverHub) SampleRateChoices(pathID string) ([]uint32, error) {
	r, _, base, err := h.baseRate(pathID)
	if err != nil {
		return nil, err
	}
	return SampleRateChoices(r.Capabilities(), base), nil
}

// baseRate открывает приёмник тракта и возвращает частоту из конфига или рекомендуемую драйвером.
func (h *ReceiverHub) baseRate(pathID string) (Receiver, *hubEntry, uint32, error) {
	r, err := h.Get(pathID)
	if err != nil {
		return nil, nil, 0, err
	}
	h.mu.Lock()
	entry := h.entries[pathID]
	h.mu.Unlock()

	rate, err := ResolveSampleRate(r.Capabilities(), entry.cfg.SampleRateHz)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("radio path %q: %w", pathID, err)
	}
	return r, entry, rate, nil
}

// currentRate — выбранная оператором частота или base, если он её не менял.
func (e *hubEntry) currentRate(base uint32) uint32 {
	if r := e.rate.Load(); r != 0 {
		return r
	}
	return base
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
			entry.session.stopIdle()
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
