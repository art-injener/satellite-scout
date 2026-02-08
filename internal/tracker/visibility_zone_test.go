package tracker

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- VisibilityRadius ---

func TestVisibilityRadius_ISS(t *testing.T) {
	// МКС: высота ~420 км, ожидаемый угловой радиус ~20° (≈0.35 рад).
	altKm := 420.0
	rho := VisibilityRadius(altKm)

	rhoDeg := rho * Rad2Deg

	// Теоретическое значение: arccos(6378.137 / (6378.137 + 420)) ≈ 20.3°.
	if rhoDeg < 19.0 || rhoDeg > 22.0 {
		t.Errorf("ISS (alt=%.0f km): expected radius ~20°, got %.2f°", altKm, rhoDeg)
	}
}

func TestVisibilityRadius_GEO(t *testing.T) {
	// GEO: высота ~35786 км, ожидаемый угловой радиус ~81°.
	altKm := 35786.0
	rho := VisibilityRadius(altKm)

	rhoDeg := rho * Rad2Deg

	// Теоретическое значение: arccos(6378.137 / (6378.137 + 35786)) ≈ 81.3°.
	if rhoDeg < 80.0 || rhoDeg > 83.0 {
		t.Errorf("GEO (alt=%.0f km): expected radius ~81°, got %.2f°", altKm, rhoDeg)
	}
}

func TestVisibilityRadius_ZeroAlt(t *testing.T) {
	// Высота 0 → радиус 0.
	rho := VisibilityRadius(0)
	if rho != 0 {
		t.Errorf("expected radius 0 for zero altitude, got %f", rho)
	}
}

func TestVisibilityRadius_NegativeAlt(t *testing.T) {
	// Отрицательная высота → радиус 0.
	rho := VisibilityRadius(-100)
	if rho != 0 {
		t.Errorf("expected radius 0 for negative altitude, got %f", rho)
	}
}

// --- MoveByBearing ---

func TestMoveByBearing_NorthFromEquator(t *testing.T) {
	// Перемещение на 10° на север от точки на экваторе.
	lat := 0.0             // экватор, рад.
	lon := 0.0             // гринвич, рад.
	dist := 10.0 * Deg2Rad // 10° в радианах.
	bearing := 0.0         // на север.

	lat2, lon2 := MoveByBearing(lat, lon, dist, bearing)

	lat2Deg := lat2 * Rad2Deg
	lon2Deg := lon2 * Rad2Deg

	// Ожидаем: lat ≈ 10°, lon ≈ 0°.
	if math.Abs(lat2Deg-10.0) > 0.1 {
		t.Errorf("latitude: expected ~10°, got %.4f°", lat2Deg)
	}

	if math.Abs(lon2Deg) > 0.1 {
		t.Errorf("longitude: expected ~0°, got %.4f°", lon2Deg)
	}
}

func TestMoveByBearing_EastFromEquator(t *testing.T) {
	// Перемещение на 15° на восток от точки на экваторе.
	lat := 0.0
	lon := 0.0
	dist := 15.0 * Deg2Rad
	bearing := math.Pi / 2 // на восток (90°).

	lat2, lon2 := MoveByBearing(lat, lon, dist, bearing)

	lat2Deg := lat2 * Rad2Deg
	lon2Deg := lon2 * Rad2Deg

	// Ожидаем: lat ≈ 0°, lon ≈ 15°.
	if math.Abs(lat2Deg) > 0.1 {
		t.Errorf("latitude: expected ~0°, got %.4f°", lat2Deg)
	}

	if math.Abs(lon2Deg-15.0) > 0.1 {
		t.Errorf("longitude: expected ~15°, got %.4f°", lon2Deg)
	}
}

func TestMoveByBearing_SouthFromNorthPole(t *testing.T) {
	// Перемещение от Северного полюса на юг на 5°.
	lat := 90.0 * Deg2Rad
	lon := 0.0
	dist := 5.0 * Deg2Rad
	bearing := math.Pi // на юг (180°).

	lat2, lon2 := MoveByBearing(lat, lon, dist, bearing)

	lat2Deg := lat2 * Rad2Deg
	_ = lon2

	// Ожидаем: lat ≈ 85°.
	if math.Abs(lat2Deg-85.0) > 0.5 {
		t.Errorf("latitude: expected ~85°, got %.4f°", lat2Deg)
	}
}

func TestMoveByBearing_ZeroDistance(t *testing.T) {
	// Перемещение на 0 → точка не меняется.
	lat := 45.0 * Deg2Rad
	lon := 30.0 * Deg2Rad

	lat2, lon2 := MoveByBearing(lat, lon, 0, 0)

	if math.Abs(lat2-lat) > 1e-10 || math.Abs(lon2-lon) > 1e-10 {
		t.Errorf("zero distance should not change point: (%.6f, %.6f) -> (%.6f, %.6f)",
			lat*Rad2Deg, lon*Rad2Deg, lat2*Rad2Deg, lon2*Rad2Deg)
	}
}

