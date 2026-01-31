package tracker

import (
	"math"
	"testing"
	"time"
)

// tolerance — допустимая погрешность для сравнения float.
const (
	toleranceCoord  = 1e-6 // Координаты, км.
	toleranceDegree = 1e-4 // Углы, градусы.
)

// almostEqual проверяет равенство с допуском.
func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

// TestWGS84Constants проверяет константы WGS84.
func TestWGS84Constants(t *testing.T) {
	// Проверяем соотношение между константами.
	expectedB := WGS84A * (1.0 - WGS84F)
	if !almostEqual(WGS84B, expectedB, 1e-10) {
		t.Errorf("WGS84B: expected %v, got %v", expectedB, WGS84B)
	}

	expectedE2 := 2*WGS84F - WGS84F*WGS84F
	if !almostEqual(WGS84E2, expectedE2, 1e-15) {
		t.Errorf("WGS84E2: expected %v, got %v", expectedE2, WGS84E2)
	}

	// Проверяем известные значения.
	if !almostEqual(WGS84A, 6378.137, 1e-6) {
		t.Errorf("WGS84A: expected 6378.137, got %v", WGS84A)
	}
}

// TestECIToECEF_AndBack проверяет обратимость ECI ↔ ECEF.
func TestECIToECEF_AndBack(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	testCases := []struct {
		name string
		eci  *ECIPosition
	}{
		{
			name: "ISS-like position",
			eci: &ECIPosition{
				X: -4400.594, Y: 1932.870, Z: 4760.712,
				Vx: -5.197, Vy: -4.845, Vz: 2.012,
				Time: testTime,
			},
		},
		{
			name: "GEO satellite",
			eci: &ECIPosition{
				X: 42164.0, Y: 0.0, Z: 0.0,
				Time: testTime,
			},
		},
		{
			name: "Polar orbit",
			eci: &ECIPosition{
				X: 0.0, Y: 0.0, Z: 7000.0,
				Time: testTime,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// ECI → ECEF → ECI.
			ecef := ECIToECEF(tc.eci)
			if ecef == nil {
				t.Fatal("ECIToECEF returned nil")
			}

			eciBack := ECEFToECI(ecef)
			if eciBack == nil {
				t.Fatal("ECEFToECI returned nil")
			}

			// Проверяем координаты.
			if !almostEqual(tc.eci.X, eciBack.X, toleranceCoord) {
				t.Errorf("X: expected %v, got %v", tc.eci.X, eciBack.X)
			}
			if !almostEqual(tc.eci.Y, eciBack.Y, toleranceCoord) {
				t.Errorf("Y: expected %v, got %v", tc.eci.Y, eciBack.Y)
			}
			if !almostEqual(tc.eci.Z, eciBack.Z, toleranceCoord) {
				t.Errorf("Z: expected %v, got %v", tc.eci.Z, eciBack.Z)
			}
		})
	}
}

// TestLLAToECEF_AndBack проверяет обратимость LLA ↔ ECEF.
func TestLLAToECEF_AndBack(t *testing.T) {
	testCases := []struct {
		name   string
		latDeg float64
		lonDeg float64
		altKm  float64
	}{
		{"Moscow", 55.7558, 37.6173, 0.156},
		{"Equator", 0.0, 0.0, 0.0},
		{"North Pole", 90.0, 0.0, 0.0},
		{"South Pole", -90.0, 0.0, 0.0},
		{"Antimeridian", 45.0, 180.0, 1.0},
		{"Negative Longitude", 45.0, -90.0, 0.5},
		{"High Altitude (ISS)", 51.6, 0.0, 420.0},
		{"GEO Altitude", 0.0, 105.0, 35786.0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lla := NewLLAFromDegrees(tc.latDeg, tc.lonDeg, tc.altKm)

			// LLA → ECEF → LLA.
			ecef := LLAToECEF(lla)
			if ecef == nil {
				t.Fatal("LLAToECEF returned nil")
			}

			llaBack := ECEFToLLA(ecef)
			if llaBack == nil {
				t.Fatal("ECEFToLLA returned nil")
			}

			// Проверяем координаты в градусах.
			latBackDeg := llaBack.LatDeg()
			lonBackDeg := llaBack.LonDeg()

			if !almostEqual(tc.latDeg, latBackDeg, toleranceDegree) {
				t.Errorf("Lat: expected %v°, got %v°", tc.latDeg, latBackDeg)
			}

			// Долгота на полюсах не определена, пропускаем проверку.
			if math.Abs(tc.latDeg) < 89.0 {
				// Нормализуем долготу к [-180, 180].
				expectedLon := tc.lonDeg
				if expectedLon > 180 {
					expectedLon -= 360
				}
				actualLon := lonBackDeg
				if actualLon > 180 {
					actualLon -= 360
				}

				if !almostEqual(expectedLon, actualLon, toleranceDegree) {
					t.Errorf("Lon: expected %v°, got %v°", expectedLon, actualLon)
				}
			}

			if !almostEqual(tc.altKm, llaBack.Alt, toleranceCoord) {
				t.Errorf("Alt: expected %v km, got %v km", tc.altKm, llaBack.Alt)
			}
		})
	}
}

