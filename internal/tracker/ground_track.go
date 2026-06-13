package tracker

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Ошибки генерации наземной трассы спутника.
var (
	ErrNilTLEForTrack = errors.New("TLE is nil")
	ErrInvalidRange   = errors.New("invalid time range: start equals end")
)

// Порог для определения геостационарной орбиты (GEO).
const geoMeanMotionThreshold = 0.1

// Порог скачка долготы для определения пересечения антимеридиана (градусы).
const antimeridianThreshold = 270.0

// TrackPoint — точка наземной трассы спутника (координаты в градусах, готово для JSON/UI).
type TrackPoint struct {
	Lon float64 `json:"lon"` // Долгота, градусы (-180..+180).
	Lat float64 `json:"lat"` // Широта, градусы (-90..+90).
	TS  int64   `json:"ts"`  // Unix timestamp, миллисекунды.
}

// GroundTrack — полная трасса орбиты, разбитая на пройденный/предстоящий участки и сегменты по антимеридиану.
// Готова для прямой сериализации в JSON и отдачи на фронтенд без обработки.
type GroundTrack struct {
	Past    [][]TrackPoint `json:"past"`     // Пройденный участок трассы (сегменты, разбитые по антимеридиану).
	Future  [][]TrackPoint `json:"future"`   // Предстоящий участок трассы (сегменты, разбитые по антимеридиану).
	NoradID int            `json:"norad_id"` // NORAD ID спутника.
}

// Points возвращает все точки плоским массивом (пройденный + предстоящий участки).
// Удобно для случаев, когда разделение на участки не нужно.
func (gt *GroundTrack) Points() []TrackPoint {
	if gt == nil {
		return nil
	}

	var result []TrackPoint

	for _, seg := range gt.Past {
		result = append(result, seg...)
	}

	for _, seg := range gt.Future {
		result = append(result, seg...)
	}

	return result
}

// TotalPoints возвращает общее количество точек.
func (gt *GroundTrack) TotalPoints() int {
	if gt == nil {
		return 0
	}

	count := 0

	for _, seg := range gt.Past {
		count += len(seg)
	}

	for _, seg := range gt.Future {
		count += len(seg)
	}

	return count
}

// IsGeostationary определяет, является ли спутник геостационарным.
// GEO спутники имеют MeanMotion ≈ 1.0 оборот/сутки (±0.1).
func IsGeostationary(tle *TLE) bool {
	if tle == nil {
		return false
	}

	return math.Abs(tle.MeanMotion-1.0) < geoMeanMotionThreshold
}

// GenerateGroundTrack генерирует наземную трассу спутника для заданного TLE и временного интервала.
// Трасса разбивается на сегменты по антимеридиану и разделяется на пройденный/предстоящий участки по текущему времени.
// step — шаг генерации точек (рекомендуется 30 сек).
func GenerateGroundTrack(tle *TLE, start, end, now time.Time, step time.Duration) (*GroundTrack, error) {
	if tle == nil {
		return nil, ErrNilTLEForTrack
	}

	if step <= 0 {
		return nil, fmt.Errorf("%w: %v", ErrInvalidStep, step)
	}

	if start.Equal(end) {
		return nil, ErrInvalidRange
	}

	// Гарантируем start < end.
	if end.Before(start) {
		start, end = end, start
	}

	// Создаём пропагатор.
	prop, err := NewPropagator(tle)
	if err != nil {
		return nil, fmt.Errorf("creating propagator: %w", err)
	}

	// Генерируем все точки трека.
	allPoints, err := generateTrackPoints(prop, start, end, step)
	if err != nil {
		return nil, err
	}

	if len(allPoints) == 0 {
		return &GroundTrack{NoradID: tle.NoradID}, nil
	}

	// Разбиваем на сегменты по антимеридиану.
	segments := splitAtAntimeridian(allPoints)

	// Разделяем на пройденный и предстоящий участки по текущему времени.
	nowMs := now.UnixMilli()
	past, future := splitPastFuture(segments, nowMs)

	return &GroundTrack{
		Past:    past,
		Future:  future,
		NoradID: tle.NoradID,
	}, nil
}

