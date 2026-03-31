package tracker

import (
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// makeTLELinePP добавляет контрольную сумму к 68-символьной строке TLE.
func makeTLELinePP(line68 string) string {
	if len(line68) != 68 {
		panic(fmt.Sprintf("line must be 68 chars, got %d", len(line68)))
	}

	checksum := calculateChecksum(line68)

	return line68 + strconv.Itoa(checksum)
}

// Эталонные TLE для тестов предсказания пролётов.

// ppISSLines — МКС (LEO, наклонение 51.6°, период ~92 мин).
var ppISSLines = []string{
	"ISS (ZARYA)",
	makeTLELinePP("1 25544U 98067A   24001.50000000  .00016717  00000-0  10270-3 0  999"),
	makeTLELinePP("2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.4981557142340"),
}

// ppPolarLines — полярная орбита (наклонение ~98°, Meteor-M2).
var ppPolarLines = []string{
	"METEOR-M2",
	makeTLELinePP("1 40069U 14037A   24001.50000000  .00000123  00000-0  12345-4 0  999"),
	makeTLELinePP("2 40069  98.5200  45.6789 0001234 123.4567 236.7890 14.2098765432109"),
}

// ppGEOLines — геостационарный спутник (MeanMotion ≈ 1.0).
var ppGEOLines = []string{
	"EUTELSAT 36B",
	makeTLELinePP("1 25924U 99059A   24001.50000000  .00000115  00000-0  00000-0 0  999"),
	makeTLELinePP("2 25924   0.0400 275.4760 0004080 185.0800  56.1900  1.0027000089900"),
}

// ppHighInclLines — высоконаклонная орбита (наклонение ~82°, Iridium-like).
var ppHighInclLines = []string{
	"IRIDIUM 33 DEB",
	makeTLELinePP("1 33777U 09005A   24001.50000000  .00000100  00000-0  50000-4 0  999"),
	makeTLELinePP("2 33777  82.4100 120.3456 0012345 234.5678 125.4321 14.3456789012345"),
}

func parsePPTLE(t *testing.T, lines []string) *TLE {
	t.Helper()

	tle, err := ParseTLE(lines)
	require.NoError(t, err)

	return tle
}

func makePPPropagator(t *testing.T, lines []string) *Propagator {
	t.Helper()

	tle := parsePPTLE(t, lines)
	prop, err := NewPropagator(tle)
	require.NoError(t, err)

	return prop
}

// Наблюдатель — Ростов-на-Дону (47.23°N, 39.72°E).
var ppObserver = NewObserver(47.23, 39.72, 0.08)

// Базовое время для тестов — эпоха TLE (1 января 2024).
var ppBaseTime = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

// --- Тесты PredictPasses ---

func TestPredictPasses_ISSFindsMultiplePasses(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	// ISS делает ~16 витков/сутки, из которых 4-6 видимы из одной точки.
	if len(passes) < 2 {
		t.Errorf("expected at least 2 ISS passes in 24h, got %d", len(passes))
	}

	if len(passes) > 12 {
		t.Errorf("too many ISS passes in 24h: %d (expected max ~12)", len(passes))
	}

	t.Logf("found %d ISS passes in 24h from observer at %.2f°N, %.2f°E",
		len(passes), ppObserver.Lat, ppObserver.Lon)

	for i, p := range passes {
		t.Logf("  pass %d: %s", i+1, p)
	}
}

func TestPredictPasses_PassStructure(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	if len(passes) == 0 {
		t.Fatal("no passes found")
	}

	for i, p := range passes {
		// AOS < TCA < LOS.
		if p.AOS >= p.TCA {
			t.Errorf("pass %d: AOS (%d) must be before TCA (%d)", i, p.AOS, p.TCA)
		}

		if p.TCA >= p.LOS {
			t.Errorf("pass %d: TCA (%d) must be before LOS (%d)", i, p.TCA, p.LOS)
		}

		// Азимуты в допустимом диапазоне.
		if p.AOSAz < 0 || p.AOSAz >= 360 {
			t.Errorf("pass %d: AOS azimuth %.1f out of range [0, 360)", i, p.AOSAz)
		}

		if p.LOSAz < 0 || p.LOSAz >= 360 {
			t.Errorf("pass %d: LOS azimuth %.1f out of range [0, 360)", i, p.LOSAz)
		}

		// TCA элевация выше минимума и в допустимом диапазоне.
		if p.TCAEl < DefaultMinElevation {
			t.Errorf("pass %d: TCA elevation %.1f° below min %.1f°", i, p.TCAEl, DefaultMinElevation)
		}

		if p.TCAEl > 90 {
			t.Errorf("pass %d: TCA elevation %.1f° above 90°", i, p.TCAEl)
		}

		// Длительность разумная: от 30 секунд до 20 минут для LEO.
		if p.Duration < 30 {
			t.Errorf("pass %d: duration %.1fs too short", i, p.Duration)
		}

		if p.Duration > 1200 {
			t.Errorf("pass %d: duration %.1fs too long for LEO", i, p.Duration)
		}

		// NoradID и имя заполнены.
		if p.NoradID != 25544 {
			t.Errorf("pass %d: expected NORAD 25544, got %d", i, p.NoradID)
		}

		if p.SatName != "ISS" {
			t.Errorf("pass %d: expected name 'ISS', got '%s'", i, p.SatName)
		}
		if p.SatAlias != "ZARYA" {
			t.Errorf("pass %d: expected alias 'ZARYA', got '%s'", i, p.SatAlias)
		}
	}
}

func TestPredictPasses_PassesSortedByAOS(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	for i := 1; i < len(passes); i++ {
		if passes[i].AOS < passes[i-1].AOS {
			t.Errorf("passes not sorted by AOS: pass %d AOS=%d, pass %d AOS=%d",
				i-1, passes[i-1].AOS, i, passes[i].AOS)
		}
	}
}

func TestPredictPasses_PassesNotOverlapping(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	for i := 1; i < len(passes); i++ {
		if passes[i].AOS < passes[i-1].LOS {
			t.Errorf("passes overlap: pass %d LOS=%d, pass %d AOS=%d",
				i-1, passes[i-1].LOS, i, passes[i].AOS)
		}
	}
}

func TestPredictPasses_SkyPathPresent(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	if len(passes) == 0 {
		t.Fatal("no passes found")
	}

	for i, p := range passes {
		// Каждый пролёт должен иметь SkyPath.
		if len(p.SkyPath) < 3 {
			t.Errorf("pass %d: SkyPath has %d points, expected at least 3", i, len(p.SkyPath))
			continue
		}

		// Все точки SkyPath в допустимых диапазонах.
		for j, pt := range p.SkyPath {
			if pt.Az < 0 || pt.Az >= 360 {
				t.Errorf("pass %d, point %d: azimuth %.1f out of [0, 360)", i, j, pt.Az)
			}

			// Элевация может быть чуть ниже 0 из-за округления.
			if pt.El < -1 || pt.El > 90.5 {
				t.Errorf("pass %d, point %d: elevation %.1f out of [-1, 90.5]", i, j, pt.El)
			}
		}

		// Точки SkyPath упорядочены по времени.
		for j := 1; j < len(p.SkyPath); j++ {
			if p.SkyPath[j].Time < p.SkyPath[j-1].Time {
				t.Errorf("pass %d: SkyPath not sorted by time at index %d", i, j)
			}
		}
	}
}

func TestPredictPasses_GEOSkipped(t *testing.T) {
	prop := makePPPropagator(t, ppGEOLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	if len(passes) != 0 {
		t.Errorf("expected 0 passes for GEO satellite, got %d", len(passes))
	}
}

func TestPredictPasses_PolarOrbit(t *testing.T) {
	prop := makePPPropagator(t, ppPolarLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	// Полярный спутник тоже должен иметь пролёты.
	if len(passes) < 1 {
		t.Errorf("expected at least 1 polar orbit pass in 24h, got %d", len(passes))
	}

	t.Logf("found %d polar orbit passes in 24h", len(passes))

	for i, p := range passes {
		t.Logf("  pass %d: %s", i+1, p)
	}
}

func TestPredictPasses_HighInclination(t *testing.T) {
	prop := makePPPropagator(t, ppHighInclLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	if len(passes) < 1 {
		t.Errorf("expected at least 1 high inclination pass in 24h, got %d", len(passes))
	}

	t.Logf("found %d high inclination passes in 24h", len(passes))
}

func TestPredictPasses_HigherMinElevation(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes5, err := PredictPasses(prop, ppObserver, start, end, 5.0)
	require.NoError(t, err)

	passes20, err := PredictPasses(prop, ppObserver, start, end, 20.0)
	require.NoError(t, err)

	// С более высоким минимумом элевации должно быть меньше или столько же пролётов.
	if len(passes20) > len(passes5) {
		t.Errorf("higher min elevation should give fewer passes: 5°→%d, 20°→%d",
			len(passes5), len(passes20))
	}

	// Все пролёты при 20° должны иметь TCAEl >= 20°.
	for i, p := range passes20 {
		if p.TCAEl < 20.0 {
			t.Errorf("pass %d: TCA elevation %.1f° < 20° min", i, p.TCAEl)
		}
	}

	t.Logf("passes with minEl=5°: %d, minEl=20°: %d", len(passes5), len(passes20))
}

func TestPredictPasses_ShortPeriod(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	// Только 2 часа.
	start := ppBaseTime
	end := start.Add(2 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	// За 2 часа может быть 0 или 1 пролёт ISS.
	if len(passes) > 3 {
		t.Errorf("too many ISS passes in 2h: %d (expected max ~2)", len(passes))
	}

	// Все пролёты в пределах заданного диапазона.
	for i, p := range passes {
		if p.AOS < start.UnixMilli() {
			t.Errorf("pass %d: AOS before start", i)
		}

		if p.LOS > end.UnixMilli()+2000 { // +2с допуск на бисекцию.
			t.Errorf("pass %d: LOS after end", i)
		}
	}

	t.Logf("found %d passes in 2h", len(passes))
}

// --- Тесты ошибок ---

func TestPredictPasses_NilPropagator(t *testing.T) {
	_, err := PredictPasses(nil, ppObserver, ppBaseTime, ppBaseTime.Add(time.Hour), DefaultMinElevation)
	if err == nil {
		t.Error("expected error for nil propagator")
	}
}

func TestPredictPasses_NilObserver(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)
	_, err := PredictPasses(prop, nil, ppBaseTime, ppBaseTime.Add(time.Hour), DefaultMinElevation)

	if err == nil {
		t.Error("expected error for nil observer")
	}
}

func TestPredictPasses_InvalidTimeRange(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	// end before start.
	_, err := PredictPasses(prop, ppObserver, ppBaseTime.Add(time.Hour), ppBaseTime, DefaultMinElevation)
	if err == nil {
		t.Error("expected error for end before start")
	}

	// start equals end.
	_, err = PredictPasses(prop, ppObserver, ppBaseTime, ppBaseTime, DefaultMinElevation)
	if err == nil {
		t.Error("expected error for start equals end")
	}
}

func TestPredictPasses_InvalidMinElevation(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)
	start := ppBaseTime
	end := start.Add(time.Hour)

	_, err := PredictPasses(prop, ppObserver, start, end, -5)
	if err == nil {
		t.Error("expected error for negative min elevation")
	}

	_, err = PredictPasses(prop, ppObserver, start, end, 95)
	if err == nil {
		t.Error("expected error for min elevation > 90")
	}
}

// --- Тест PredictPassesForTLE ---

func TestPredictPassesForTLE_ISS(t *testing.T) {
	tle := parsePPTLE(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPassesForTLE(tle, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	if len(passes) < 2 {
		t.Errorf("expected at least 2 passes, got %d", len(passes))
	}
}

func TestPredictPassesForTLE_NilTLE(t *testing.T) {
	_, err := PredictPassesForTLE(nil, ppObserver, ppBaseTime, ppBaseTime.Add(time.Hour), DefaultMinElevation)
	if err == nil {
		t.Error("expected error for nil TLE")
	}
}

// --- Тест ComputeOrbitNumber ---

func TestComputeOrbitNumber_ISS(t *testing.T) {
	tle := parsePPTLE(t, ppISSLines)

	// На эпоху TLE номер орбиты ≈ RevNumber.
	orbitAtEpoch := ComputeOrbitNumber(tle, tle.Epoch)
	t.Logf("ISS orbit at epoch: %d (TLE RevNumber: %d)", orbitAtEpoch, tle.RevNumber)

	// Номер орбиты на эпоху должен быть близок к RevNumber из TLE.
	diff := math.Abs(float64(orbitAtEpoch - tle.RevNumber))
	if diff > 2 {
		t.Errorf("orbit at epoch %d differs from TLE RevNumber %d by %v (expected ≤2)",
			orbitAtEpoch, tle.RevNumber, diff)
	}

	// Через 1 сутки: ISS ~15.5 витков/сутки → +15 или +16 витков.
	orbitNextDay := ComputeOrbitNumber(tle, tle.Epoch.Add(24*time.Hour))
	revPerDay := orbitNextDay - orbitAtEpoch
	t.Logf("ISS orbits per day: %d (MeanMotion: %.4f)", revPerDay, tle.MeanMotion)

	// ISS MeanMotion ~15.5, значит за сутки ~15-16 витков.
	if revPerDay < 14 || revPerDay > 17 {
		t.Errorf("expected 14-17 orbits per day, got %d", revPerDay)
	}
}

func TestComputeOrbitNumber_Monotonic(t *testing.T) {
	tle := parsePPTLE(t, ppISSLines)

	// Номера орбит должны монотонно возрастать.
	prev := ComputeOrbitNumber(tle, ppBaseTime)
	for h := 1; h <= 24; h++ {
		curr := ComputeOrbitNumber(tle, ppBaseTime.Add(time.Duration(h)*time.Hour))
		if curr < prev {
			t.Errorf("orbit number decreased: hour %d = %d, hour %d = %d", h-1, prev, h, curr)
		}
		prev = curr
	}
}

func TestComputeOrbitNumber_InPasses(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)
	if len(passes) < 2 {
		t.Skip("need at least 2 passes")
	}

	// Номера орбит в пролётах должны быть положительными и возрастающими.
	for i, p := range passes {
		if p.OrbitNumber <= 0 {
			t.Errorf("pass %d: orbit number %d should be positive", i, p.OrbitNumber)
		}

		if i > 0 && p.OrbitNumber < passes[i-1].OrbitNumber {
			t.Errorf("pass %d: orbit %d < previous pass orbit %d",
				i, p.OrbitNumber, passes[i-1].OrbitNumber)
		}
	}

	t.Logf("orbit numbers in passes: %v",
		func() []int {
			nums := make([]int, len(passes))
			for i, p := range passes {
				nums[i] = p.OrbitNumber
			}
			return nums
		}())
}

func TestComputeOrbitNumber_NilTLE(t *testing.T) {
	result := ComputeOrbitNumber(nil, ppBaseTime)
	if result != 0 {
		t.Errorf("expected 0 for nil TLE, got %d", result)
	}
}

// --- Тест azElToXY (полярная проекция) ---

func TestAzElToXY_CardinalDirections(t *testing.T) {
	// Проверяем ключевые направления на горизонте (El=0°).
	// На горизонте r=1, все точки на краю круга.
	tests := []struct {
		name  string
		az    float64 // градусы
		el    float64 // градусы
		wantX float64
		wantY float64
	}{
		// Горизонт: r = 1 - 0/(π/2) = 1
		{"N (Az=0°, El=0°)", 0, 0, 0, -1},     // Север = вверх в SVG (Y < 0)
		{"E (Az=90°, El=0°)", 90, 0, 1, 0},    // Восток = вправо
		{"S (Az=180°, El=0°)", 180, 0, 0, 1},  // Юг = вниз в SVG (Y > 0)
		{"W (Az=270°, El=0°)", 270, 0, -1, 0}, // Запад = влево
		// Зенит: r = 1 - 90/(90) = 0
		{"Зенит (Az=0°, El=90°)", 0, 90, 0, 0},   // Центр
		{"Зенит (Az=45°, El=90°)", 45, 90, 0, 0}, // Центр при любом азимуте
		// Полувысота: r = 1 - 45/90 = 0.5
		{"NE (Az=45°, El=45°)", 45, 45, 0.3536, -0.3536}, // r=0.5, phi=45°
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := azElToXY(tt.az, tt.el)
			if math.Abs(x-tt.wantX) > 0.01 || math.Abs(y-tt.wantY) > 0.01 {
				t.Errorf("azElToXY(az=%.0f°, el=%.0f°) = (%.4f, %.4f), want (%.4f, %.4f)",
					tt.az, tt.el, x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestAzElToXY_SkyPathHasXY(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)
	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)
	if len(passes) == 0 {
		t.Skip("need at least 1 pass")
	}

	p := passes[0]
	for i, pt := range p.SkyPath {
		// X, Y должны быть в [-1, 1].
		if pt.X < -1.01 || pt.X > 1.01 || pt.Y < -1.01 || pt.Y > 1.01 {
			t.Errorf("SkyPath[%d]: X=%.4f, Y=%.4f out of range [-1,1]", i, pt.X, pt.Y)
		}

		// Проверяем что X/Y соответствуют Az/El.
		expectedX, expectedY := azElToXY(pt.Az, pt.El)
		if math.Abs(pt.X-expectedX) > 0.001 || math.Abs(pt.Y-expectedY) > 0.001 {
			t.Errorf("SkyPath[%d]: X/Y (%.4f, %.4f) не соответствуют Az/El (%.1f°, %.1f°) → ожидалось (%.4f, %.4f)",
				i, pt.X, pt.Y, pt.Az, pt.El, expectedX, expectedY)
		}
	}
	t.Logf("Pass %s: %d SkyPath points, X range checked OK", p.SatName, len(p.SkyPath))
}

// --- Тест computeAER ---

func TestComputeAER_ReturnsValidAER(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	aer := computeAER(prop, ppObserver, ppBaseTime)
	if aer == nil {
		t.Fatal("computeAER returned nil")
	}

	// Азимут должен быть в [0, 2π).
	if aer.Az < 0 || aer.Az >= 2*math.Pi {
		t.Errorf("azimuth %.4f out of range [0, 2π)", aer.Az)
	}

	// Элевация может быть отрицательной (спутник за горизонтом).
	if aer.El < -math.Pi/2 || aer.El > math.Pi/2 {
		t.Errorf("elevation %.4f out of range [-π/2, π/2]", aer.El)
	}

	// Дальность положительная.
	if aer.Range <= 0 {
		t.Errorf("range %.1f should be positive", aer.Range)
	}
}

// --- Тест findMaxElevation ---

func TestFindMaxElevation_BetweenAOSAndLOS(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	passes, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)
	if len(passes) == 0 {
		t.Skip("no passes found, cannot test findMaxElevation")
	}

	p := passes[0]

	// TCA должен быть между AOS и LOS.
	if p.TCA < p.AOS || p.TCA > p.LOS {
		t.Errorf("TCA %d not between AOS %d and LOS %d", p.TCA, p.AOS, p.LOS)
	}

	// TCA элевация должна быть максимальной — проверяем, что AOS и LOS ниже.
	aosAER := computeAER(prop, ppObserver, p.AOSTime())
	losAER := computeAER(prop, ppObserver, p.LOSTime())

	if aosAER != nil && aosAER.ElDeg() > p.TCAEl {
		t.Errorf("AOS elevation %.1f° > TCA elevation %.1f°", aosAER.ElDeg(), p.TCAEl)
	}

	if losAER != nil && losAER.ElDeg() > p.TCAEl {
		t.Errorf("LOS elevation %.1f° > TCA elevation %.1f°", losAER.ElDeg(), p.TCAEl)
	}
}

// --- Тест с разными наблюдателями ---

func TestPredictPasses_DifferentObservers(t *testing.T) {
	prop := makePPPropagator(t, ppISSLines)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	// Наблюдатель на экваторе.
	equatorObs := NewObserver(0, 0, 0)
	passesEq, err := PredictPasses(prop, equatorObs, start, end, DefaultMinElevation)
	require.NoError(t, err)

	// Наблюдатель на средних широтах.
	passesMid, err := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
	require.NoError(t, err)

	// Наблюдатель на высокой широте (за пределами наклонения ISS — 51.6°).
	highLatObs := NewObserver(70, 0, 0)
	passesHigh, err := PredictPasses(prop, highLatObs, start, end, DefaultMinElevation)
	require.NoError(t, err)

	t.Logf("ISS passes in 24h: equator=%d, mid-lat (47°N)=%d, high-lat (70°N)=%d",
		len(passesEq), len(passesMid), len(passesHigh))

	// Все должны получить хотя бы некоторое количество пролётов (ISS виден до ~55-56°N).
	if len(passesEq) == 0 {
		t.Error("expected at least some passes at equator")
	}
}

// --- Benchmark ---

func BenchmarkPredictPasses_ISS24h(b *testing.B) {
	tle := parseBenchTLE(b, ppISSLines)

	prop, err := NewPropagator(tle)
	require.NoError(b, err)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	b.ResetTimer()

	for b.Loop() {
		_, benchErr := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
		require.NoError(b, benchErr)
	}
}

func BenchmarkPredictPasses_Polar24h(b *testing.B) {
	tle := parseBenchTLE(b, ppPolarLines)

	prop, err := NewPropagator(tle)
	require.NoError(b, err)

	start := ppBaseTime
	end := start.Add(24 * time.Hour)

	b.ResetTimer()

	for b.Loop() {
		_, benchErr := PredictPasses(prop, ppObserver, start, end, DefaultMinElevation)
		require.NoError(b, benchErr)
	}
}

// parseBenchTLE — вспомогательная функция для парсинга TLE в бенчмарках.
func parseBenchTLE(b *testing.B, lines []string) *TLE {
	b.Helper()

	tle, err := ParseTLE(lines)
	require.NoError(b, err)

	return tle
}