func TestMoveByBearing_AntimeridianCrossing(t *testing.T) {
	// Перемещение на восток от точки вблизи антимеридиана (+179°).
	lat := 0.0
	lon := 179.0 * Deg2Rad
	dist := 3.0 * Deg2Rad
	bearing := math.Pi / 2 // на восток.

	_, lon2 := MoveByBearing(lat, lon, dist, bearing)

	lon2Deg := lon2 * Rad2Deg

	// Долгота должна быть нормализована: -178° (перешли через +180°).
	if lon2Deg > 0 {
		t.Errorf("expected negative longitude after antimeridian crossing, got %.4f°", lon2Deg)
	}
}

// --- GenerateVisibilityZone ---

func TestGenerateVisibilityZone_ISS(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	zone, err := GenerateVisibilityZone(tle, now, defaultZonePoints)
	if err != nil {
		t.Fatalf("GenerateVisibilityZone: %v", err)
	}

	// Проверяем количество точек.
	if len(zone.Points) != defaultZonePoints {
		t.Errorf("expected %d points, got %d", defaultZonePoints, len(zone.Points))
	}

	// Проверяем радиус (ISS ~420 км → ~20°).
	if zone.RadiusDeg < 18.0 || zone.RadiusDeg > 23.0 {
		t.Errorf("ISS visibility radius: expected ~20°, got %.2f°", zone.RadiusDeg)
	}

	// Проверяем высоту (ISS ~410-430 км).
	if zone.AltitudeKm < 350 || zone.AltitudeKm > 500 {
		t.Errorf("ISS altitude: expected 350-500 km, got %.2f km", zone.AltitudeKm)
	}

	// NoradID.
	if zone.NoradID != 25544 {
		t.Errorf("NoradID: expected 25544, got %d", zone.NoradID)
	}

	// Проверяем что точки образуют замкнутый контур (первая ≈ последняя).
	first := zone.Points[0]
	last := zone.Points[len(zone.Points)-1]

	// Контур не замыкается дублированием — но все точки должны быть на одинаковом расстоянии от центра.
	// Проверяем, что все точки лежат примерно на угловом радиусе от центра.
	for i, p := range zone.Points {
		dist := angularDistance(zone.CenterLat, zone.CenterLon, p.Lat, p.Lon)
		distDeg := dist * Rad2Deg

		// Допуск ±1.5° из-за сферической геометрии vs WGS84.
		if math.Abs(distDeg-zone.RadiusDeg) > 1.5 {
			t.Errorf("point %d: distance from center %.2f° differs from radius %.2f° by more than 1.5°",
				i, distDeg, zone.RadiusDeg)
		}
	}

	_ = first
	_ = last
}

func TestGenerateVisibilityZone_GEO(t *testing.T) {
	tle := parseTestTLE(t, gtGEOLines)
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	zone, err := GenerateVisibilityZone(tle, now, defaultZonePoints)
	if err != nil {
		t.Fatalf("GenerateVisibilityZone GEO: %v", err)
	}

	// GEO спутник: радиус зоны видимости ~81°.
	if zone.RadiusDeg < 78.0 || zone.RadiusDeg > 84.0 {
		t.Errorf("GEO visibility radius: expected ~81°, got %.2f°", zone.RadiusDeg)
	}

	// Высота GEO ~35000-36000 км.
	if zone.AltitudeKm < 34000 || zone.AltitudeKm > 37000 {
		t.Errorf("GEO altitude: expected 34000-37000 km, got %.2f km", zone.AltitudeKm)
	}
}

func TestGenerateVisibilityZone_NilTLE(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	_, err := GenerateVisibilityZone(nil, now, defaultZonePoints)
	if err == nil {
		t.Error("expected error for nil TLE")
	}
}

func TestGenerateVisibilityZone_InvalidPoints(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Слишком мало точек.
	_, err := GenerateVisibilityZone(tle, now, 2)
	if err == nil {
		t.Error("expected error for numPoints < minimum")
	}
}

// --- GenerateVisibilityZoneFromLLA ---

func TestGenerateVisibilityZoneFromLLA_Equator(t *testing.T) {
	// Спутник над экватором, высота 420 км.
	lla := &LLA{
		Lat: 0,
		Lon: 0,
		Alt: 420.0,
	}

	zone := GenerateVisibilityZoneFromLLA(lla, 25544, defaultZonePoints)
	if zone == nil {
		t.Fatal("GenerateVisibilityZoneFromLLA returned nil")
	}

	if len(zone.Points) != defaultZonePoints {
		t.Errorf("expected %d points, got %d", defaultZonePoints, len(zone.Points))
	}

	// Центр должен быть в (0, 0).
	if math.Abs(zone.CenterLat) > 0.01 || math.Abs(zone.CenterLon) > 0.01 {
		t.Errorf("zone center: expected (0, 0), got (%.4f, %.4f)", zone.CenterLat, zone.CenterLon)
	}

	// Проверяем симметрию: должны быть точки с положительной и отрицательной широтой.
	hasPositiveLat := false
	hasNegativeLat := false

	for _, p := range zone.Points {
		if p.Lat > 1.0 {
			hasPositiveLat = true
		}

		if p.Lat < -1.0 {
			hasNegativeLat = true
		}
	}

	if !hasPositiveLat || !hasNegativeLat {
		t.Error("equatorial visibility zone should be symmetric in latitude")
	}
}