// DefaultGroundTrackStep — шаг дискретизации наземной трассы в GenerateDefaultGroundTrack.
// Должен совпадать с шагом второй пропагации для azimuth маркера на карте (см. SatelliteTrackingService),
// иначе угол иконки расходится с отрезками полилинии на canvas.
const DefaultGroundTrackStep = 30 * time.Second

// Длительность звёздных суток в минутах — за это время Земля делает полный
// оборот относительно инерциальной системы отсчёта. Используется для расчёта
// суточного смещения наземной трассы к западу: за один орбитальный период
// Земля поворачивается на 360° × period / siderealDayMinutes градусов к
// востоку, что сдвигает подспутниковую точку к западу.
const siderealDayMinutes = 1436.0681

// Запас покрытия по долготе сверх 360° — гарантирует отсутствие «дыры» на карте
// при дискретном шаге пропагации (~3.7°/30с для LEO).
const groundTrackCoverageOverlap = 1.04

// GenerateDefaultGroundTrack генерирует трассу орбиты с автодиапазоном по времени,
// рассчитанным так, чтобы трасса покрывала ровно 360° по долготе (полный обход карты)
// без избыточности.
//
// За один орбитальный период `T` спутник совершает полный виток в инерциальной
// системе отсчёта (360° по долготе), но Земля за это время поворачивается на
// 360° × T / 1436.07 минут к востоку, смещая подспутниковую точку к западу.
// Соответственно наземная трасса покрывает не полные 360°, а (360° − westingDeg).
//
// Чтобы трасса покрыла полные 360° по долготе, пропагируем
//
//	coverageMin = T × 360° / (360° − westingDeg) × overlap
//
// где overlap = 1.04 даёт ~4% запаса для гарантированного отсутствия «дыр» при
// дискретном шаге 30 с (один шаг ≈ 3.7° для LEO).
//
// Для обратной орбиты (наклонение > 90°) westingDeg отрицателен → coverageMin
// уменьшается. Для квази-геостационарных орбит (период близок к звёздным суткам)
// знаменатель стремится к нулю — фолбек: coverageMin = T × 1.05 (пять процентов запаса).
func GenerateDefaultGroundTrack(tle *TLE, now time.Time) (*GroundTrack, error) {
	if tle == nil {
		return nil, ErrNilTLEForTrack
	}

	periodMin := tle.OrbitalPeriod()
	if periodMin <= 0 {
		return nil, fmt.Errorf("%w: orbital period %.2f min", ErrInvalidRange, periodMin)
	}

	// Westing per orbital period в градусах. Положительный для prograde, отрицательный
	// для retrograde (но retrograde встречается редко и SGP4 всё равно даёт правильный
	// знак mean motion; формула универсальна).
	westingDeg := 360.0 * periodMin / siderealDayMinutes
	denom := 360.0 - westingDeg

	// Квази-GEO: westingDeg близок к 360° → знаменатель вырождается. Берём один период
	// с небольшим запасом — для GEO трасса всё равно почти точка, длина не критична.
	coverageMin := periodMin * 1.05
	if denom > 1.0 {
		coverageMin = periodMin * 360.0 / denom * groundTrackCoverageOverlap
	}

	coverage := time.Duration(coverageMin * float64(time.Minute))
	half := coverage / 2

	start := now.Add(-half)
	end := now.Add(half)

	return GenerateGroundTrack(tle, start, end, now, DefaultGroundTrackStep)
}

// Параметры алгоритма «трасса по окну долготы».
const (
	// Широта, выше которой считаем точку «околополюсной»: при пересечении полюса
	// долгота скачкообразно меняется на ~180° (артефакт equirectangular-проекции),
	// для накопления continuous lon такие шаги пропускаются.
	polepassLatDeg = 85.0
	// Time-fallback на каждое направление: даём 1.07 орбитального периода —
	// этого достаточно, чтобы пройти ровно 360° по continuous lon (один виток ≈
	// 360°−westingDeg ≈ 336°, плюс 7% запаса). Если sat смещён от observer на ±α°
	// (внутри окна), forward direction должен пройти (180°−α) до правой границы,
	// backward (180°+α) до левой; max = 360°−ε ≈ один виток. Для polar один
	// виток покрывает 360° за один период (lon-clip не срабатывает из-за
	// polepass'ов, но time-fallback останавливает в нужный момент).
	timeFallbackHalfPeriodFraction = 1.07
	// Защита от GEO/HEO: continuous lon почти не меняется, время-fallback может
	// дать тысячи точек за 1.07 виток sidereal day. Жёсткий лимит на число точек
	// в каждом направлении — 800 (≈ 400 минут × 30 с шага).
	maxPointsPerSide = 800
	// Микросдвиг от номинальной границы окна (0.01°), чтобы граничная точка
	// проектировалась на нужную сторону canvas: lon = ±180° от observerLon —
	// это один и тот же меридиан, project() выбирает левую сторону. Сдвиг на
	// ε внутрь окна делает выбор стороны однозначным (визуально незаметно).
	lonWindowBoundaryEpsilonDeg = 0.01
)

