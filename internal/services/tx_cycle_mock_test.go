package services

import (
	"encoding/json"
	"testing"
)

// ── txCycleRing ──────────────────────────────────────────────────────────────

func TestTxCycleRing_PushAndSnapshot(t *testing.T) {
	r := newTxCycleRing(4)

	r.push(txCycleHistoryItem{Packets: 1, Power: 0.1})
	r.push(txCycleHistoryItem{Packets: 2, Power: 0.2})
	r.push(txCycleHistoryItem{Packets: 3, Power: 0.3})

	snap := r.snapshot()
	if len(snap) != 3 {
		t.Fatalf("len = %d, want 3", len(snap))
	}
	// Порядок: от новейшего к старейшему
	if snap[0].Packets != 3 || snap[1].Packets != 2 || snap[2].Packets != 1 {
		t.Fatalf("order: got %v", snap)
	}
}

func TestTxCycleRing_Overflow(t *testing.T) {
	r := newTxCycleRing(3)

	for i := 1; i <= 5; i++ {
		r.push(txCycleHistoryItem{Packets: i, Power: float64(i) * 0.1})
	}

	snap := r.snapshot()
	if len(snap) != 3 {
		t.Fatalf("len = %d, want 3", len(snap))
	}
	// Должны остаться 5, 4, 3 (последние три)
	if snap[0].Packets != 5 || snap[1].Packets != 4 || snap[2].Packets != 3 {
		t.Fatalf("overflow order: got %v", snap)
	}
}

func TestTxCycleRing_EmptySnapshot(t *testing.T) {
	r := newTxCycleRing(4)
	snap := r.snapshot()
	if len(snap) != 0 {
		t.Fatalf("empty ring snapshot len = %d, want 0", len(snap))
	}
}

// ── txState ──────────────────────────────────────────────────────────────────

func TestTxState_TotalPacketsAccumulates(t *testing.T) {
	st := newTxState(3)
	st.ring.push(txCycleHistoryItem{Packets: 10})
	st.totalPackets += 10
	st.ring.push(txCycleHistoryItem{Packets: 5})
	st.totalPackets += 5
	st.ring.push(txCycleHistoryItem{Packets: 7})
	st.totalPackets += 7

	if st.totalPackets != 22 {
		t.Fatalf("totalPackets = %d, want 22", st.totalPackets)
	}

	// Добавляем ещё — буфер вытеснит старое, но total продолжит расти
	st.ring.push(txCycleHistoryItem{Packets: 3})
	st.totalPackets += 3

	if st.totalPackets != 25 {
		t.Fatalf("totalPackets after overflow = %d, want 25", st.totalPackets)
	}
	snap := st.ring.snapshot()
	if len(snap) != 3 {
		t.Fatalf("ring len = %d, want 3", len(snap))
	}
}

// ── TxCycleMock: интеграция ─────────────────────────────────────────────────

type mockBroadcaster struct {
	lastEvent string
	lastData  []byte
	calls     int
}

func (b *mockBroadcaster) Broadcast(eventType string, data []byte) {
	b.lastEvent = eventType
	b.lastData = data
	b.calls++
}

type mockGroupSource struct {
	norads []int
}

func (s *mockGroupSource) GroupNoradIDs() []int {
	return s.norads
}

type mockCatalog struct {
	txByNorad map[int][]TransmitterRef
}

func (c *mockCatalog) ListActiveTransmitters(noradID int) []TransmitterRef {
	return c.txByNorad[noradID]
}

func newTestMock(norads []int, catalog map[int][]TransmitterRef) (*TxCycleMock, *mockBroadcaster) {
	hub := &mockBroadcaster{}
	src := &mockGroupSource{norads: norads}
	cat := &mockCatalog{txByNorad: catalog}
	m := NewTxCycleMock(hub, src, cat, 0)
	m.silentProbability = 0 // детерминированная генерация
	return m, hub
}