// TestECEFToAER_KnownPositions проверяет расчёт AER для известных позиций.
func TestECEFToAER_KnownPositions(t *testing.T) {
	// Наблюдатель в Москве.
	observer := NewObserver(55.7558, 37.6173, 0.156)
	obsECEF := ObserverToECEF(observer)
	obsLLA := observer.ToLLA()

	testCases := []struct {
		name      string
		satLLA    *LLA // Позиция спутника над точкой.
		expectEl  float64
		expectAz  float64
		tolerance float64
	}{
		{
			name:      "Satellite directly overhead",
			satLLA:    NewLLAFromDegrees(55.7558, 37.6173, 400.0),
			expectEl:  90.0 * Deg2Rad, // Прямо над головой.
			expectAz:  0.0,            // Азимут не определён при El=90°.
			tolerance: 0.1,            // Градусы.
		},
		{
			name:      "Satellite to the North",
			satLLA:    NewLLAFromDegrees(65.0, 37.6173, 400.0),
			expectAz:  0.0, // Север.
			tolerance: 10.0,
		},
		{
			name:      "Satellite to the East",
			satLLA:    NewLLAFromDegrees(55.7558, 50.0, 400.0),
			expectAz:  90.0 * Deg2Rad, // Восток.
			tolerance: 10.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			satECEF := LLAToECEF(tc.satLLA)
			aer := ECEFToAER(satECEF, obsECEF, obsLLA)

			if aer == nil {
				t.Fatal("ECEFToAER returned nil")
			}

			// Для спутника над головой проверяем только El.
			if tc.name == "Satellite directly overhead" {
				if !almostEqual(aer.El, tc.expectEl, tc.tolerance*Deg2Rad) {
					t.Errorf("El: expected ~%v°, got %v°",
						tc.expectEl*Rad2Deg, aer.ElDeg())
				}
			}

			// Проверяем что Range > 0.
			if aer.Range <= 0 {
				t.Errorf("Range should be positive, got %v", aer.Range)
			}
		})
	}
}

// TestObserverGetAER проверяет удобный метод Observer.GetAER.
func TestObserverGetAER(t *testing.T) {
	observer := NewObserver(55.7558, 37.6173, 0.156)

	testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	eci := &ECIPosition{
		X: -4400.594, Y: 1932.870, Z: 4760.712,
		Time: testTime,
	}

	aer := observer.GetAER(eci)
	if aer == nil {
		t.Fatal("GetAER returned nil")
	}

	// Проверяем что значения в разумных пределах.
	if aer.Az < 0 || aer.Az > 2*math.Pi {
		t.Errorf("Az out of range: %v", aer.AzDeg())
	}

	if aer.El < -math.Pi/2 || aer.El > math.Pi/2 {
		t.Errorf("El out of range: %v", aer.ElDeg())
	}

	if aer.Range <= 0 {
		t.Errorf("Range should be positive: %v", aer.Range)
	}
}

