package tracker

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Ошибки расчёта зоны видимости.
var (
	ErrNilTLEForZone        = errors.New("TLE is nil")
	ErrTooFewPoints         = errors.New("number of zone points must be >= 8")
	ErrVisibilityZoneFailed = errors.New("failed to generate visibility zone")
)

// Количество точек контура зоны видимости по умолчанию (шаг 5°).
const defaultZonePoints = 72

// Минимальное допустимое количество точек контура.
const minZonePoints = 8

// ZonePoint — точка контура зоны видимости спутника (координаты в градусах, готово для JSON/UI).
type ZonePoint struct {
	Lon float64 `json:"lon"` // Долгота, градусы (-180..+180).
	Lat float64 `json:"lat"` // Широта, градусы (-90..+90).
}

// VisibilityZone — контур зоны видимости спутника на поверхности Земли.
// Зона видимости — область, из которой спутник виден над горизонтом (elevation ≥ 0°).
// Segments содержит один или несколько замкнутых полигонов (разбитых по антимеридиану).
type VisibilityZone struct {
	Segments   [][]ZonePoint `json:"segments"`    // Замкнутые полигоны контура (разбиты по ±180°).
	CenterLon  float64       `json:"center_lon"`  // Долгота подспутниковой точки, °.
	CenterLat  float64       `json:"center_lat"`  // Широта подспутниковой точки, °.
	RadiusDeg  float64       `json:"radius_deg"`  // Угловой радиус зоны видимости, °.
	AltitudeKm float64       `json:"altitude_km"` // Высота орбиты, км.
	NoradID    int           `json:"norad_id"`    // NORAD ID спутника.
}

// VisibilityRadius вычисляет угловой радиус зоны видимости спутника (в радианах).
// altitudeKm — высота орбиты над поверхностью Земли в км.
// Формула: rho = arccos(R_earth / (R_earth + altitude)).
// При нулевой или отрицательной высоте возвращает 0.
func VisibilityRadius(altitudeKm float64) float64 {
	if altitudeKm <= 0 {
		return 0
	}

	ratio := WGS84A / (WGS84A + altitudeKm)

	// Защита от численных ошибок (ratio должен быть в [0, 1]).
	if ratio >= 1.0 {
		return 0
	}

	return math.Acos(ratio)
}

// MoveByBearing вычисляет координаты точки, смещённой от (lat, lon) на угловое расстояние distance
// по направлению bearing. Все параметры в радианах. Используется сферическая геометрия.
//
// Формулы:
//
//	lat2 = arcsin(sin(lat)*cos(d) + cos(lat)*sin(d)*cos(bearing))
//	lon2 = lon + atan2(sin(bearing)*sin(d)*cos(lat), cos(d) - sin(lat)*sin(lat2))
func MoveByBearing(lat, lon, distance, bearing float64) (float64, float64) {
	sinLat := math.Sin(lat)
	cosLat := math.Cos(lat)
	sinDist := math.Sin(distance)
	cosDist := math.Cos(distance)
	sinBearing := math.Sin(bearing)
	cosBearing := math.Cos(bearing)

	// Широта новой точки.
	lat2 := math.Asin(sinLat*cosDist + cosLat*sinDist*cosBearing)

	// Долгота новой точки.
	lon2 := lon + math.Atan2(sinBearing*sinDist*cosLat, cosDist-sinLat*math.Sin(lat2))

	// Нормализация долготы в диапазон (-π, +π].
	lon2 = normalizeLon(lon2)

	return lat2, lon2
}