// GenerateGroundTrackByLonWindow генерирует наземную трассу так, чтобы она ровно
// укладывалась в видимое окно карты по долготе с центром в observerLon (станция
// наблюдения = центр карты).
//
// Алгоритм (4 этапа):
//
//  1. Текущая позиция КА в момент now → (satLon, satLat). Привязываем satLon к
//     окну [observerLon−180°, observerLon+180°] кратным сдвигом на ±360°.
//  2. Окно зафиксировано: leftBound = observerLon−180°, rightBound = observerLon+180°.
//  3. Пропагируем итеративно вперёд и назад от now с шагом step, накапливая
//     «continuous» longitude (delta между соседними точками; при |lat|>85°
//     polepass-фильтр пропускает шаг — там lon скачет на ~180° из-за артефакта
//     equirectangular-проекции, не реального westing).
//  4. Стопаем по двум критериям (что наступит раньше):
//     – continuous lon вышел за границу окна → линейная интерполяция точно до
//     границы (lon-clip с интерполяцией). Это даёт «сплошную линию от края до
//     края карты» для LEO/MEO non-polar.
//     – |t−now| > period × timeFallbackHalfPeriodFraction → time-clip-fallback.
//     Защита для polar (continuous lon растёт медленно из-за polepass) и HEO
//     (continuous lon разворачивается, не достигает границы).
//
// Граничная точка получает lon = observerLon ± (180° − ε); ε = 0.01°
// гарантирует однозначную проекцию на правый/левый край canvas (избегаем
// double-mapping антимеридиана окна).
//
// Дальше — стандартное splitAtAntimeridian (гринвич-антимеридиан) и splitPastFuture.
//
//nolint:gocognit,gocyclo,funlen // lon-window clipping: forward/backward loops с pole-pass
func GenerateGroundTrackByLonWindow(
	tle *TLE,
	now time.Time,
	observerLon float64,
	step time.Duration,
) (*GroundTrack, error) {
	if tle == nil {
		return nil, ErrNilTLEForTrack
	}
	if step <= 0 {
		return nil, fmt.Errorf("%w: %v", ErrInvalidStep, step)
	}

	prop, err := NewPropagator(tle)
	if err != nil {
		return nil, fmt.Errorf("creating propagator: %w", err)
	}

	// Этап 1: текущая позиция КА.
	nowEci, err := prop.Propagate(now)
	if err != nil {
		return nil, fmt.Errorf("propagating at now: %w", err)
	}
	nowLla := ECEFToLLA(ECIToECEF(nowEci))
	satLonRaw := nowLla.LonDeg()
	satLat := nowLla.LatDeg()

	// Этап 2: привязка к окну (в continuous lon, может выйти за [-180, 180)).
	satLonAnchor := satLonRaw
	for satLonAnchor < observerLon-180.0 {
		satLonAnchor += 360.0
	}
	for satLonAnchor >= observerLon+180.0 {
		satLonAnchor -= 360.0
	}

	// Time-fallback (страховка для polar/HEO/GEO).
	periodMin := tle.OrbitalPeriod()
	if periodMin <= 0 {
		periodMin = 90.0
	}
	timeFallback := time.Duration(periodMin * timeFallbackHalfPeriodFraction * float64(time.Minute))

	nowMs := now.UnixMilli()
	nowPoint := TrackPoint{Lon: satLonRaw, Lat: satLat, TS: nowMs}

	// Границы окна по continuous lon (с микросдвигом ε внутрь, чтобы граничные
	// точки однозначно проектировались на нужный край canvas, см. doc выше).
	leftBound := observerLon - 180.0 + lonWindowBoundaryEpsilonDeg
	rightBound := observerLon + 180.0 - lonWindowBoundaryEpsilonDeg

	// normalizeLonRaw: continuous lon → raw lon ∈ [-180, 180).
	normalizeLonRaw := func(lon float64) float64 {
		y := math.Mod(lon+180.0, 360.0)
		if y < 0 {
			y += 360.0
		}
		return y - 180.0
	}

	// Утилита: останавливаемся при пересечении любой границы окна. Возвращает
	// (target_continuous_lon, true) если на этом шаге пересечена граница, иначе
	// (0, false). Полу-окна определяются от observerLon, не от sat — это даёт
	// покрытие по всему окну карты независимо от положения КА в нём.
	clipBoundary := func(prevCont, newCont float64) (float64, bool) {
		if newCont >= rightBound && prevCont < rightBound {
			return rightBound, true
		}
		if newCont <= leftBound && prevCont > leftBound {
			return leftBound, true
		}
		return 0, false
	}

	// Этап 3+4 forward: пропагация в будущее с lon-clip + time-fallback.
	futurePoints := []TrackPoint{nowPoint}
	contLonF, prevRawLonF, prevLatF, prevTSF := satLonAnchor, satLonRaw, satLat, nowMs
	for i := 1; i <= maxPointsPerSide; i++ {
		dt := time.Duration(i) * step
		if dt > timeFallback {
			break
		}
		t := now.Add(dt)
		eci, errProp := prop.Propagate(t)
		if errProp != nil {
			break
		}
		lla := ECEFToLLA(ECIToECEF(eci))
		rawLon, rawLat, currTS := lla.LonDeg(), lla.LatDeg(), t.UnixMilli()

		isPolepass := math.Abs(rawLat) > polepassLatDeg || math.Abs(prevLatF) > polepassLatDeg
		var delta float64
		if !isPolepass {
			delta = rawLon - prevRawLonF
			for delta > 180 {
				delta -= 360
			}
			for delta < -180 {
				delta += 360
			}
		}
		newContLonF := contLonF + delta

		// Lon-clip: пересечение границы окна → линейная интерполяция к ней.
		if !isPolepass && delta != 0 { //nolint:nestif // расчёт ratio и граничной точки в одном блоке
			if targetCont, crossed := clipBoundary(contLonF, newContLonF); crossed {
				ratio := (targetCont - contLonF) / delta
				if ratio > 1 {
					ratio = 1
				} else if ratio < 0 {
					ratio = 0
				}
				bLon := normalizeLonRaw(targetCont)
				bLat := prevLatF + (rawLat-prevLatF)*ratio
				bTS := prevTSF + int64(float64(currTS-prevTSF)*ratio)
				futurePoints = append(futurePoints, TrackPoint{Lon: bLon, Lat: bLat, TS: bTS})
				break
			}
		}

		contLonF, prevRawLonF, prevLatF, prevTSF = newContLonF, rawLon, rawLat, currTS
		futurePoints = append(futurePoints, TrackPoint{Lon: rawLon, Lat: rawLat, TS: currTS})
	}

	// Этап 3+4 backward: симметрично, в прошлое. Точки собираются в обратном
	// порядке времени (now → t-N), затем разворачиваем.
	pastPointsRev := []TrackPoint{}
	contLonB, prevRawLonB, prevLatB, prevTSB := satLonAnchor, satLonRaw, satLat, nowMs
	for i := 1; i <= maxPointsPerSide; i++ {
		dt := time.Duration(i) * step
		if dt > timeFallback {
			break
		}
		t := now.Add(-dt)
		eci, errProp := prop.Propagate(t)
		if errProp != nil {
			break
		}
		lla := ECEFToLLA(ECIToECEF(eci))
		rawLon, rawLat, currTS := lla.LonDeg(), lla.LatDeg(), t.UnixMilli()

		isPolepass := math.Abs(rawLat) > polepassLatDeg || math.Abs(prevLatB) > polepassLatDeg
		var delta float64
		if !isPolepass {
			delta = rawLon - prevRawLonB
			for delta > 180 {
				delta -= 360
			}
			for delta < -180 {
				delta += 360
			}
		}
		newContLonB := contLonB + delta

		if !isPolepass && delta != 0 { //nolint:nestif // расчёт ratio и граничной точки в одном блоке
			if targetCont, crossed := clipBoundary(contLonB, newContLonB); crossed {
				ratio := (targetCont - contLonB) / delta
				if ratio > 1 {
					ratio = 1
				} else if ratio < 0 {
					ratio = 0
				}
				bLon := normalizeLonRaw(targetCont)
				bLat := prevLatB + (rawLat-prevLatB)*ratio
				bTS := prevTSB + int64(float64(currTS-prevTSB)*ratio)
				pastPointsRev = append(pastPointsRev, TrackPoint{Lon: bLon, Lat: bLat, TS: bTS})
				break
			}
		}

		contLonB, prevRawLonB, prevLatB, prevTSB = newContLonB, rawLon, rawLat, currTS
		pastPointsRev = append(pastPointsRev, TrackPoint{Lon: rawLon, Lat: rawLat, TS: currTS})
	}

	// past собран в обратном порядке времени — переворачиваем.
	pastPoints := make([]TrackPoint, len(pastPointsRev))
	for i, p := range pastPointsRev {
		pastPoints[len(pastPointsRev)-1-i] = p
	}

	// Объединяем в одну time-ordered последовательность.
	allPoints := make([]TrackPoint, 0, len(pastPoints)+len(futurePoints))
	allPoints = append(allPoints, pastPoints...)
	allPoints = append(allPoints, futurePoints...)
	if len(allPoints) == 0 {
		return &GroundTrack{NoradID: tle.NoradID}, nil
	}

	// ВАЖНО: НЕ вызываем splitAtAntimeridian — он режет по lon=±180° (гринвич-AM),
	// что для observerLon ≠ 0 даёт разрыв в середине canvas (точка lon=180 при
	// center=observerLon проектируется в x ≈ (180−observerLon)/360 × w + w/2 —
	// не на край!). По построению трасса этой функции не пересекает антимеридиан
	// окна (= observerLon ± 180°), её raw lon-точки могут «прыгнуть» через ±180°
	// в середине окна, но пиксельный шаг при этом мал (≈ 1 шаг ECEF lon × scale)
	// и линия рисуется сплошной. Если же center = 0 (observerLon = 0), то
	// boundary совпадают с гринвич-AM, и фронтовая проверка `_antimeridianThreshold`
	// (разрыв > w/2 на canvas) корректно разделит концы трассы на разные сегменты.
	past, future := splitPastFuture([][]TrackPoint{allPoints}, nowMs)

	return &GroundTrack{Past: past, Future: future, NoradID: tle.NoradID}, nil
}