// TestNilInputs проверяет обработку nil входных данных.
func TestNilInputs(t *testing.T) {
	if ECIToECEF(nil) != nil {
		t.Error("ECIToECEF(nil) should return nil")
	}

	if ECEFToECI(nil) != nil {
		t.Error("ECEFToECI(nil) should return nil")
	}

	if LLAToECEF(nil) != nil {
		t.Error("LLAToECEF(nil) should return nil")
	}

	if ECEFToLLA(nil) != nil {
		t.Error("ECEFToLLA(nil) should return nil")
	}

	if ObserverToECEF(nil) != nil {
		t.Error("ObserverToECEF(nil) should return nil")
	}

	var obs *Observer
	if obs.ToLLA() != nil {
		t.Error("nil Observer.ToLLA() should return nil")
	}

	if obs.GetAER(&ECIPosition{}) != nil {
		t.Error("nil Observer.GetAER() should return nil")
	}

	observer := NewObserver(55.0, 37.0, 0.0)
	if observer.GetAER(nil) != nil {
		t.Error("Observer.GetAER(nil) should return nil")
	}

	if ECEFToAER(nil, nil, nil) != nil {
		t.Error("ECEFToAER(nil, nil, nil) should return nil")
	}
}

// TestLLADegreeConversions проверяет конвертацию градусов.
func TestLLADegreeConversions(t *testing.T) {
	lla := NewLLAFromDegrees(45.0, 90.0, 100.0)

	if !almostEqual(lla.LatDeg(), 45.0, 1e-10) {
		t.Errorf("LatDeg: expected 45.0, got %v", lla.LatDeg())
	}

	if !almostEqual(lla.LonDeg(), 90.0, 1e-10) {
		t.Errorf("LonDeg: expected 90.0, got %v", lla.LonDeg())
	}
}

// TestAERDegreeConversions проверяет конвертацию AER в градусы.
func TestAERDegreeConversions(t *testing.T) {
	aer := &AER{
		Az:    math.Pi / 2, // 90°.
		El:    math.Pi / 4, // 45°.
		Range: 1000.0,
	}

	if !almostEqual(aer.AzDeg(), 90.0, 1e-10) {
		t.Errorf("AzDeg: expected 90.0, got %v", aer.AzDeg())
	}

	if !almostEqual(aer.ElDeg(), 45.0, 1e-10) {
		t.Errorf("ElDeg: expected 45.0, got %v", aer.ElDeg())
	}
}

// TestKnownECEFToLLA проверяет преобразование для известных точек.
func TestKnownECEFToLLA(t *testing.T) {
	// Точка на экваторе (0°, 0°), уровень моря.
	ecef := &ECEFPosition{X: WGS84A, Y: 0, Z: 0}
	lla := ECEFToLLA(ecef)

	if !almostEqual(lla.LatDeg(), 0.0, toleranceDegree) {
		t.Errorf("Equator Lat: expected 0°, got %v°", lla.LatDeg())
	}
	if !almostEqual(lla.LonDeg(), 0.0, toleranceDegree) {
		t.Errorf("Equator Lon: expected 0°, got %v°", lla.LonDeg())
	}
	if !almostEqual(lla.Alt, 0.0, 0.001) {
		t.Errorf("Equator Alt: expected 0 km, got %v km", lla.Alt)
	}

	// Северный полюс.
	ecefPole := &ECEFPosition{X: 0, Y: 0, Z: WGS84B}
	llaPole := ECEFToLLA(ecefPole)

	if !almostEqual(llaPole.LatDeg(), 90.0, toleranceDegree) {
		t.Errorf("North Pole Lat: expected 90°, got %v°", llaPole.LatDeg())
	}
	if !almostEqual(llaPole.Alt, 0.0, 0.001) {
		t.Errorf("North Pole Alt: expected 0 km, got %v km", llaPole.Alt)
	}
}

// BenchmarkECIToECEF измеряет производительность преобразования ECI→ECEF.
func BenchmarkECIToECEF(b *testing.B) {
	testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	eci := &ECIPosition{
		X: -4400.594, Y: 1932.870, Z: 4760.712,
		Time: testTime,
	}

	for b.Loop() {
		ECIToECEF(eci)
	}
}

// BenchmarkECEFToLLA измеряет производительность преобразования ECEF→LLA.
func BenchmarkECEFToLLA(b *testing.B) {
	ecef := &ECEFPosition{X: 1000.0, Y: 2000.0, Z: 6000.0}

	for b.Loop() {
		ECEFToLLA(ecef)
	}
}

// BenchmarkObserverGetAER измеряет производительность полного преобразования ECI→AER.
func BenchmarkObserverGetAER(b *testing.B) {
	observer := NewObserver(55.7558, 37.6173, 0.156)
	testTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	eci := &ECIPosition{
		X: -4400.594, Y: 1932.870, Z: 4760.712,
		Time: testTime,
	}

	for b.Loop() {
		observer.GetAER(eci)
	}
}
