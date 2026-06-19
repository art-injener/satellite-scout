package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
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
// идентификатор от SatNOGS, в auto-link совпадает с rowId строки).
type TransmitterRef struct {
	UUID string
}

// TxCatalog — каталог активных передатчиков КА.
// Возвращает только активные (alive + status=active) с валидным downlink —
// чтобы mock не генерировал пакеты для неактуальных передатчиков.
type TxCatalog interface {
	ListActiveTransmitters(noradID int) []TransmitterRef
}

// stripCapacity — размер кольцевого буфера истории tx_cycle на передатчик.
// Рассчитан по числу отображаемых ячеек детектора в нижней панели.
const stripCapacity = 10

// ── Кольцевой буфер истории ──────────────────────────────────────────

type txCycleHistoryItem struct {
	Packets int     `json:"packets"`
	Power   float64 `json:"power"`
}

type txCycleRing struct {
	items    []txCycleHistoryItem
	head     int
	count    int
	capacity int
}

func newTxCycleRing(capacity int) *txCycleRing {
	if capacity <= 0 {
		capacity = stripCapacity
	}
	return &txCycleRing{items: make([]txCycleHistoryItem, capacity), capacity: capacity}
}

func (r *txCycleRing) push(item txCycleHistoryItem) {
	r.items[r.head] = item
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
}

// snapshot возвращает срез от новейшего к старейшему.
func (r *txCycleRing) snapshot() []txCycleHistoryItem {
	out := make([]txCycleHistoryItem, r.count)
	for i := range r.count {
		idx := (r.head - 1 - i + r.capacity) % r.capacity
		out[i] = r.items[idx]
	}
	return out
}

// ── Состояние одного передатчика ─────────────────────────────────────

type txState struct {
	ring         *txCycleRing
	totalPackets int
	totalFailed  int
}

func newTxState(ringCap int) *txState {
	return &txState{ring: newTxCycleRing(ringCap)}
}

// ── JSON-структуры SSE-события "tx_cycle" ────────────────────────────

type txCycleEvent struct {
	TS         int64        `json:"ts"`
	Satellites []txCycleSat `json:"satellites"`
}

type txCycleSat struct {
	NoradID      int         `json:"norad_id"`
	Transmitters []txCycleTx `json:"transmitters"`
}

type txCycleTx struct {
	UUID         string               `json:"uuid"`
	Packets      int                  `json:"packets"`
	Power        float64              `json:"power"`
	TotalPackets int                  `json:"total_packets"`
	History      []txCycleHistoryItem `json:"history"`
	// Параметры демодулятора текущего визита.
	PacketsFailed int     `json:"packets_failed"` // Не прошли CRC в этом визите.
	TotalFailed   int     `json:"total_failed"`   // Σ битых пакетов за пролёт.
	SNRDb         float64 `json:"snr_db"`         // SNR в полосе IF, дБ.
	Lock          string  `json:"lock"`           // "OK" | "SEARCH" | "LOST".
}

// TxCycleMock — генератор фейковых событий "tx_cycle" для auto-link
// в нижней панели Авто-режима.
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

	// noradID → uuid → *txState (кольцевой буфер + Σ пакетов за пролёт).
	history map[int]map[string]*txState
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
		history:           make(map[int]map[string]*txState),
	}
}

// Run генерирует tx_cycle-события до отмены контекста.
// Безопасно вызывать с nil-сервисом: метод просто завершится.
func (m *TxCycleMock) Run(ctx context.Context) {
	if m == nil || m.hub == nil || m.source == nil || m.catalog == nil {
		slog.InfoContext(ctx, "tx_cycle mock disabled (missing dependencies)")
		return
	}
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	slog.InfoContext(ctx, "tx_cycle mock started", slog.Duration("interval", m.interval))

	// Первый тик через короткую паузу — чтобы satnogsService успел подгрузить
	// каталог при первом satellite_group_update; тогда новый клиент сразу
	// получит ненулевую активность из кеша SSE Hub.
	warmup := time.NewTimer(2 * time.Second)
	defer warmup.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "tx_cycle mock stopped")
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

	m.cleanupStaleNorad(norads)

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

// cleanupStaleNorad удаляет буферы КА, ушедших из группы.
func (m *TxCycleMock) cleanupStaleNorad(active []int) {
	set := make(map[int]struct{}, len(active))
	for _, id := range active {
		set[id] = struct{}{}
	}
	for id := range m.history {
		if _, ok := set[id]; !ok {
			delete(m.history, id)
		}
	}
}

// getOrCreateTxState возвращает txState для передатчика, создавая при необходимости.
func (m *TxCycleMock) getOrCreateTxState(norad int, uuid string) *txState {
	byUUID, ok := m.history[norad]
	if !ok {
		byUUID = make(map[string]*txState)
		m.history[norad] = byUUID
	}
	st, ok := byUUID[uuid]
	if !ok {
		st = newTxState(stripCapacity)
		byUUID[uuid] = st
	}
	return st
}

// txVisit — параметры одного визита сканера на передатчик (для одной строки UI).
type txVisit struct {
	packets       int
	power         float64
	packetsFailed int
	snrDB         float64
	lock          string
}

// generateVisit — случайный визит с согласованными параметрами:
// активный → SNR>0, Lock=OK/SEARCH, packets_failed ≈ 12%·(1-power);
// молчащий → Lock=LOST, SNR=0.
func (m *TxCycleMock) generateVisit() txVisit {
	if m.rng.Float64() < m.silentProbability {
		return txVisit{lock: "LOST"}
	}
	packets := 1 + m.rng.IntN(m.maxPackets)
	power := 0.15 + m.rng.Float64()*0.85
	// SNR ≈ 5…23 дБ, монотонно растёт с power, шум ±1.5 дБ.
	snrDB := 5.0 + power*18.0 + (m.rng.Float64()-0.5)*3.0
	// Доля битых пакетов 0…12% — обратно пропорциональна power.
	failRate := 0.12 * (1.0 - power)
	if failRate < 0 {
		failRate = 0
	}
	packetsFailed := int(float64(packets) * failRate)
	// Lock=SEARCH при слабом сигнале (~12% всех визитов), иначе OK.
	lock := "OK"
	if power < 0.30 && m.rng.Float64() < 0.4 {
		lock = "SEARCH"
	}
	return txVisit{
		packets:       packets,
		power:         power,
		packetsFailed: packetsFailed,
		snrDB:         snrDB,
		lock:          lock,
	}
}

// generateForSatellite — случайные параметры визитов для всех передатчиков КА.
// Обновляет кольцевой буфер и накопительные счётчики каждого передатчика.
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
		v := m.generateVisit()

		st := m.getOrCreateTxState(norad, ref.UUID)
		st.ring.push(txCycleHistoryItem{Packets: v.packets, Power: v.power})
		st.totalPackets += v.packets
		st.totalFailed += v.packetsFailed

		out = append(out, txCycleTx{
			UUID:          ref.UUID,
			Packets:       v.packets,
			Power:         v.power,
			TotalPackets:  st.totalPackets,
			History:       st.ring.snapshot(),
			PacketsFailed: v.packetsFailed,
			TotalFailed:   st.totalFailed,
			SNRDb:         roundFloat(v.snrDB, 1),
			Lock:          v.lock,
		})
	}
	return out
}

// roundFloat округляет до заданного числа знаков после запятой.
func roundFloat(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}