// generateTrackPoints генерирует массив точек TrackPoint для заданного временного интервала.
func generateTrackPoints(prop *Propagator, start, end time.Time, step time.Duration) ([]TrackPoint, error) {
	// Оцениваем количество точек для предварительного выделения памяти.
	estimatedPoints := int(end.Sub(start)/step) + 1
	points := make([]TrackPoint, 0, estimatedPoints)

	for t := start; !t.After(end); t = t.Add(step) {
		eci, err := prop.Propagate(t)
		if err != nil {
			// При ошибке пропагации (декей орбиты и пр.) прекращаем.
			if len(points) > 0 {
				return points, nil
			}

			return nil, fmt.Errorf("propagation at %v: %w", t, err)
		}

		ecef := ECIToECEF(eci)
		lla := ECEFToLLA(ecef)

		points = append(points, TrackPoint{
			Lon: lla.LonDeg(),
			Lat: lla.LatDeg(),
			TS:  t.UnixMilli(),
		})
	}

	return points, nil
}

// splitAtAntimeridian разбивает массив точек на сегменты при пересечении антимеридиана (±180°).
// При пересечении добавляется интерполированная точка на границе ±180°.
func splitAtAntimeridian(points []TrackPoint) [][]TrackPoint {
	if len(points) == 0 {
		return nil
	}

	var segments [][]TrackPoint
	currentSeg := []TrackPoint{points[0]}

	for i := 1; i < len(points); i++ {
		prevLon := points[i-1].Lon
		currLon := points[i].Lon

		// Определяем пересечение антимеридиана: скачок долготы > 270°
		// (обычный шаг для LEO при 30 сек ~ 2-4°, так что 270° — явный переход через ±180°).
		if math.Abs(currLon-prevLon) > antimeridianThreshold {
			// Интерполируем точку пересечения.
			boundaryPrev, boundaryNext := interpolateAntimeridian(points[i-1], points[i])

			// Завершаем текущий сегмент точкой на границе.
			currentSeg = append(currentSeg, boundaryPrev)
			segments = append(segments, currentSeg)

			// Начинаем новый сегмент с точки на другой стороне границы.
			currentSeg = []TrackPoint{boundaryNext, points[i]}
		} else {
			currentSeg = append(currentSeg, points[i])
		}
	}

	// Добавляем последний сегмент.
	if len(currentSeg) > 0 {
		segments = append(segments, currentSeg)
	}

	return segments
}