func TestTxCycleMock_HistoryInEvent(t *testing.T) {
	catalog := map[int][]TransmitterRef{
		25544: {{UUID: "tx-a"}, {UUID: "tx-b"}},
	}
	m, hub := newTestMock([]int{25544}, catalog)

	// 3 тика — должно накопиться 3 элемента истории
	m.tick()
	m.tick()
	m.tick()

	if hub.calls != 3 {
		t.Fatalf("broadcasts = %d, want 3", hub.calls)
	}

	var ev txCycleEvent
	if err := json.Unmarshal(hub.lastData, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ev.Satellites) != 1 {
		t.Fatalf("sats = %d, want 1", len(ev.Satellites))
	}
	sat := ev.Satellites[0]
	if len(sat.Transmitters) != 2 {
		t.Fatalf("txs = %d, want 2", len(sat.Transmitters))
	}
	for _, tx := range sat.Transmitters {
		if len(tx.History) != 3 {
			t.Errorf("tx %s history len = %d, want 3", tx.UUID, len(tx.History))
		}
		// history[0] = текущий визит = совпадает с packets/power
		if tx.History[0].Packets != tx.Packets {
			t.Errorf("tx %s: history[0].Packets=%d != packets=%d", tx.UUID, tx.History[0].Packets, tx.Packets)
		}
	}
}

func TestTxCycleMock_TotalPacketsAccumulates(t *testing.T) {
	catalog := map[int][]TransmitterRef{
		25544: {{UUID: "tx-a"}},
	}
	m, hub := newTestMock([]int{25544}, catalog)

	var totalAfter3 int
	for i := 0; i < 3; i++ {
		m.tick()
	}

	var ev txCycleEvent
	if err := json.Unmarshal(hub.lastData, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	totalAfter3 = ev.Satellites[0].Transmitters[0].TotalPackets
	if totalAfter3 <= 0 {
		t.Fatalf("total_packets after 3 ticks = %d, want > 0", totalAfter3)
	}

	// Ещё один тик — total должен вырасти
	m.tick()
	if err := json.Unmarshal(hub.lastData, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	totalAfter4 := ev.Satellites[0].Transmitters[0].TotalPackets
	if totalAfter4 <= totalAfter3 {
		t.Fatalf("total_packets after 4 ticks = %d, want > %d", totalAfter4, totalAfter3)
	}
}

func TestTxCycleMock_HistoryCapacity(t *testing.T) {
	catalog := map[int][]TransmitterRef{
		25544: {{UUID: "tx-a"}},
	}
	m, hub := newTestMock([]int{25544}, catalog)

	// stripCapacity+5 тиков — буфер не должен расти больше stripCapacity
	for i := 0; i < stripCapacity+5; i++ {
		m.tick()
	}

	var ev txCycleEvent
	if err := json.Unmarshal(hub.lastData, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tx := ev.Satellites[0].Transmitters[0]
	if len(tx.History) != stripCapacity {
		t.Fatalf("history len = %d, want %d", len(tx.History), stripCapacity)
	}
	// total_packets должен быть больше суммы history (часть вытеснена)
	histSum := 0
	for _, h := range tx.History {
		histSum += h.Packets
	}
	if tx.TotalPackets < histSum {
		t.Fatalf("total_packets=%d < histSum=%d", tx.TotalPackets, histSum)
	}
}

func TestTxCycleMock_GroupChange(t *testing.T) {
	catalog := map[int][]TransmitterRef{
		25544: {{UUID: "tx-a"}},
		40069: {{UUID: "tx-b"}},
	}
	src := &mockGroupSource{norads: []int{25544, 40069}}
	hub := &mockBroadcaster{}
	cat := &mockCatalog{txByNorad: catalog}
	m := NewTxCycleMock(hub, src, cat, 0)
	m.silentProbability = 0

	// Накопим историю для обоих КА
	m.tick()
	m.tick()

	if _, ok := m.history[25544]; !ok {
		t.Fatal("history[25544] missing after ticks")
	}
	if _, ok := m.history[40069]; !ok {
		t.Fatal("history[40069] missing after ticks")
	}

	// Убираем 40069 из группы
	src.norads = []int{25544}
	m.tick()

	if _, ok := m.history[40069]; ok {
		t.Fatal("history[40069] should be cleaned after group change")
	}
	if _, ok := m.history[25544]; !ok {
		t.Fatal("history[25544] should remain")
	}

	// Проверяем что 25544 продолжает накапливать
	var ev txCycleEvent
	if err := json.Unmarshal(hub.lastData, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tx := ev.Satellites[0].Transmitters[0]
	if len(tx.History) != 3 {
		t.Fatalf("history len = %d, want 3 (continued)", len(tx.History))
	}
}

func TestTxCycleMock_EmptyGroup(t *testing.T) {
	catalog := map[int][]TransmitterRef{}
	m, hub := newTestMock([]int{}, catalog)
	m.tick()
	if hub.calls != 0 {
		t.Fatalf("should not broadcast for empty group, got %d calls", hub.calls)
	}
}