// GenerateVisibilityZone генерирует зону видимости спутника для заданного TLE и момента времени.
// Выполняет полный расчёт: SGP4 → ECI → ECEF → LLA → контур.
// numPoints — количество точек контура (рекомендуется 72 = шаг 5°).
func GenerateVisibilityZone(tle *TLE, t time.Time, numPoints int) (*VisibilityZone, error) {
	if tle == nil {
		return nil, ErrNilTLEForZone
	}

	if numPoints < minZonePoints {
		return nil, fmt.Errorf("%w: got %d, minimum %d", ErrTooFewPoints, numPoints, minZonePoints)
	}

	// Создаём пропагатор и вычисляем позицию.
	prop, err := NewPropagator(tle)
	if err != nil {
		return nil, fmt.Errorf("creating propagator: %w", err)
	}

	eci, err := prop.Propagate(t)
	if err != nil {
		return nil, fmt.Errorf("propagation at %v: %w", t, err)
	}

	// ECI → ECEF → LLA.
	ecef := ECIToECEF(eci)
	lla := ECEFToLLA(ecef)

	zone := GenerateVisibilityZoneFromLLA(lla, tle.NoradID, numPoints)
	if zone == nil {
		return nil, ErrVisibilityZoneFailed
	}

	return zone, nil
}

// GenerateVisibilityZoneFromLLA генерирует контур зоны видимости по готовым координатам LLA.
// Используется когда позиция спутника уже известна (без повторного SGP4).
// lla — координаты спутника (широта/долгота в радианах, высота в км).
// Возвращает nil при nil LLA.
func GenerateVisibilityZoneFromLLA(lla *LLA, noradID, numPoints int) *VisibilityZone {
	if lla == nil {
		return nil
	}

	if numPoints < minZonePoints {
		numPoints = minZonePoints
	}

	// Угловой радиус зоны видимости.
	rho := VisibilityRadius(lla.Alt)
	if rho <= 0 {
		return nil
	}

	// Генерируем точки контура.
	points := make([]ZonePoint, numPoints)
	bearingStep := 2 * math.Pi / float64(numPoints)

	for i := range numPoints {
		bearing := float64(i) * bearingStep

		lat2, lon2 := MoveByBearing(lla.Lat, lla.Lon, rho, bearing)

		points[i] = ZonePoint{
			Lon: lon2 * Rad2Deg,
			Lat: lat2 * Rad2Deg,
		}
	}

	// Уплотняем контур: вставляем промежуточные точки по дуге большого круга
	// между соседними точками, которые далеко друг от друга на equirectangular-карте.
	// Без этого вблизи полюсов прямые отрезки между точками «убегают» по долготе.
	points = densifyZoneContour(points, densifyMaxLonGap)

	segments := splitZoneAtAntimeridian(points)

	// Для footprint, пересекающего полюс, замыкаем полигоны через край карты.
	centerLatDeg := lla.LatDeg()
	rhoDeg := rho * Rad2Deg
	segments = closePolarSegments(segments, centerLatDeg, rhoDeg)

	return &VisibilityZone{
		Segments:   segments,
		CenterLon:  lla.LonDeg(),
		CenterLat:  lla.LatDeg(),
		RadiusDeg:  rho * Rad2Deg,
		AltitudeKm: lla.Alt,
		NoradID:    noradID,
	}
}

// Максимальный разрыв по долготе (°) между соседними точками контура.
// Если Δlon больше — вставляются промежуточные точки вдоль дуги большого круга.
const densifyMaxLonGap = 10.0

