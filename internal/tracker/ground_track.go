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

// GenerateDefaultGroundTrack генерирует трассу орбиты с автодиапазоном:
// 1 орбитальный период назад + 3 периода вперёд, шаг 30 секунд.
func GenerateDefaultGroundTrack(tle *TLE, now time.Time) (*GroundTrack, error) {
	if tle == nil {
		return nil, ErrNilTLEForTrack
	}

	period := time.Duration(tle.OrbitalPeriod() * float64(time.Minute))
	if period <= 0 {
		return nil, fmt.Errorf("%w: orbital period %.2f min", ErrInvalidRange, tle.OrbitalPeriod())
	}

	const defaultStep = 30 * time.Second

	start := now.Add(-1 * period)
	end := now.Add(3 * period)

	return GenerateGroundTrack(tle, start, end, now, defaultStep)
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
