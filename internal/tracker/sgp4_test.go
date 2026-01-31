package tracker

import (
	"math"
	"testing"
	"time"
)

// Эталонный TLE для ISS (ZARYA) — для SGP4 тестов.
// Используем те же TLE что и в tle_test.go (с валидными checksum).
var (
	sgp4TestISSName  = "ISS (ZARYA)"
	sgp4TestISSLine1 = issLine1 // из tle_test.go.
	sgp4TestISSLine2 = issLine2 // из tle_test.go.
)

// TestNewPropagator проверяет создание Propagator из TLE.
func TestNewPropagator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tle     *TLE
		wantErr bool
	}{
		{
			name: "valid ISS TLE",
			tle: &TLE{
				Name:    sgp4TestISSName,
				Line1:   sgp4TestISSLine1,
				Line2:   sgp4TestISSLine2,
				NoradID: 25544,
			},
			wantErr: false,
		},
		{
			name:    "nil TLE",
			tle:     nil,
			wantErr: true,
		},
		{
			name: "missing Line1",
			tle: &TLE{
				Name:  sgp4TestISSName,
				Line1: "",
				Line2: sgp4TestISSLine2,
			},
			wantErr: true,
		},
		{
			name: "missing Line2",
			tle: &TLE{
				Name:  sgp4TestISSName,
				Line1: sgp4TestISSLine1,
				Line2: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prop, err := NewPropagator(tt.tle)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewPropagator() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr && prop == nil {
				t.Error("NewPropagator() returned nil without error")
			}

			if !tt.wantErr && prop.TLE() != tt.tle {
				t.Error("NewPropagator() TLE mismatch")
			}
		})
	}
}

// TestNewPropagatorFromParsedTLE проверяет создание Propagator из распарсенного TLE.
func TestNewPropagatorFromParsedTLE(t *testing.T) {
	t.Parallel()

	lines := []string{sgp4TestISSName, sgp4TestISSLine1, sgp4TestISSLine2}

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}

	prop, err := NewPropagator(tle)
	if err != nil {
		t.Fatalf("NewPropagator() error = %v", err)
	}

	const expectedNoradID = 25544
	if prop.TLE().NoradID != expectedNoradID {
		t.Errorf("Expected NORAD ID %d, got %d", expectedNoradID, prop.TLE().NoradID)
	}
}

// TestPropagate проверяет базовую пропагацию.
func TestPropagate(t *testing.T) {
	t.Parallel()

	prop := createTestPropagator(t)

	// Пропагируем на эпоху TLE.
	// Epoch: 24001.50000000 = 1 января 2024, 12:00 UTC.
	testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	pos, err := prop.Propagate(testTime)
	if err != nil {
		t.Fatalf("Propagate() error = %v", err)
	}

	// Проверяем, что позиция разумная для LEO спутника.
	// ISS на высоте ~420 км, радиус Земли ~6371 км.
	// Ожидаемое расстояние от центра: 6371 + 420 = 6791 км (±100 км).
	distance := pos.Magnitude()

	const minDistance, maxDistance = 6600.0, 7000.0
	if distance < minDistance || distance > maxDistance {
		t.Errorf("Distance from Earth center = %.2f km, expected ~6791 km", distance)
	}

	// Проверяем высоту.
	altitude := pos.Altitude()

	const minAltitude, maxAltitude = 350.0, 500.0
	if altitude < minAltitude || altitude > maxAltitude {
		t.Errorf("Altitude = %.2f km, expected %v-%v km for ISS", altitude, minAltitude, maxAltitude)
	}

	// Проверяем скорость (ISS ~7.66 км/с).
	speed := pos.Speed()

	const minSpeed, maxSpeed = 7.0, 8.0
	if speed < minSpeed || speed > maxSpeed {
		t.Errorf("Speed = %.4f km/s, expected ~7.66 km/s for ISS", speed)
	}

	// Проверяем время.
	if !pos.Time.Equal(testTime) {
		t.Errorf("Time mismatch: got %v, want %v", pos.Time, testTime)
	}
}

// TestPropagateMultipleTimes проверяет пропагацию на разные моменты времени.
func TestPropagateMultipleTimes(t *testing.T) {
	t.Parallel()

	prop := createTestPropagator(t)
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Пропагируем на несколько моментов времени.
	times := []time.Time{
		baseTime,
		baseTime.Add(10 * time.Minute),
		baseTime.Add(30 * time.Minute),
		baseTime.Add(1 * time.Hour),
	}

	positions := propagateMultiple(t, prop, times)

	// Проверяем, что позиции различаются.
	checkPositionsDiffer(t, positions)

	// Проверяем, что все позиции в разумных пределах.
	checkPositionsInRange(t, positions)
}