// interpolateAntimeridian вычисляет две точки на границе ±180° при пересечении антимеридиана.
// Возвращает точку на стороне p1 (+180 или -180) и точку на стороне p2 (-180 или +180).
func interpolateAntimeridian(p1, p2 TrackPoint) (TrackPoint, TrackPoint) {
	// Определяем направление пересечения.
	// p1.Lon > 0 и p2.Lon < 0 → пересечение через +180°.
	// p1.Lon < 0 и p2.Lon > 0 → пересечение через -180°.

	var boundaryLon1, boundaryLon2 float64

	if p1.Lon > 0 {
		// Переход: +lon → -lon (через +180°).
		boundaryLon1 = 180.0
		boundaryLon2 = -180.0
	} else {
		// Переход: -lon → +lon (через -180°).
		boundaryLon1 = -180.0
		boundaryLon2 = 180.0
	}

	// Для интерполяции широты используем «развёрнутую» долготу.
	// Если переход через +180°: p2 «на самом деле» имеет долготу p2.Lon + 360.
	// Если переход через -180°: p2 «на самом деле» имеет долготу p2.Lon - 360.
	var p2LonUnwrapped float64
	if p1.Lon > 0 {
		p2LonUnwrapped = p2.Lon + 360.0
	} else {
		p2LonUnwrapped = p2.Lon - 360.0
	}

	// Доля пути от p1 до границы (линейная интерполяция по долготе).
	dLon := p2LonUnwrapped - p1.Lon
	var t float64
	if math.Abs(dLon) > 1e-10 {
		t = (boundaryLon1 - p1.Lon) / dLon
	} else {
		t = 0.5
	}

	// Ограничиваем t в разумных пределах.
	t = math.Max(0.0, math.Min(1.0, t))

	// Интерполированная широта.
	interpLat := p1.Lat + (p2.Lat-p1.Lat)*t

	// Интерполированное время.
	interpTS := p1.TS + int64(float64(p2.TS-p1.TS)*t)

	return TrackPoint{
			Lon: boundaryLon1,
			Lat: interpLat,
			TS:  interpTS,
		}, TrackPoint{
			Lon: boundaryLon2,
			Lat: interpLat,
			TS:  interpTS,
		}
}

