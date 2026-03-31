package services

import (
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func msFromNow(d time.Duration) int64 {
	return time.Now().UTC().Add(d).UnixMilli()
}

func makePass(noradID int, name string, aosOffset, losOffset time.Duration) *tracker.Pass {
	return &tracker.Pass{
		NoradID:  noradID,
		SatName:  name,
		AOS:      msFromNow(aosOffset),
		LOS:      msFromNow(losOffset),
		Duration: losOffset.Seconds() - aosOffset.Seconds(),
	}
}

// ── FindConcurrentPasses ──────────────────────────────────────────────────────

func TestFindConcurrentPasses_EmptyInput(t *testing.T) {
	now := time.Now().UTC()
	result := FindConcurrentPasses(nil, now, 5*time.Minute)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestFindConcurrentPasses_ActivePass(t *testing.T) {
	now := time.Now().UTC()
	// Пролёт начался 1 мин назад, заканчивается через 4 мин → в окне
	passes := []*tracker.Pass{
		makePass(1, "SAT-A", -1*time.Minute, 4*time.Minute),
	}
	result := FindConcurrentPasses(passes, now, 5*time.Minute)
	if len(result) != 1 {
		t.Fatalf("expected 1 pass, got %d", len(result))
	}
	if !result[0].IsVisible {
		t.Error("active pass should be visible")
	}
}

func TestFindConcurrentPasses_FuturePassInWindow(t *testing.T) {
	now := time.Now().UTC()
	// Пролёт начнётся через 3 мин, окно 5 мин → должен попасть
	passes := []*tracker.Pass{
		makePass(2, "SAT-B", 3*time.Minute, 8*time.Minute),
	}
	result := FindConcurrentPasses(passes, now, 5*time.Minute)
	if len(result) != 1 {
		t.Fatalf("expected 1 pass, got %d", len(result))
	}
	if result[0].IsVisible {
		t.Error("future pass should not be visible yet")
	}
}

func TestFindConcurrentPasses_FuturePassOutsideWindow(t *testing.T) {
	now := time.Now().UTC()
	// Пролёт начнётся через 10 мин, окно 5 мин → не должен попасть
	passes := []*tracker.Pass{
		makePass(3, "SAT-C", 10*time.Minute, 15*time.Minute),
	}
	result := FindConcurrentPasses(passes, now, 5*time.Minute)
	if len(result) != 0 {
		t.Fatalf("expected 0 passes, got %d", len(result))
	}
}

func TestFindConcurrentPasses_ExpiredPass(t *testing.T) {
	now := time.Now().UTC()
	// Пролёт завершился 1 мин назад → не должен попасть
	passes := []*tracker.Pass{
		makePass(4, "SAT-D", -10*time.Minute, -1*time.Minute),
	}
	result := FindConcurrentPasses(passes, now, 5*time.Minute)
	if len(result) != 0 {
		t.Fatalf("expected 0 passes, got %d", len(result))
	}
}

func TestFindConcurrentPasses_SortedByAOS(t *testing.T) {
	now := time.Now().UTC()
	passes := []*tracker.Pass{
		makePass(5, "SAT-E", 4*time.Minute, 9*time.Minute),  // AOS позже
		makePass(6, "SAT-F", 1*time.Minute, 6*time.Minute),  // AOS раньше
		makePass(7, "SAT-G", 2*time.Minute, 7*time.Minute),
	}
	result := FindConcurrentPasses(passes, now, 5*time.Minute)
	if len(result) != 3 {
		t.Fatalf("expected 3 passes, got %d", len(result))
	}
	// Должны быть отсортированы по AOS.
	for i := 1; i < len(result); i++ {
		if result[i].Pass.AOS < result[i-1].Pass.AOS {
			t.Errorf("pass %d has AOS < pass %d (not sorted)", i, i-1)
		}
	}
}

func TestFindConcurrentPasses_MultipleInWindow(t *testing.T) {
	now := time.Now().UTC()
	passes := []*tracker.Pass{
		makePass(10, "SAT-A", -1*time.Minute, 5*time.Minute),  // активный
		makePass(11, "SAT-B", 2*time.Minute, 7*time.Minute),   // предстоящий в окне
		makePass(12, "SAT-C", 6*time.Minute, 11*time.Minute),  // вне окна
	}
	result := FindConcurrentPasses(passes, now, 5*time.Minute)
	if len(result) != 2 {
		t.Fatalf("expected 2 passes in window, got %d", len(result))
	}
}

func TestFindConcurrentPasses_StableSortByNoradID(t *testing.T) {
	now := time.Now().UTC()
	// Два пролёта с одинаковым AOS — должна быть стабильная сортировка по NoradID.
	sameAOS := msFromNow(2 * time.Minute)
	passes := []*tracker.Pass{
		{NoradID: 200, SatName: "SAT-B", AOS: sameAOS, LOS: msFromNow(7 * time.Minute), Duration: 300},
		{NoradID: 100, SatName: "SAT-A", AOS: sameAOS, LOS: msFromNow(7 * time.Minute), Duration: 300},
	}
	result := FindConcurrentPasses(passes, now, 5*time.Minute)
	if len(result) != 2 {
		t.Fatalf("expected 2 passes, got %d", len(result))
	}
	if result[0].NoradID != 100 {
		t.Errorf("expected NoradID 100 first (stable sort), got %d", result[0].NoradID)
	}
}

func TestNoradInPostLosGap(t *testing.T) {
	now := time.Now().UTC()
	t.Run("future_only", func(t *testing.T) {
		passes := []*tracker.Pass{makePass(1, "A", 10*time.Minute, 15*time.Minute)}
		if NoradInPostLosGap(passes, 1, now) {
			t.Error("expected false when only future passes in data (pre-AOS wait)")
		}
	})
	t.Run("after_los_before_next_aos", func(t *testing.T) {
		passes := []*tracker.Pass{
			makePass(1, "A", -20*time.Minute, -5*time.Minute),
			makePass(1, "A", 30*time.Minute, 40*time.Minute),
		}
		if !NoradInPostLosGap(passes, 1, now) {
			t.Error("expected true in gap after LOS")
		}
	})
	t.Run("active_pass", func(t *testing.T) {
		passes := []*tracker.Pass{makePass(1, "A", -2*time.Minute, 8*time.Minute)}
		if NoradInPostLosGap(passes, 1, now) {
			t.Error("expected false during active pass")
		}
	})
}

// ── SelectPrimarySatellite ────────────────────────────────────────────────────

func TestSelectPrimary_EmptyGroup(t *testing.T) {
	id := SelectPrimarySatellite(nil, nil, time.Now().UTC())
	if id != 0 {
		t.Errorf("expected 0 for empty group, got %d", id)
	}
}

func TestSelectPrimary_SingleVisible(t *testing.T) {
	now := time.Now().UTC()
	sats := []PassInfo{
		{NoradID: 10, IsVisible: true, Pass: tracker.Pass{LOS: msFromNow(5 * time.Minute), Duration: 300}},
	}
	id := SelectPrimarySatellite(sats, nil, now)
	if id != 10 {
		t.Errorf("expected 10, got %d", id)
	}
}

func TestSelectPrimary_MaxRemainingTime(t *testing.T) {
	now := time.Now().UTC()
	sats := []PassInfo{
		{NoradID: 10, IsVisible: true, Pass: tracker.Pass{LOS: msFromNow(3 * time.Minute)}},
		{NoradID: 20, IsVisible: true, Pass: tracker.Pass{LOS: msFromNow(10 * time.Minute)}},
		{NoradID: 30, IsVisible: true, Pass: tracker.Pass{LOS: msFromNow(5 * time.Minute)}},
	}
	id := SelectPrimarySatellite(sats, nil, now)
	if id != 20 {
		t.Errorf("expected 20 (max remaining), got %d", id)
	}
}

func TestSelectPrimary_TieBreakerNoradID(t *testing.T) {
	now := time.Now().UTC()
	sameLOS := msFromNow(10 * time.Minute)
	sats := []PassInfo{
		{NoradID: 30, IsVisible: true, Pass: tracker.Pass{LOS: sameLOS}},
		{NoradID: 10, IsVisible: true, Pass: tracker.Pass{LOS: sameLOS}},
		{NoradID: 20, IsVisible: true, Pass: tracker.Pass{LOS: sameLOS}},
	}
	id := SelectPrimarySatellite(sats, nil, now)
	if id != 10 {
		t.Errorf("expected 10 (smallest NoradID), got %d", id)
	}
}

func TestSelectPrimary_FallbackMaxRemainingWhenNoneVisible(t *testing.T) {
	now := time.Now().UTC()
	// Нет видимых → максимальное оставшееся время до LOS.
	sats := []PassInfo{
		{NoradID: 5, IsVisible: false, Pass: tracker.Pass{AOS: msFromNow(1 * time.Minute), LOS: msFromNow(3 * time.Minute)}},
		{NoradID: 6, IsVisible: false, Pass: tracker.Pass{AOS: msFromNow(2 * time.Minute), LOS: msFromNow(12 * time.Minute)}},
	}
	id := SelectPrimarySatellite(sats, nil, now)
	if id != 6 {
		t.Errorf("expected 6 (max remaining in group), got %d", id)
	}
}

func TestSelectPrimary_ManualSelection(t *testing.T) {
	now := time.Now().UTC()
	sats := []PassInfo{
		{NoradID: 10, IsVisible: true, Pass: tracker.Pass{LOS: msFromNow(10 * time.Minute)}},
		{NoradID: 20, IsVisible: true, Pass: tracker.Pass{LOS: msFromNow(2 * time.Minute)}},
	}
	manual := 20
	id := SelectPrimarySatellite(sats, &manual, now)
	if id != 20 {
		t.Errorf("expected manual selection 20, got %d", id)
	}
}

func TestSelectPrimary_ManualSelectionExpired(t *testing.T) {
	now := time.Now().UTC()
	// Ручной выбор 99 отсутствует в группе → fallback.
	sats := []PassInfo{
		{NoradID: 10, IsVisible: true, Pass: tracker.Pass{LOS: msFromNow(5 * time.Minute)}},
	}
	manual := 99
	id := SelectPrimarySatellite(sats, &manual, now)
	if id != 10 {
		t.Errorf("expected fallback to 10, got %d", id)
	}
}

// ── ShouldSwitchPrimary ───────────────────────────────────────────────────────

func TestShouldSwitch_EmptyGroup(t *testing.T) {
	sw, _ := ShouldSwitchPrimary(10, nil, time.Now().UTC())
	if sw {
		t.Error("should not switch for empty group")
	}
}

func TestShouldSwitch_ActivePassContinues(t *testing.T) {
	now := time.Now().UTC()
	sats := []PassInfo{
		{
			NoradID:   10,
			IsVisible: true,
			Pass:      tracker.Pass{AOS: msFromNow(-1 * time.Minute), LOS: msFromNow(4 * time.Minute)},
		},
	}
	sw, _ := ShouldSwitchPrimary(10, sats, now)
	if sw {
		t.Error("should not switch: pass still active")
	}
}

func TestShouldSwitch_PassEnded(t *testing.T) {
	// В реальном сценарии FindConcurrentPasses уже удаляет завершённые пролёты (LOS <= now)
	// из группы. Поэтому satellite 10 (чей пролёт закончился) отсутствует в satellites.
	// ShouldSwitchPrimary должна переключить primary на satellite 20, когда 10 не найден в группе.
	now := time.Now().UTC()
	sats := []PassInfo{
		{
			NoradID:   20,
			IsVisible: false,
			Pass:      tracker.Pass{AOS: msFromNow(1 * time.Minute), LOS: msFromNow(6 * time.Minute)},
		},
	}
	// Primary — 10, но его нет в группе (пролёт завершился, убран FindConcurrentPasses).
	sw, newID := ShouldSwitchPrimary(10, sats, now)
	if !sw {
		t.Error("should switch: primary not in group (pass ended)")
	}
	if newID != 20 {
		t.Errorf("expected new primary 20, got %d", newID)
	}
}

func TestShouldSwitch_PrimaryNotInGroup(t *testing.T) {
	// Primary 99 не в группе → переключиться на 10.
	now := time.Now().UTC()
	sats := []PassInfo{
		{
			NoradID: 10,
			Pass:    tracker.Pass{AOS: msFromNow(1 * time.Minute), LOS: msFromNow(6 * time.Minute)},
		},
	}
	sw, newID := ShouldSwitchPrimary(99, sats, now)
	if !sw {
		t.Error("should switch: primary not in group")
	}
	if newID != 10 {
		t.Errorf("expected new primary 10, got %d", newID)
	}
}

// ── GroupChanged ──────────────────────────────────────────────────────────────

func TestGroupChanged_NilAndEmpty(t *testing.T) {
	if GroupChanged(nil, nil) {
		t.Error("nil vs nil should not be changed")
	}
	if !GroupChanged(nil, []int{1}) {
		t.Error("nil vs [1] should be changed")
	}
}

func TestGroupChanged_SameIDs(t *testing.T) {
	a := []int{1, 2, 3}
	b := []int{1, 2, 3}
	if GroupChanged(a, b) {
		t.Error("same IDs should not be changed")
	}
}

func TestGroupChanged_DifferentIDs(t *testing.T) {
	a := []int{1, 2}
	b := []int{1, 3}
	if !GroupChanged(a, b) {
		t.Error("different IDs should be changed")
	}
}

// ── BuildConcurrentPassGroup ──────────────────────────────────────────────────

func TestBuildGroup_SetsActiveFlag(t *testing.T) {
	sats := []PassInfo{
		{NoradID: 10, Pass: tracker.Pass{AOS: 1000, LOS: 2000}},
		{NoradID: 20, Pass: tracker.Pass{AOS: 1100, LOS: 2100}},
	}
	group := BuildConcurrentPassGroup(sats, 10)

	if group.PrimarySatID != 10 {
		t.Errorf("expected primary 10, got %d", group.PrimarySatID)
	}
	for _, s := range group.Satellites {
		if s.NoradID == 10 && !s.IsActive {
			t.Error("primary should have IsActive=true")
		}
		if s.NoradID == 20 && s.IsActive {
			t.Error("secondary should have IsActive=false")
		}
	}
}

func TestBuildGroup_TimeWindow(t *testing.T) {
	sats := []PassInfo{
		{NoradID: 10, Pass: tracker.Pass{AOS: 1000, LOS: 3000}},
		{NoradID: 20, Pass: tracker.Pass{AOS: 500, LOS: 2500}},
	}
	group := BuildConcurrentPassGroup(sats, 10)

	if group.TimeWindow.Start != 500 {
		t.Errorf("expected window.Start=500, got %d", group.TimeWindow.Start)
	}
	if group.TimeWindow.End != 3000 {
		t.Errorf("expected window.End=3000, got %d", group.TimeWindow.End)
	}
}

// ── [BUG-A] Преждевременная смена primary ─────────────────────────────────────
// SelectPrimarySatellite не должен перебивать текущий primary,
// пока ShouldSwitchPrimary говорит «не переключай».

func TestShouldSwitch_SwitchToLongerRemainingTime(t *testing.T) {
	now := time.Now().UTC()
	// Спутник A — primary, пролёт ещё активен, LOS через 3 мин.
	// Спутник B — в группе, тоже видим, LOS через 13 мин (больше оставшегося времени).
	// ShouldSwitchPrimary(A) должен переключить на B — у него больше оставшегося времени.
	sats := []PassInfo{
		{
			NoradID:   10,
			IsVisible: true,
			Pass:      tracker.Pass{AOS: msFromNow(-5 * time.Minute), LOS: msFromNow(3 * time.Minute), Duration: 480},
		},
		{
			NoradID:   20,
			IsVisible: true,
			Pass:      tracker.Pass{AOS: msFromNow(-2 * time.Minute), LOS: msFromNow(13 * time.Minute), Duration: 900},
		},
	}
	sw, newID := ShouldSwitchPrimary(10, sats, now)
	if !sw {
		t.Error("should switch: satellite 20 has more remaining time than primary 10")
	}
	if newID != 20 {
		t.Errorf("expected switch to 20 (max remaining), got %d", newID)
	}
}

func TestShouldSwitch_NoSwitchWhenCurrentIsBest(t *testing.T) {
	now := time.Now().UTC()
	// Спутник A — primary, LOS через 10 мин. Спутник B — LOS через 3 мин.
	// Текущий primary — лучший, не переключаемся.
	sats := []PassInfo{
		{
			NoradID:   10,
			IsVisible: true,
			Pass:      tracker.Pass{AOS: msFromNow(-2 * time.Minute), LOS: msFromNow(10 * time.Minute)},
		},
		{
			NoradID:   20,
			IsVisible: true,
			Pass:      tracker.Pass{AOS: msFromNow(-1 * time.Minute), LOS: msFromNow(3 * time.Minute)},
		},
	}
	sw, _ := ShouldSwitchPrimary(10, sats, now)
	if sw {
		t.Error("should NOT switch: primary 10 has the most remaining time")
	}
}

func TestShouldSwitch_SwitchAfterLOSEvenToSameNorad(t *testing.T) {
	now := time.Now().UTC()
	// Спутник 10 — primary, но это подстановка следующего витка (AOS далеко в будущем).
	// IsSubstitute=true: пролёт закончился, запись взята из следующего витка.
	sats := []PassInfo{
		{
			NoradID:      10,
			IsVisible:    false,
			IsSubstitute: true,
			Pass:         tracker.Pass{AOS: msFromNow(80 * time.Minute), LOS: msFromNow(90 * time.Minute), Duration: 600},
		},
	}
	sw, newID := ShouldSwitchPrimary(10, sats, now)
	if !sw {
		t.Error("should switch: satellite 10 is a substitute (future orbit), previous pass already ended")
	}
	if newID != 10 {
		t.Errorf("expected newID=10 (re-selected), got %d", newID)
	}
}

func TestShouldSwitch_KeepPrimaryWaitingForAOS(t *testing.T) {
	now := time.Now().UTC()
	// Спутник 10 — primary, ещё не виден, AOS через 30 секунд. Не переключать.
	sats := []PassInfo{
		{
			NoradID:   10,
			IsVisible: false,
			Pass:      tracker.Pass{AOS: msFromNow(30 * time.Second), LOS: msFromNow(6 * time.Minute), Duration: 330},
		},
	}
	sw, _ := ShouldSwitchPrimary(10, sats, now)
	if sw {
		t.Error("should NOT switch: satellite is waiting for AOS (in window, not visible yet)")
	}
}

// ── [BUG-B] GroupEntries change detection ─────────────────────────────────────

func TestGroupEntries_IdenticalGroups(t *testing.T) {
	sats := []PassInfo{
		{NoradID: 10, IsVisible: true, Pass: tracker.Pass{AOS: 1000, LOS: 2000}},
		{NoradID: 20, IsVisible: false, Pass: tracker.Pass{AOS: 3000, LOS: 4000}},
	}
	a := GroupEntries(sats)
	b := GroupEntries(sats)
	if GroupEntriesChanged(a, b) {
		t.Error("identical groups should not be detected as changed")
	}
}

func TestGroupEntries_VisibilityTransition(t *testing.T) {
	// Тот же NORAD ID, но IsVisible изменился (visible→invisible при LOS).
	old := []GroupEntry{
		{NoradID: 10, IsVisible: true, AOS: 1000, LOS: 2000},
	}
	new := []GroupEntry{
		{NoradID: 10, IsVisible: false, AOS: 1000, LOS: 2000},
	}
	if !GroupEntriesChanged(old, new) {
		t.Error("visibility transition (true→false) must be detected as change")
	}
}

func TestGroupEntries_SameNoradDifferentPass(t *testing.T) {
	// Тот же NORAD, но другой пролёт (следующий виток — другие AOS/LOS).
	old := []GroupEntry{
		{NoradID: 10, IsVisible: true, AOS: 1000, LOS: 2000},
	}
	new := []GroupEntry{
		{NoradID: 10, IsVisible: false, AOS: 100000, LOS: 200000},
	}
	if !GroupEntriesChanged(old, new) {
		t.Error("same NORAD with different pass (AOS/LOS) must be detected as change")
	}
}

func TestGroupEntries_DifferentSize(t *testing.T) {
	old := []GroupEntry{
		{NoradID: 10, IsVisible: true, AOS: 1000, LOS: 2000},
	}
	new := []GroupEntry{
		{NoradID: 10, IsVisible: true, AOS: 1000, LOS: 2000},
		{NoradID: 20, IsVisible: false, AOS: 3000, LOS: 4000},
	}
	if !GroupEntriesChanged(old, new) {
		t.Error("different group size must be detected as change")
	}
}

func TestGroupEntries_NilVsNil(t *testing.T) {
	if GroupEntriesChanged(nil, nil) {
		t.Error("nil vs nil should not be changed")
	}
}

func TestGroupEntries_SortedByNoradID(t *testing.T) {
	sats := []PassInfo{
		{NoradID: 30, IsVisible: true, Pass: tracker.Pass{AOS: 3000, LOS: 4000}},
		{NoradID: 10, IsVisible: true, Pass: tracker.Pass{AOS: 1000, LOS: 2000}},
	}
	entries := GroupEntries(sats)
	if entries[0].NoradID != 10 || entries[1].NoradID != 30 {
		t.Errorf("entries should be sorted by NoradID, got [%d, %d]", entries[0].NoradID, entries[1].NoradID)
	}
}

// ── [BUG-C] ShouldSwitchPrimary vs подстановка следующего витка ───────────────

func TestShouldSwitch_SubstituteFuturePassSameNorad(t *testing.T) {
	now := time.Now().UTC()
	// Окно пусто → подставлен следующий виток спутника 10 (AOS через 80 мин).
	// IsSubstitute=true: пролёт НЕ из скользящего окна.
	sats := []PassInfo{
		{
			NoradID:      10,
			IsVisible:    false,
			IsSubstitute: true,
			Pass:         tracker.Pass{AOS: msFromNow(80 * time.Minute), LOS: msFromNow(90 * time.Minute), Duration: 600},
		},
	}
	sw, _ := ShouldSwitchPrimary(10, sats, now)
	if !sw {
		t.Error("should switch: substitute pass (next orbit) is not the same as the ended pass")
	}
}

func TestShouldSwitch_SwitchToBetterCandidateAmongMultiple(t *testing.T) {
	now := time.Now().UTC()
	sats := []PassInfo{
		{
			NoradID:   10,
			IsVisible: true,
			Pass:      tracker.Pass{AOS: msFromNow(-3 * time.Minute), LOS: msFromNow(5 * time.Minute)},
		},
		{
			NoradID:   20,
			IsVisible: true,
			Pass:      tracker.Pass{AOS: msFromNow(-1 * time.Minute), LOS: msFromNow(12 * time.Minute)},
		},
		{
			NoradID:   30,
			IsVisible: false,
			Pass:      tracker.Pass{AOS: msFromNow(30 * time.Second), LOS: msFromNow(8 * time.Minute)},
		},
	}
	// Primary=10 виден, но у 20 больше оставшегося времени → переключиться на 20.
	sw, newID := ShouldSwitchPrimary(10, sats, now)
	if !sw {
		t.Error("should switch: satellite 20 has more remaining time than primary 10")
	}
	if newID != 20 {
		t.Errorf("expected switch to 20, got %d", newID)
	}
}

// ── Интеграционный тест: полный цикл пролёта ─────────────────────────────────

func TestFullPassLifecycle_GroupEntriesDetectAllTransitions(t *testing.T) {
	// Симуляция жизненного цикла:
	// 1. Ожидание AOS (IsVisible=false, AOS в будущем)
	// 2. Активный пролёт (IsVisible=true)
	// 3. Пролёт завершён (выпадает из окна, подстановка следующего витка)

	phase1 := []GroupEntry{
		{NoradID: 10, IsVisible: false, AOS: 1000, LOS: 2000},
	}
	phase2 := []GroupEntry{
		{NoradID: 10, IsVisible: true, AOS: 1000, LOS: 2000},
	}
	phase3 := []GroupEntry{
		{NoradID: 10, IsVisible: false, AOS: 100000, LOS: 200000},
	}

	if !GroupEntriesChanged(phase1, phase2) {
		t.Error("phase1→phase2: visibility transition must be detected")
	}
	if !GroupEntriesChanged(phase2, phase3) {
		t.Error("phase2→phase3: pass change (AOS/LOS + visibility) must be detected")
	}
	if !GroupEntriesChanged(phase1, phase3) {
		t.Error("phase1→phase3: different pass data must be detected")
	}
}
