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
	// IsSubstitute — запись подставлена как ближайший будущий пролёт (окно было пустым).
	// Такой спутник НЕ находится в скользящем окне, AOS далеко в будущем.
	IsSubstitute bool
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

// NoradInPostLosGap — нет активного пролёта (AOS≤now≤LOS), но в данных есть пролёт с LOS < now.
// Отличает зазор «после окончания сеанса» от сценария «до первого AOS» (все пролёты ещё в будущем).
func NoradInPostLosGap(passes []*tracker.Pass, norad int, now time.Time) bool {
	t := now.UnixMilli()
	var lastPastLOS int64
	for _, p := range passes {
		if p.NoradID != norad {
			continue
		}
		if p.AOS <= t && t <= p.LOS {
			return false
		}
		if p.LOS < t && p.LOS > lastPastLOS {
			lastPastLOS = p.LOS
		}
	}
	return lastPastLOS > 0
}

// SelectPrimarySatellite выбирает активный (primary) спутник из группы.
//
// Логика:
//  1. Если userSelection != nil и спутник есть в группе → вернуть его (ручной выбор).
//  2. Среди видимых (IsVisible = true) → спутник с максимальным оставшимся временем (LOS − now).
//     Это гарантирует, что primary — спутник с самым длинным окном связи от текущего момента.
//  3. Если видимых нет → среди всей группы наибольшее оставшееся время до LOS.
//  4. При равном остатке — меньший NoradID (детерминированный tie-breaker).
//
// Возвращает 0, если группа пустая.
func SelectPrimarySatellite(satellites []PassInfo, userSelection *int, now time.Time) int {
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

	nowMs := now.UnixMilli()

	// Среди видимых — выбираем с максимальным оставшимся временем (LOS − now).
	var best *PassInfo
	var bestRemaining int64
	for i := range satellites {
		s := &satellites[i]
		if !s.IsVisible {
			continue
		}
		rem := s.Pass.LOS - nowMs
		if best == nil || rem > bestRemaining || (rem == bestRemaining && s.NoradID < best.NoradID) {
			best = s
			bestRemaining = rem
		}
	}
	if best != nil {
		return best.NoradID
	}

	// Нет видимых — максимальное оставшееся время до LOS среди всей группы.
	best = nil
	bestRemaining = 0
	for i := range satellites {
		s := &satellites[i]
		rem := s.Pass.LOS - nowMs
		if best == nil || rem > bestRemaining || (rem == bestRemaining && s.NoradID < best.NoradID) {
			best = s
			bestRemaining = rem
		}
	}
	if best != nil {
		return best.NoradID
	}
	return 0
}

// ShouldSwitchPrimary проверяет, нужно ли переключить primary спутник.
//
// Возвращает (shouldSwitch=true, newID) если нужно переключение:
//   - текущий primary больше не в группе, или
//   - его пролёт завершился (now > LOS), или
//   - появился видимый спутник с большим оставшимся временем (LOS − now).
//
// Возвращает (shouldSwitch=false, 0) если текущий primary — лучший кандидат.
//
// ВАЖНО: когда пролёт завершился, возвращает shouldSwitch=true даже если
// новый primary — тот же NORAD ID (следующий виток). Это нужно, чтобы
// caller обнаружил переход и обновил данные пролёта (AOS/LOS/SkyPath).
func ShouldSwitchPrimary(currentPrimaryID int, satellites []PassInfo, now time.Time) (shouldSwitch bool, newPrimaryID int) {
	if len(satellites) == 0 {
		return false, 0
	}

	nowMs := now.UnixMilli()
	// Ищем текущий primary в группе.
	for _, s := range satellites {
		if s.NoradID == currentPrimaryID {
			if s.IsVisible && nowMs <= s.Pass.LOS {
				// Пролёт активен — проверяем, нет ли лучшего кандидата по оставшемуся времени.
				bestID := SelectPrimarySatellite(satellites, nil, now)
				if bestID != 0 && bestID != currentPrimaryID {
					return true, bestID
				}
				return false, 0
			}
			if !s.IsVisible && !s.IsSubstitute && nowMs < s.Pass.AOS {
				// Пролёт в скользящем окне, AOS ещё впереди — ожидание начала сеанса.
				// Подстановки (IsSubstitute=true) сюда НЕ попадают — для них пролёт
				// уже завершился, а запись взята из следующего витка.
				return false, 0
			}
			// Пролёт завершился, спутник подставлен из будущего витка, или вышел из видимости → переключаемся.
			break
		}
	}

	// Primary не найден в группе или его пролёт завершился → выбираем нового.
	newID := SelectPrimarySatellite(satellites, nil, now)
	return true, newID
}

// GroupEntry — элемент для change detection: NORAD ID + видимость + границы пролёта.
// Сравнение по (NoradID, IsVisible, AOS, LOS) позволяет обнаружить:
//   - смену состава группы (добавление/удаление спутника)
//   - переход видимости (visible→invisible при LOS, invisible→visible при AOS)
//   - замену пролёта (тот же спутник, но другой виток с новыми AOS/LOS)
type GroupEntry struct {
	NoradID   int
	IsVisible bool
	AOS       int64
	LOS       int64
}

// GroupEntries строит отсортированный по NoradID слайс GroupEntry из группы.
func GroupEntries(satellites []PassInfo) []GroupEntry {
	entries := make([]GroupEntry, len(satellites))
	for i, s := range satellites {
		entries[i] = GroupEntry{
			NoradID:   s.NoradID,
			IsVisible: s.IsVisible,
			AOS:       s.Pass.AOS,
			LOS:       s.Pass.LOS,
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].NoradID < entries[j].NoradID
	})
	return entries
}

// GroupEntriesChanged возвращает true, если данные группы изменились.
func GroupEntriesChanged(oldEntries, newEntries []GroupEntry) bool {
	if len(oldEntries) != len(newEntries) {
		return true
	}
	for i := range oldEntries {
		if oldEntries[i] != newEntries[i] {
			return true
		}
	}
	return false
}

// GroupIDs возвращает отсортированный список NORAD ID из группы.
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
