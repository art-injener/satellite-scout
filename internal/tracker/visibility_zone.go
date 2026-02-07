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
type VisibilityZone struct {
	Points     []ZonePoint `json:"points"`      // Точки контура (lon, lat в градусах).
	CenterLon  float64     `json:"center_lon"`  // Долгота подспутниковой точки, °.
	CenterLat  float64     `json:"center_lat"`  // Широта подспутниковой точки, °.
	RadiusDeg  float64     `json:"radius_deg"`  // Угловой радиус зоны видимости, °.
	AltitudeKm float64     `json:"altitude_km"` // Высота орбиты, км.
	NoradID    int         `json:"norad_id"`    // NORAD ID спутника.
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

	return &VisibilityZone{
		Points:     points,
		CenterLon:  lla.LonDeg(),
		CenterLat:  lla.LatDeg(),
		RadiusDeg:  rho * Rad2Deg,
		AltitudeKm: lla.Alt,
		NoradID:    noradID,
	}
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