// TestPropagateRange проверяет пропагацию на интервале.
func TestPropagateRange(t *testing.T) {
	t.Parallel()

	prop := createTestPropagator(t)

	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(1 * time.Hour)
	step := 10 * time.Minute

	positions, err := prop.PropagateRange(start, end, step)
	if err != nil {
		t.Fatalf("PropagateRange() error = %v", err)
	}

	// Ожидаем 7 точек: 12:00, 12:10, 12:20, 12:30, 12:40, 12:50, 13:00.
	const expectedCount = 7
	if len(positions) != expectedCount {
		t.Errorf("PropagateRange() returned %d positions, expected %d", len(positions), expectedCount)
	}

	// Проверяем последовательность времён.
	for i, pos := range positions {
		expectedTime := start.Add(time.Duration(i) * step)
		if !pos.Time.Equal(expectedTime) {
			t.Errorf("Position[%d] time = %v, expected %v", i, pos.Time, expectedTime)
		}
	}
}

// TestPropagateRangeInvalidStep проверяет обработку некорректного шага.
func TestPropagateRangeInvalidStep(t *testing.T) {
	t.Parallel()

	prop := createTestPropagator(t)

	start := time.Now()
	end := start.Add(1 * time.Hour)

	// Нулевой шаг.
	_, err := prop.PropagateRange(start, end, 0)
	if err == nil {
		t.Error("PropagateRange() should fail with zero step")
	}

	// Отрицательный шаг.
	_, err = prop.PropagateRange(start, end, -1*time.Minute)
	if err == nil {
		t.Error("PropagateRange() should fail with negative step")
	}
}

// TestGravityModels проверяет разные модели гравитации.
func TestGravityModels(t *testing.T) {
	t.Parallel()

	tle := createTestTLE()
	testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// WGS72.
	prop72, err := NewPropagatorWithGravity(tle, GravityWGS72)
	if err != nil {
		t.Fatalf("NewPropagatorWithGravity(WGS72) error = %v", err)
	}

	pos72, err := prop72.Propagate(testTime)
	if err != nil {
		t.Fatalf("Propagate(WGS72) error = %v", err)
	}

	// WGS84.
	prop84, err := NewPropagatorWithGravity(tle, GravityWGS84)
	if err != nil {
		t.Fatalf("NewPropagatorWithGravity(WGS84) error = %v", err)
	}

	pos84, err := prop84.Propagate(testTime)
	if err != nil {
		t.Fatalf("Propagate(WGS84) error = %v", err)
	}

	// Проверяем, что обе модели дают результат.
	const minMag, maxMag = 6600.0, 7000.0

	if pos72.Magnitude() < minMag || pos72.Magnitude() > maxMag {
		t.Errorf("WGS72 position magnitude = %.2f km, out of range", pos72.Magnitude())
	}

	if pos84.Magnitude() < minMag || pos84.Magnitude() > maxMag {
		t.Errorf("WGS84 position magnitude = %.2f km, out of range", pos84.Magnitude())
	}

	// Модели должны давать слегка разные результаты (но разница очень мала для LEO).
	t.Logf("WGS72: X=%.3f, Y=%.3f, Z=%.3f", pos72.X, pos72.Y, pos72.Z)
	t.Logf("WGS84: X=%.3f, Y=%.3f, Z=%.3f", pos84.X, pos84.Y, pos84.Z)
}

// TestGMST проверяет расчёт GMST.
func TestGMST(t *testing.T) {
	t.Parallel()

	// J2000.0 epoch: 1 January 2000, 12:00:00 TT.
	// GMST at J2000.0 ≈ 18.697374558 hours ≈ 4.89496 radians.
	j2000 := time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)
	gmst := GMST(j2000)

	// GMST должен быть в диапазоне 0-2π.
	twoPi := 2 * math.Pi
	if gmst < 0 || gmst > twoPi {
		t.Errorf("GMST = %f, expected in range [0, 2π]", gmst)
	}

	t.Logf("GMST at J2000.0: %.6f radians (%.4f hours)", gmst, gmst*12/math.Pi)
}

// TestJulianDay проверяет расчёт юлианской даты.
func TestJulianDay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		time     time.Time
		expected float64
	}{
		{
			name:     "J2000.0 epoch",
			time:     time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC),
			expected: 2451545.0,
		},
		{
			name:     "1 Jan 2024 midnight",
			time:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: 2460310.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			jd := JulianDay(tt.time)
			diff := math.Abs(jd - tt.expected)

			// Допускаем небольшую погрешность (< 0.001 дня ≈ 1.4 минуты).
			const maxDiff = 0.001
			if diff > maxDiff {
				t.Errorf("JulianDay(%v) = %.4f, expected %.4f (diff %.6f)", tt.time, jd, tt.expected, diff)
			}
		})
	}
}

