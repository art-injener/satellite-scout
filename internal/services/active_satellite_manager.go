package services

import (
	"sort"
	"time"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// Параметры скользящего окна по умолчанию.
const (
	// Окно вперёд: пролёты с AOS < now + DefaultWindowForward попадают в группу.
	DefaultWindowForward = 1 * time.Minute

	// Горизонт расчёта пролётов для кеша группы.
	DefaultGroupPassHorizon = 1 * time.Hour
)

// PassInfo — информация об одном спутнике в группе скользящего окна.
type PassInfo struct {
	// NORAD ID спутника.
	NoradID int
	// Название спутника.
	SatName string
	// Данные пролёта (AOS, TCA, LOS, Duration, SkyPath и т.д.).
	Pass tracker.Pass
	// IsActive — спутник является активным (primary) в данный момент.
	IsActive bool
	// IsVisible — спутник находится над горизонтом (El > 0°) в данный момент.
	IsVisible bool
}

// TimeWindow — временное окно группы.
type TimeWindow struct {
	// Start — начало окна (earliest AOS в группе, Unix ms).
	Start int64
	// End — конец окна (latest LOS в группе, Unix ms).
	End int64
}

// ConcurrentPassGroup — группа спутников, наблюдаемых одновременно в скользящем окне.
type ConcurrentPassGroup struct {
	// TimeWindow — временной диапазон от min(AOS) до max(LOS) всех спутников в группе.
	TimeWindow TimeWindow
	// Satellites — список спутников в группе, отсортированных по AOS.
	Satellites []PassInfo
	// PrimarySatID — NORAD ID активного (primary) спутника.
	PrimarySatID int
}

// FindConcurrentPasses фильтрует кешированный список пролётов по скользящему окну.
//
// Окно: [now, now + windowForward].
// В группу попадают пролёты с AOS < now + windowForward && LOS > now
// (условие пересечения интервала [AOS, LOS] с [now, now + windowForward]).
//
// Никаких вызовов SGP4 или PassPredictor — только фильтрация по времени.
// Результат отсортирован по AOS, при равенстве — по NoradID (стабильный порядок).
func FindConcurrentPasses(passes []*tracker.Pass, now time.Time, windowForward time.Duration) []PassInfo {
	windowStartMs := now.UnixMilli()
	windowEndMs := now.Add(windowForward).UnixMilli()

	var result []PassInfo
	for _, p := range passes {
		// Пролёт пересекается с окном: AOS < windowEnd && LOS > windowStart.
		if p.AOS < windowEndMs && p.LOS > windowStartMs {
			result = append(result, PassInfo{
				NoradID:   p.NoradID,
				SatName:   p.SatName,
				Pass:      *p,
				IsVisible: p.AOS <= windowStartMs && windowStartMs <= p.LOS,
			})
		}
	}

	// Сортировка по AOS, затем по NoradID для стабильного порядка.
	sort.Slice(result, func(i, j int) bool {
		if result[i].Pass.AOS == result[j].Pass.AOS {
			return result[i].NoradID < result[j].NoradID
		}
		return result[i].Pass.AOS < result[j].Pass.AOS
	})

	return result
}

// SelectPrimarySatellite выбирает активный (primary) спутник из группы.
//
// Логика:
//  1. Если userSelection != nil и спутник есть в группе → вернуть его (ручной выбор).
//  2. Среди видимых (IsVisible = true) → спутник с наибольшей длительностью пролёта.
//  3. Если видимых нет → спутник с минимальным AOS (ближайший к наблюдению).
//  4. При равной длительности/AOS — меньший NoradID (детерминированный tie-breaker).
//
// Возвращает 0, если группа пустая.
func SelectPrimarySatellite(satellites []PassInfo, userSelection *int) int {
	if len(satellites) == 0 {
		return 0
	}

	// Ручной выбор: проверяем, что спутник ещё в группе.
	if userSelection != nil {
		for _, s := range satellites {
			if s.NoradID == *userSelection {
				return *userSelection
			}
		}
		// Спутник вышел из окна — сбрасываем ручной выбор (caller должен обнулить userSelection).
	}

	// Среди видимых — выбираем с максимальной длительностью.
	var best *PassInfo
	for i := range satellites {
		s := &satellites[i]
		if !s.IsVisible {
			continue
		}
		if best == nil ||
			s.Pass.Duration > best.Pass.Duration ||
			(s.Pass.Duration == best.Pass.Duration && s.NoradID < best.NoradID) {
			best = s
		}
	}
	if best != nil {
		return best.NoradID
	}

	// Нет видимых — ближайший по AOS (список уже отсортирован).
	return satellites[0].NoradID
}

// ShouldSwitchPrimary проверяет, нужно ли переключить primary спутник.
//
// Возвращает (true, newID) если нужно переключение:
//   - текущий primary больше не в группе, или
//   - его пролёт завершился (now > LOS).
//
// Возвращает (false, 0) если переключение не нужно.
func ShouldSwitchPrimary(currentPrimaryID int, satellites []PassInfo, now time.Time) (shouldSwitch bool, newPrimaryID int) {
	if len(satellites) == 0 {
		return false, 0
	}

	nowMs := now.UnixMilli()

	// Ищем текущий primary в группе.
	for _, s := range satellites {
		if s.NoradID == currentPrimaryID {
			if nowMs <= s.Pass.LOS {
				// Пролёт ещё идёт или ещё не начался — не переключаемся.
				return false, 0
			}
			// Пролёт завершился — переключаемся.
			break
		}
	}

	// Primary не найден в группе или его пролёт завершился → выбираем нового.
	newID := SelectPrimarySatellite(satellites, nil)
	if newID == currentPrimaryID {
		return false, 0
	}
	return true, newID
}

// GroupIDs возвращает отсортированный список NORAD ID из группы.
// Используется для сравнения групп на изменение (change detection).
func GroupIDs(satellites []PassInfo) []int {
	ids := make([]int, len(satellites))
	for i, s := range satellites {
		ids[i] = s.NoradID
	}
	sort.Ints(ids)
	return ids
}

// GroupChanged возвращает true, если состав группы изменился.
// Сравнивает два отсортированных слайса ID.
func GroupChanged(oldIDs, newIDs []int) bool {
	if len(oldIDs) != len(newIDs) {
		return true
	}
	for i := range oldIDs {
		if oldIDs[i] != newIDs[i] {
			return true
		}
	}
	return false
}

// findNearestFuturePass ищет ближайший пролёт с AOS > now среди всех пролётов.
// Используется как fallback когда скользящее окно пустое.
// Возвращает nil если пролётов нет.
func findNearestFuturePass(passes []*tracker.Pass, now time.Time) *tracker.Pass {
	nowMs := now.UnixMilli()
	var nearest *tracker.Pass
	for _, p := range passes {
		if p == nil || p.AOS <= nowMs {
			continue
		}
		if nearest == nil || p.AOS < nearest.AOS {
			nearest = p
		}
	}
	return nearest
}

// BuildConcurrentPassGroup строит ConcurrentPassGroup из отфильтрованного списка PassInfo.
// Устанавливает TimeWindow, PrimarySatID и флаг IsActive для каждого спутника.
func BuildConcurrentPassGroup(satellites []PassInfo, primaryID int) ConcurrentPassGroup {
	if len(satellites) == 0 {
		return ConcurrentPassGroup{}
	}

	// Отмечаем активного спутника и вычисляем TimeWindow.
	var minAOS, maxLOS int64
	updated := make([]PassInfo, len(satellites))
	for i, s := range satellites {
		s.IsActive = (s.NoradID == primaryID)
		updated[i] = s

		if i == 0 || s.Pass.AOS < minAOS {
			minAOS = s.Pass.AOS
		}
		if i == 0 || s.Pass.LOS > maxLOS {
			maxLOS = s.Pass.LOS
		}
	}

	return ConcurrentPassGroup{
		TimeWindow:   TimeWindow{Start: minAOS, End: maxLOS},
		Satellites:   updated,
		PrimarySatID: primaryID,
	}
}
