package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"time"
)

// DefaultTxCycleInterval — период публикации mock-событий tx_cycle по умолчанию.
const DefaultTxCycleInterval = 5 * time.Second

// TxCycleBroadcaster — минимальный интерфейс SSE-хаба, нужный mock-сервису.
// Намеренно узкий, чтобы упростить юнит-тестирование.
type TxCycleBroadcaster interface {
	Broadcast(eventType string, data []byte)
}

// TxCycleGroupSource — источник текущего состава активной группы (NORAD ID).
type TxCycleGroupSource interface {
	GroupNoradIDs() []int
}

// TransmitterRef — узкая ссылка на передатчик для mock/SSE.
// Содержит только то, что нужно фронту для сопоставления (UUID — стабильный
// идентификатор от SatNOGS, используется как rowId в auto-link/heat-grid).
type TransmitterRef struct {
	UUID string
}

// TxCatalog — каталог активных передатчиков КА.
// Возвращает только активные (alive + status=active) с валидным downlink —
// чтобы mock не генерировал пакеты для неактуальных передатчиков.
type TxCatalog interface {
	ListActiveTransmitters(noradID int) []TransmitterRef
}

// txCycleEvent — JSON-структура SSE-события "tx_cycle".
type txCycleEvent struct {
	TS         int64        `json:"ts"`
	Satellites []txCycleSat `json:"satellites"`
}

type txCycleSat struct {
	NoradID      int         `json:"norad_id"`
	Transmitters []txCycleTx `json:"transmitters"`
}

type txCycleTx struct {
	UUID    string  `json:"uuid"`
	Packets int     `json:"packets"`
	Power   float64 `json:"power"`
}

// TxCycleMock — генератор фейковых событий "tx_cycle" для разработки UI
// нижней панели Авто-режима (auto-link + heat-grid TX × циклы).
//
// На каждом тике:
//  1. Берёт NORAD ID активной группы из TxCycleGroupSource.
//  2. Для каждого КА читает список активных передатчиков из TxCatalog.
//  3. Для каждого передатчика генерирует случайные packets и power
//     (~25% передатчиков «молчат» в каждом цикле для наглядности).
//  4. Отправляет одно SSE-событие "tx_cycle" с полным снимком цикла.
//
// Это временное решение до реализации ScanStrategy (см. ADR-004 §3).
// Когда появится реальная стратегия сканирования — TxCycleMock будет заменён,
// формат события "tx_cycle" сохранится.
type TxCycleMock struct {
	hub      TxCycleBroadcaster
	source   TxCycleGroupSource
	catalog  TxCatalog
	interval time.Duration
	rng      *rand.Rand

	// Параметры генерации, вынесены для тестов.
	silentProbability float64 // вероятность что передатчик «молчит»
	maxPackets        int     // максимум пакетов в активной ячейке
}

// NewTxCycleMock конструктор.
// Если interval <= 0 — берётся DefaultTxCycleInterval.
// Если catalog == nil — Run() ничего не делает.
func NewTxCycleMock(
	hub TxCycleBroadcaster,
	source TxCycleGroupSource,
	catalog TxCatalog,
	interval time.Duration,
) *TxCycleMock {
	if interval <= 0 {
		interval = DefaultTxCycleInterval
	}
	seed1 := uint64(time.Now().UnixNano())
	seed2 := uint64(0xDEADBEEFCAFEBABE)
	return &TxCycleMock{
		hub:               hub,
		source:            source,
		catalog:           catalog,
		interval:          interval,
		rng:               rand.New(rand.NewPCG(seed1, seed2)),
		silentProbability: 0.25,
		maxPackets:        40,
	}
}

// Run генерирует tx_cycle-события до отмены контекста.
// Безопасно вызывать с nil-сервисом: метод просто завершится.
func (m *TxCycleMock) Run(ctx context.Context) {
	if m == nil || m.hub == nil || m.source == nil || m.catalog == nil {
		slog.Info("tx_cycle mock disabled (missing dependencies)")
		return
	}
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	slog.Info("tx_cycle mock started", slog.Duration("interval", m.interval))

	// Первый тик через короткую паузу — чтобы satnogsService успел подгрузить
	// каталог при первом satellite_group_update; тогда новый клиент сразу
	// получит ненулевую активность из кеша SSE Hub.
	warmup := time.NewTimer(2 * time.Second)
	defer warmup.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("tx_cycle mock stopped")
			return
		case <-warmup.C:
			m.tick()
		case <-ticker.C:
			m.tick()
		}
	}
}

// tick — один цикл генерации и рассылки.
func (m *TxCycleMock) tick() {
	norads := m.source.GroupNoradIDs()
	if len(norads) == 0 {
		return
	}
	sats := make([]txCycleSat, 0, len(norads))
	for _, norad := range norads {
		txs := m.generateForSatellite(norad)
		if len(txs) == 0 {
			continue
		}
		sats = append(sats, txCycleSat{
			NoradID:      norad,
			Transmitters: txs,
		})
	}
	if len(sats) == 0 {
		return
	}
	payload := txCycleEvent{
		TS:         time.Now().UTC().UnixMilli(),
		Satellites: sats,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("tx_cycle mock marshal failed", slog.String("error", err.Error()))
		return
	}
	m.hub.Broadcast("tx_cycle", data)
}

// generateForSatellite — случайные packets/power для всех передатчиков КА.
func (m *TxCycleMock) generateForSatellite(norad int) []txCycleTx {
	refs := m.catalog.ListActiveTransmitters(norad)
	if len(refs) == 0 {
		return nil
	}
	out := make([]txCycleTx, 0, len(refs))
	for _, ref := range refs {
		if ref.UUID == "" {
			continue
		}
		if m.rng.Float64() < m.silentProbability {
			out = append(out, txCycleTx{UUID: ref.UUID, Packets: 0, Power: 0})
			continue
		}
		packets := 1 + m.rng.IntN(m.maxPackets)
		power := 0.15 + m.rng.Float64()*0.85
		out = append(out, txCycleTx{
			UUID:    ref.UUID,
			Packets: packets,
			Power:   power,
		})
	}
	return out
}