// densifyZoneContour уплотняет замкнутый контур зоны видимости.
// Между каждой парой соседних точек (включая last→first), у которых разница
// долгот на equirectangular-карте превышает maxLonGap°, вставляются промежуточные
// точки, вычисленные по SLERP (сферическая линейная интерполяция).
// Это исключает визуальные артефакты при проекции контура вблизи полюсов.
func densifyZoneContour(points []ZonePoint, maxLonGap float64) []ZonePoint {
	n := len(points)
	if n < 3 {
		return points
	}

	result := make([]ZonePoint, 0, n*2)

	for i := range n {
		result = append(result, points[i])

		j := (i + 1) % n

		// Разница долгот с учётом антимеридиана.
		lonGap := math.Abs(points[i].Lon - points[j].Lon)
		if lonGap > 180 {
			lonGap = 360 - lonGap
		}

		if lonGap <= maxLonGap {
			continue
		}

		// Количество промежуточных точек.
		numInsert := int(math.Ceil(lonGap / maxLonGap))

		lat1 := points[i].Lat * Deg2Rad
		lon1 := points[i].Lon * Deg2Rad
		lat2 := points[j].Lat * Deg2Rad
		lon2 := points[j].Lon * Deg2Rad

		d := haversineDistance(lat1, lon1, lat2, lon2)
		if d < 1e-10 {
			continue
		}

		for k := 1; k < numInsert; k++ {
			f := float64(k) / float64(numInsert)
			lat, lon := slerpGreatCircle(lat1, lon1, lat2, lon2, d, f)
			result = append(result, ZonePoint{
				Lon: normalizeLonDeg(lon * Rad2Deg),
				Lat: lat * Rad2Deg,
			})
		}
	}

	return result
}

// haversineDistance вычисляет угловое расстояние (рад) между двумя точками (lat/lon в радианах).
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	dlat := lat2 - lat1
	dlon := lon2 - lon1
	a := math.Sin(dlat/2)*math.Sin(dlat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dlon/2)*math.Sin(dlon/2)

	return 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// slerpGreatCircle вычисляет промежуточную точку на дуге большого круга
// между (lat1,lon1) и (lat2,lon2) при доле f ∈ (0,1).
// d — угловое расстояние между точками (рад).
func slerpGreatCircle(lat1, lon1, lat2, lon2, d, f float64) (float64, float64) {
	sinD := math.Sin(d)
	a := math.Sin((1-f)*d) / sinD
	b := math.Sin(f*d) / sinD

	x := a*math.Cos(lat1)*math.Cos(lon1) + b*math.Cos(lat2)*math.Cos(lon2)
	y := a*math.Cos(lat1)*math.Sin(lon1) + b*math.Cos(lat2)*math.Sin(lon2)
	z := a*math.Sin(lat1) + b*math.Sin(lat2)

	lat := math.Atan2(z, math.Sqrt(x*x+y*y))
	lon := math.Atan2(y, x)

	return lat, lon
}

// normalizeLonDeg нормализует долготу в градусах в диапазон (-180, +180].
func normalizeLonDeg(lon float64) float64 {
	for lon > 180 {
		lon -= 360
	}

	for lon <= -180 {
		lon += 360
	}

	return lon
}

// splitZoneAtAntimeridian разбивает замкнутый контур на сегменты по антимеридиану (±180°).
// Если контур не пересекает антимеридиан — возвращает один сегмент.
// При пересечении — вставляет точки на границе ±180° и возвращает несколько замкнутых полигонов.
func splitZoneAtAntimeridian(points []ZonePoint) [][]ZonePoint {
	n := len(points)
	if n < 3 {
		return [][]ZonePoint{points}
	}

	// Проверяем, пересекает ли контур антимеридиан.
	hasCrossing := false
	for i := range n {
		j := (i + 1) % n
		if isAntimeridianCrossing(points[i].Lon, points[j].Lon) {
			hasCrossing = true
			break
		}
	}

	if !hasCrossing {
		return [][]ZonePoint{points}
	}

	// Разбиваем кольцо на сегменты при каждом пересечении антимеридиана.
	var segments [][]ZonePoint
	current := make([]ZonePoint, 0, n)

	for i := range n {
		j := (i + 1) % n
		current = append(current, points[i])

		if isAntimeridianCrossing(points[i].Lon, points[j].Lon) {
			latCross := interpolateAntimeridianLat(points[i], points[j])

			current = append(current, ZonePoint{Lon: nearestMeridianBoundary(points[i].Lon), Lat: latCross})
			segments = append(segments, current)

			current = []ZonePoint{{Lon: nearestMeridianBoundary(points[j].Lon), Lat: latCross}}
		}
	}

	// Последний сегмент замыкается на первый (кольцо).
	if len(current) > 0 && len(segments) > 0 {
		segments[0] = append(current, segments[0]...)
	}

	return segments
}