// TestECIPositionMethods проверяет методы ECIPosition.
func TestECIPositionMethods(t *testing.T) {
	t.Parallel()

	pos := &ECIPosition{
		X:    4000.0,
		Y:    3000.0,
		Z:    5000.0,
		Vx:   5.0,
		Vy:   4.0,
		Vz:   3.0,
		Time: time.Now(),
	}

	// Magnitude: sqrt(4000² + 3000² + 5000²) = sqrt(16M + 9M + 25M) = sqrt(50M) ≈ 7071.07 km.
	expectedMag := math.Sqrt(4000*4000 + 3000*3000 + 5000*5000)
	if math.Abs(pos.Magnitude()-expectedMag) > 0.1 {
		t.Errorf("Magnitude() = %.2f, expected %.2f", pos.Magnitude(), expectedMag)
	}

	// Speed: sqrt(5² + 4² + 3²) = sqrt(25 + 16 + 9) = sqrt(50) ≈ 7.07 km/s.
	expectedSpeed := math.Sqrt(5*5 + 4*4 + 3*3)
	if math.Abs(pos.Speed()-expectedSpeed) > 0.01 {
		t.Errorf("Speed() = %.4f, expected %.4f", pos.Speed(), expectedSpeed)
	}

	// Altitude: Magnitude - 6371 km.
	const earthRadius = 6371.0

	expectedAlt := expectedMag - earthRadius
	if math.Abs(pos.Altitude()-expectedAlt) > 0.1 {
		t.Errorf("Altitude() = %.2f, expected %.2f", pos.Altitude(), expectedAlt)
	}

	// String.
	str := pos.String()
	if str == "" {
		t.Error("String() returned empty string")
	}

	t.Logf("ECIPosition.String(): %s", str)
}

// BenchmarkPropagate измеряет производительность пропагации.
func BenchmarkPropagate(b *testing.B) {
	tle := createTestTLE()

	prop, err := NewPropagator(tle)
	if err != nil {
		b.Fatalf("NewPropagator() error = %v", err)
	}

	testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	i := 0

	b.ResetTimer()

	for b.Loop() {
		_, err := prop.Propagate(testTime.Add(time.Duration(i) * time.Second))
		if err != nil {
			b.Fatalf("Propagate() error = %v", err)
		}

		i++
	}
}

// BenchmarkNewPropagator измеряет производительность создания Propagator.
func BenchmarkNewPropagator(b *testing.B) {
	tle := createTestTLE()

	b.ResetTimer()

	for b.Loop() {
		_, err := NewPropagator(tle)
		if err != nil {
			b.Fatalf("NewPropagator() error = %v", err)
		}
	}
}

// --- Вспомогательные функции ---

// createTestTLE создаёт TLE для тестов.
func createTestTLE() *TLE {
	return &TLE{
		Name:    sgp4TestISSName,
		Line1:   sgp4TestISSLine1,
		Line2:   sgp4TestISSLine2,
		NoradID: 25544,
	}
}

// createTestPropagator создаёт Propagator для тестов.
func createTestPropagator(t *testing.T) *Propagator {
	t.Helper()

	tle := createTestTLE()

	prop, err := NewPropagator(tle)
	if err != nil {
		t.Fatalf("NewPropagator() error = %v", err)
	}

	return prop
}

// propagateMultiple пропагирует на несколько моментов времени.
func propagateMultiple(t *testing.T, prop *Propagator, times []time.Time) []*ECIPosition {
	t.Helper()

	positions := make([]*ECIPosition, 0, len(times))

	for _, tm := range times {
		pos, err := prop.Propagate(tm)
		if err != nil {
			t.Fatalf("Propagate(%v) error = %v", tm, err)
		}

		positions = append(positions, pos)
	}

	return positions
}

// checkPositionsDiffer проверяет, что позиции различаются.
func checkPositionsDiffer(t *testing.T, positions []*ECIPosition) {
	t.Helper()

	for i := 1; i < len(positions); i++ {
		if positions[i].X == positions[0].X &&
			positions[i].Y == positions[0].Y &&
			positions[i].Z == positions[0].Z {
			t.Error("Positions should differ at different times")
		}
	}
}

// checkPositionsInRange проверяет, что позиции в разумных пределах.
func checkPositionsInRange(t *testing.T, positions []*ECIPosition) {
	t.Helper()

	const minDistance, maxDistance = 6600.0, 7000.0

	for i, pos := range positions {
		distance := pos.Magnitude()
		if distance < minDistance || distance > maxDistance {
			t.Errorf("Position[%d] distance = %.2f km, expected %v-%v km", i, distance, minDistance, maxDistance)
		}
	}
}