func TestGenerateVisibilityZoneFromLLA_NorthPole(t *testing.T) {
	// Спутник над Северным полюсом, высота 800 км.
	lla := &LLA{
		Lat: 90.0 * Deg2Rad,
		Lon: 0,
		Alt: 800.0,
	}

	zone := GenerateVisibilityZoneFromLLA(lla, 12345, defaultZonePoints)
	if zone == nil {
		t.Fatal("GenerateVisibilityZoneFromLLA (pole) returned nil")
	}

	// Все точки контура должны иметь широту > 50° (зона вокруг полюса).
	for i, p := range zone.Points {
		if p.Lat < 50.0 {
			t.Errorf("point %d: latitude %.2f° too low for North Pole zone", i, p.Lat)
			break
		}
	}
}

func TestGenerateVisibilityZoneFromLLA_Antimeridian(t *testing.T) {
	// Спутник около антимеридиана: lon = 179°.
	lla := &LLA{
		Lat: 0,
		Lon: 179.0 * Deg2Rad,
		Alt: 420.0,
	}

	zone := GenerateVisibilityZoneFromLLA(lla, 99999, defaultZonePoints)
	if zone == nil {
		t.Fatal("GenerateVisibilityZoneFromLLA (antimeridian) returned nil")
	}

	// Должны быть точки и с положительной, и с отрицательной долготой.
	hasPositiveLon := false
	hasNegativeLon := false

	for _, p := range zone.Points {
		if p.Lon > 170.0 {
			hasPositiveLon = true
		}

		if p.Lon < -170.0 {
			hasNegativeLon = true
		}
	}

	if !hasPositiveLon || !hasNegativeLon {
		t.Error("antimeridian visibility zone should have points on both sides of ±180°")
	}
}

func TestGenerateVisibilityZoneFromLLA_NilLLA(t *testing.T) {
	zone := GenerateVisibilityZoneFromLLA(nil, 25544, defaultZonePoints)
	if zone != nil {
		t.Error("expected nil for nil LLA")
	}
}

// --- Координаты в допустимых диапазонах ---

func TestGenerateVisibilityZone_CoordinatesInRange(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	zone, err := GenerateVisibilityZone(tle, now, defaultZonePoints)
	if err != nil {
		t.Fatalf("GenerateVisibilityZone: %v", err)
	}

	for i, p := range zone.Points {
		if p.Lat < -90 || p.Lat > 90 {
			t.Errorf("point %d: latitude %.4f° out of range [-90, 90]", i, p.Lat)
		}

		if p.Lon < -180 || p.Lon > 180 {
			t.Errorf("point %d: longitude %.4f° out of range [-180, 180]", i, p.Lon)
		}
	}
}

// --- Benchmark ---

func BenchmarkGenerateVisibilityZone_ISS(b *testing.B) {
	tle := parseTestTLEB(b, gtISSLines)
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	b.ResetTimer()

	for b.Loop() {
		_, err := GenerateVisibilityZone(tle, now, defaultZonePoints)
		require.NoError(b, err)
	}
}

func BenchmarkMoveByBearing(b *testing.B) {
	lat := 45.0 * Deg2Rad
	lon := 30.0 * Deg2Rad
	dist := 20.0 * Deg2Rad
	bearing := math.Pi / 4

	b.ResetTimer()

	for b.Loop() {
		MoveByBearing(lat, lon, dist, bearing)
	}
}

// --- Вспомогательные функции ---

// angularDistance вычисляет угловое расстояние между двумя точками (lat/lon в градусах).
// Используется формула Haversine.
func angularDistance(lat1Deg, lon1Deg, lat2Deg, lon2Deg float64) float64 {
	lat1 := lat1Deg * Deg2Rad
	lon1 := lon1Deg * Deg2Rad
	lat2 := lat2Deg * Deg2Rad
	lon2 := lon2Deg * Deg2Rad

	dlat := lat2 - lat1
	dlon := lon2 - lon1

	a := math.Sin(dlat/2)*math.Sin(dlat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dlon/2)*math.Sin(dlon/2)

	return 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// parseTestTLEB — вспомогательная функция для benchmark.
func parseTestTLEB(b *testing.B, lines []string) *TLE {
	b.Helper()

	tle, err := ParseTLE(lines)
	if err != nil {
		b.Fatalf("ParseTLE failed: %v", err)
	}

	return tle
}