// nearestMeridianBoundary возвращает ±180° в зависимости от знака долготы.
func nearestMeridianBoundary(lon float64) float64 {
	if lon > 0 {
		return 180.0
	}
	return -180.0
}

// isAntimeridianCrossing определяет, пересекают ли две точки антимеридиан (±180°).
func isAntimeridianCrossing(lon1, lon2 float64) bool {
	return math.Abs(lon1-lon2) > 180.0
}

// interpolateAntimeridianLat вычисляет широту в точке пересечения антимеридиана
// между двумя точками (линейная интерполяция по долготе).
func interpolateAntimeridianLat(p1, p2 ZonePoint) float64 {
	// Развернём долготы, чтобы путь шёл через ±180°.
	lon1, lon2 := p1.Lon, p2.Lon
	if lon1 > 0 && lon2 < 0 {
		lon2 += 360.0
	} else if lon1 < 0 && lon2 > 0 {
		lon1 += 360.0
	}

	// Интерполируем lat на lon=180° (или 180+360=540, что одно и то же).
	target := 180.0
	if lon1 > 180.0 || lon2 > 180.0 {
		target = 180.0
		if lon1 > 180.0 && lon2 > 180.0 {
			target = 360.0 + 180.0
		}
	}

	dlon := lon2 - lon1
	if math.Abs(dlon) < 1e-10 {
		return (p1.Lat + p2.Lat) / 2
	}

	fraction := (target - lon1) / dlon
	return p1.Lat + fraction*(p2.Lat-p1.Lat)
}

// closePolarSegments добавляет угловые точки карты к сегментам footprint,
// пересекающего полюс. На equirectangular-проекции полюс — это линия (верх/низ карты).
// Без замыкания через полюс closePath() создаёт горизонтальную «полоску» вместо заливки.
//
// Для North Pole (centerLat + radius > 90°): контур трассирует нижнюю границу footprint,
// а верхняя часть уходит за полюс. Добавляем точки по верхнему краю карты (lat=90°).
//
// Для South Pole (centerLat - radius < -90°): аналогично, добавляем lat=-90°.
func closePolarSegments(segments [][]ZonePoint, centerLatDeg, radiusDeg float64) [][]ZonePoint {
	crossesNorth := centerLatDeg+radiusDeg > 90.0
	crossesSouth := centerLatDeg-radiusDeg < -90.0

	if !crossesNorth && !crossesSouth {
		return segments
	}

	for i, seg := range segments {
		if len(seg) < 3 {
			continue
		}

		first := seg[0]
		last := seg[len(seg)-1]

		// Проверяем: сегмент начинается и заканчивается на краях карты (±180°)?
		startsAtEdge := math.Abs(math.Abs(first.Lon)-180.0) < 0.1
		endsAtEdge := math.Abs(math.Abs(last.Lon)-180.0) < 0.1

		if !startsAtEdge || !endsAtEdge {
			continue
		}

		if crossesNorth {
			// Добавляем угловые точки через Северный полюс.
			segments[i] = append(seg,
				ZonePoint{Lon: last.Lon, Lat: 90.0},
				ZonePoint{Lon: first.Lon, Lat: 90.0},
			)
		} else if crossesSouth {
			// Добавляем угловые точки через Южный полюс.
			segments[i] = append(seg,
				ZonePoint{Lon: last.Lon, Lat: -90.0},
				ZonePoint{Lon: first.Lon, Lat: -90.0},
			)
		}
	}

	return segments
}

// normalizeLon нормализует долготу в диапазон (-π, +π].
func normalizeLon(lon float64) float64 {
	for lon > math.Pi {
		lon -= 2 * math.Pi
	}

	for lon <= -math.Pi {
		lon += 2 * math.Pi
	}

	return lon
}