// splitPastFuture разделяет сегменты на пройденный (ts < nowMs) и предстоящий (ts >= nowMs) участки.
// Сегмент, содержащий точку now, разделяется на две части.
func splitPastFuture(segments [][]TrackPoint, nowMs int64) ([][]TrackPoint, [][]TrackPoint) {
	var past, future [][]TrackPoint
	for _, seg := range segments {
		if len(seg) == 0 {
			continue
		}

		// Весь сегмент в прошлом.
		if seg[len(seg)-1].TS < nowMs {
			past = append(past, seg)
			continue
		}

		// Весь сегмент в будущем.
		if seg[0].TS >= nowMs {
			future = append(future, seg)
			continue
		}

		// Сегмент пересекает now — разделяем.
		splitIdx := -1

		for i, p := range seg {
			if p.TS >= nowMs {
				splitIdx = i
				break
			}
		}

		if splitIdx <= 0 {
			// Все точки в будущем или разделение на первой точке.
			future = append(future, seg)
			continue
		}

		// Пройденный участок: от начала до splitIdx.
		pastPart := seg[:splitIdx]
		if len(pastPart) > 0 {
			past = append(past, pastPart)
		}

		// Предстоящий участок: от splitIdx до конца.
		futurePart := seg[splitIdx:]
		if len(futurePart) > 0 {
			future = append(future, futurePart)
		}
	}

	return past, future
}
