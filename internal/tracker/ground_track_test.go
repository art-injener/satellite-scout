package tracker

import (
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"
)

// makeTLELineGT добавляет контрольную сумму к 68-символьной строке TLE.
func makeTLELineGT(line68 string) string {
	if len(line68) != 68 {
		panic(fmt.Sprintf("line must be 68 chars, got %d", len(line68)))
	}

	checksum := calculateChecksum(line68)

	return line68 + strconv.Itoa(checksum)
}

// Эталонные TLE для тестов (с автоматически рассчитанными контрольными суммами).

// gtISSLines — МКС (наклонение 51.6°, период ~92 мин, LEO ~420 км).
var gtISSLines = []string{
	"ISS (ZARYA)",
	makeTLELineGT("1 25544U 98067A   24001.50000000  .00016717  00000-0  10270-3 0  999"),
	makeTLELineGT("2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.4981557142340"),
}

// gtPolarLines — полярная орбита (наклонение ~98°, SSO, Meteor-M2).
var gtPolarLines = []string{
	"METEOR-M2",
	makeTLELineGT("1 40069U 14037A   24001.50000000  .00000123  00000-0  12345-4 0  999"),
	makeTLELineGT("2 40069  98.5200  45.6789 0001234 123.4567 236.7890 14.2098765432109"),
}

// gtGEOLines — геостационарный спутник (MeanMotion ≈ 1.0).
var gtGEOLines = []string{
	"EUTELSAT 36B",
	makeTLELineGT("1 25924U 99059A   24001.50000000  .00000115  00000-0  00000-0 0  999"),
	makeTLELineGT("2 25924   0.0400 275.4760 0004080 185.0800  56.1900  1.0027000089900"),
}

// parseTestTLE — вспомогательная функция для парсинга TLE в тестах.
func parseTestTLE(t *testing.T, lines []string) *TLE {
	t.Helper()

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE failed: %v", err)
	}

	return tle
}

// --- IsGeostationary ---

func TestIsGeostationary_GEO(t *testing.T) {
	tle := parseTestTLE(t, gtGEOLines)

	if !IsGeostationary(tle) {
		t.Errorf("expected GEO satellite (MeanMotion=%.4f), got false", tle.MeanMotion)
	}
}

func TestIsGeostationary_LEO(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)

	if IsGeostationary(tle) {
		t.Errorf("ISS (MeanMotion=%.4f) should NOT be geostationary", tle.MeanMotion)
	}
}

func TestIsGeostationary_Nil(t *testing.T) {
	if IsGeostationary(nil) {
		t.Error("expected false for nil TLE")
	}
}

// --- Генерация наземной трассы спутника ---

func TestGenerateGroundTrack_ISS(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch

	step := 30 * time.Second
	period := time.Duration(tle.OrbitalPeriod() * float64(time.Minute))
	start := now.Add(-1 * period)
	end := now.Add(3 * period)

	gt, err := GenerateGroundTrack(tle, start, end, now, step)
	if err != nil {
		t.Fatalf("GenerateGroundTrack failed: %v", err)
	}

	// Проверяем, что трасса орбиты не пустая.
	total := gt.TotalPoints()
	if total == 0 {
		t.Fatal("ground track has zero points")
	}

	// Ожидаемое количество точек: ~4 периода / 30 сек ≈ 736 (±2 из-за интерполяции).
	expectedMin := 700
	expectedMax := 800
	if total < expectedMin || total > expectedMax {
		t.Errorf("expected %d-%d points, got %d", expectedMin, expectedMax, total)
	}

	// Проверяем, что есть и пройденный, и предстоящий участки.
	if len(gt.Past) == 0 {
		t.Error("expected non-empty past segments")
	}

	if len(gt.Future) == 0 {
		t.Error("expected non-empty future segments")
	}

	// Проверяем, что NoradID заполнен.
	if gt.NoradID != 25544 {
		t.Errorf("expected NoradID=25544, got %d", gt.NoradID)
	}

	// Проверяем диапазон координат.
	for _, seg := range append(gt.Past, gt.Future...) {
		for _, p := range seg {
			if p.Lat < -90 || p.Lat > 90 {
				t.Errorf("lat out of range: %.4f", p.Lat)
			}

			if p.Lon < -180 || p.Lon > 180 {
				t.Errorf("lon out of range: %.4f", p.Lon)
			}
		}
	}
}

func TestGenerateGroundTrack_ISS_LatitudeRange(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch
	step := 30 * time.Second
	period := time.Duration(tle.OrbitalPeriod() * float64(time.Minute))

	gt, err := GenerateGroundTrack(tle, now, now.Add(period), now, step)
	if err != nil {
		t.Fatalf("GenerateGroundTrack failed: %v", err)
	}

	// ISS с наклонением 51.6° не должен заходить за ±52°.
	maxLat := 0.0

	for _, seg := range gt.Future {
		for _, p := range seg {
			if math.Abs(p.Lat) > maxLat {
				maxLat = math.Abs(p.Lat)
			}
		}
	}

	if maxLat > 53.0 {
		t.Errorf("ISS max latitude %.2f° exceeds inclination 51.6° + margin", maxLat)
	}

	if maxLat < 40.0 {
		t.Errorf("ISS max latitude %.2f° is too low for inclination 51.6°", maxLat)
	}
}

func TestGenerateGroundTrack_PolarOrbit(t *testing.T) {
	tle := parseTestTLE(t, gtPolarLines)
	now := tle.Epoch
	step := 30 * time.Second
	period := time.Duration(tle.OrbitalPeriod() * float64(time.Minute))

	gt, err := GenerateGroundTrack(tle, now, now.Add(period), now, step)
	if err != nil {
		t.Fatalf("GenerateGroundTrack failed: %v", err)
	}

	// Полярная орбита (98.7°) должна проходить через высокие широты.
	maxLat := 0.0

	for _, seg := range gt.Future {
		for _, p := range seg {
			if math.Abs(p.Lat) > maxLat {
				maxLat = math.Abs(p.Lat)
			}
		}
	}

	if maxLat < 80.0 {
		t.Errorf("polar orbit max latitude %.2f° is too low", maxLat)
	}
}

func TestGenerateGroundTrack_GEO(t *testing.T) {
	tle := parseTestTLE(t, gtGEOLines)
	now := tle.Epoch
	step := 60 * time.Second

	gt, err := GenerateGroundTrack(tle, now, now.Add(24*time.Hour), now, step)
	if err != nil {
		t.Fatalf("GenerateGroundTrack failed: %v", err)
	}

	// GEO спутник: широта ≈ 0°, долгота ≈ const (±несколько градусов).
	// Проверяем, что разброс долготы мал.
	var minLon, maxLon float64
	first := true

	allPoints := gt.Points()
	for _, p := range allPoints {
		if first {
			minLon = p.Lon
			maxLon = p.Lon
			first = false
		}

		if p.Lon < minLon {
			minLon = p.Lon
		}

		if p.Lon > maxLon {
			maxLon = p.Lon
		}
	}

	// Разброс долготы для GEO должен быть мал (< 1° для почти идеального GEO).
	lonSpread := maxLon - minLon
	if lonSpread > 5.0 {
		t.Errorf("GEO lon spread %.2f° is too large (expected < 5°)", lonSpread)
	}

	// Широта должна быть близка к 0°.
	for _, p := range allPoints {
		if math.Abs(p.Lat) > 2.0 {
			t.Errorf("GEO latitude %.2f° is too far from equator", p.Lat)
			break
		}
	}
}

// --- Антимеридиан ---

func TestSplitAtAntimeridian_NoCrossing(t *testing.T) {
	points := []TrackPoint{
		{Lon: 10.0, Lat: 50.0, TS: 1000},
		{Lon: 15.0, Lat: 50.5, TS: 2000},
		{Lon: 20.0, Lat: 51.0, TS: 3000},
	}

	segments := splitAtAntimeridian(points)

	if len(segments) != 1 {
		t.Errorf("expected 1 segment, got %d", len(segments))
	}

	if len(segments[0]) != 3 {
		t.Errorf("expected 3 points in segment, got %d", len(segments[0]))
	}
}

func TestSplitAtAntimeridian_SingleCrossing(t *testing.T) {
	// Переход через +180° (восток → запад).
	points := []TrackPoint{
		{Lon: 170.0, Lat: 40.0, TS: 1000},
		{Lon: 175.0, Lat: 42.0, TS: 2000},
		{Lon: -175.0, Lat: 44.0, TS: 3000}, // Перескок через антимеридиан.
		{Lon: -170.0, Lat: 46.0, TS: 4000},
	}

	segments := splitAtAntimeridian(points)

	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}

	// Первый сегмент должен заканчиваться на +180°.
	lastOfFirst := segments[0][len(segments[0])-1]
	if lastOfFirst.Lon != 180.0 {
		t.Errorf("first segment should end at +180°, got %.2f°", lastOfFirst.Lon)
	}

	// Второй сегмент должен начинаться с -180°.
	firstOfSecond := segments[1][0]
	if firstOfSecond.Lon != -180.0 {
		t.Errorf("second segment should start at -180°, got %.2f°", firstOfSecond.Lon)
	}

	// Интерполированные точки должны иметь одинаковую широту.
	if math.Abs(lastOfFirst.Lat-firstOfSecond.Lat) > 0.01 {
		t.Errorf("interpolated latitudes differ: %.4f vs %.4f", lastOfFirst.Lat, firstOfSecond.Lat)
	}
}

func TestSplitAtAntimeridian_ReverseCrossing(t *testing.T) {
	// Переход через -180° (запад → восток).
	points := []TrackPoint{
		{Lon: -170.0, Lat: 40.0, TS: 1000},
		{Lon: -175.0, Lat: 42.0, TS: 2000},
		{Lon: 175.0, Lat: 44.0, TS: 3000}, // Перескок через антимеридиан.
		{Lon: 170.0, Lat: 46.0, TS: 4000},
	}

	segments := splitAtAntimeridian(points)

	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}

	// Первый сегмент должен заканчиваться на -180°.
	lastOfFirst := segments[0][len(segments[0])-1]
	if lastOfFirst.Lon != -180.0 {
		t.Errorf("first segment should end at -180°, got %.2f°", lastOfFirst.Lon)
	}

	// Второй сегмент должен начинаться с +180°.
	firstOfSecond := segments[1][0]
	if firstOfSecond.Lon != 180.0 {
		t.Errorf("second segment should start at +180°, got %.2f°", firstOfSecond.Lon)
	}
}

func TestSplitAtAntimeridian_MultipleCrossings(t *testing.T) {
	// Два пересечения антимеридиана (ISS-подобная орбита за несколько витков).
	points := []TrackPoint{
		{Lon: 160.0, Lat: 30.0, TS: 1000},
		{Lon: 175.0, Lat: 35.0, TS: 2000},
		{Lon: -170.0, Lat: 40.0, TS: 3000}, // Первое пересечение.
		{Lon: -150.0, Lat: 42.0, TS: 4000},
		{Lon: -175.0, Lat: 44.0, TS: 5000},
		{Lon: 170.0, Lat: 46.0, TS: 6000}, // Второе пересечение.
		{Lon: 150.0, Lat: 48.0, TS: 7000},
	}

	segments := splitAtAntimeridian(points)

	if len(segments) != 3 {
		t.Errorf("expected 3 segments, got %d", len(segments))
	}
}

func TestSplitAtAntimeridian_Empty(t *testing.T) {
	segments := splitAtAntimeridian(nil)
	if segments != nil {
		t.Errorf("expected nil for empty input, got %v", segments)
	}
}

// --- Пройденный/Предстоящий участки ---

func TestSplitPastFuture_AllPast(t *testing.T) {
	segments := [][]TrackPoint{
		{
			{Lon: 10, Lat: 50, TS: 1000},
			{Lon: 15, Lat: 51, TS: 2000},
		},
	}

	past, future := splitPastFuture(segments, 5000)

	if len(past) != 1 {
		t.Errorf("expected 1 past segment, got %d", len(past))
	}

	if len(future) != 0 {
		t.Errorf("expected 0 future segments, got %d", len(future))
	}
}

func TestSplitPastFuture_AllFuture(t *testing.T) {
	segments := [][]TrackPoint{
		{
			{Lon: 10, Lat: 50, TS: 5000},
			{Lon: 15, Lat: 51, TS: 6000},
		},
	}

	past, future := splitPastFuture(segments, 1000)

	if len(past) != 0 {
		t.Errorf("expected 0 past segments, got %d", len(past))
	}

	if len(future) != 1 {
		t.Errorf("expected 1 future segment, got %d", len(future))
	}
}

func TestSplitPastFuture_Split(t *testing.T) {
	segments := [][]TrackPoint{
		{
			{Lon: 10, Lat: 50, TS: 1000},
			{Lon: 15, Lat: 51, TS: 2000},
			{Lon: 20, Lat: 52, TS: 3000},
			{Lon: 25, Lat: 53, TS: 4000},
		},
	}

	past, future := splitPastFuture(segments, 2500)

	if len(past) != 1 {
		t.Fatalf("expected 1 past segment, got %d", len(past))
	}

	if len(future) != 1 {
		t.Fatalf("expected 1 future segment, got %d", len(future))
	}

	// Пройденный: точки с ts < 2500 (ts=1000, ts=2000).
	if len(past[0]) != 2 {
		t.Errorf("expected 2 past points, got %d", len(past[0]))
	}

	// Предстоящий: точки с ts >= 2500 (ts=3000, ts=4000).
	if len(future[0]) != 2 {
		t.Errorf("expected 2 future points, got %d", len(future[0]))
	}
}

// --- Генерация трассы орбиты с автодиапазоном ---

func TestGenerateDefaultGroundTrack_ISS(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch

	gt, err := GenerateDefaultGroundTrack(tle, now)
	if err != nil {
		t.Fatalf("GenerateDefaultGroundTrack failed: %v", err)
	}

	total := gt.TotalPoints()
	if total == 0 {
		t.Fatal("ground track has zero points")
	}

	// 1.0 период (0.3 назад + 0.7 вперёд) × 92 мин / 30 сек ≈ 184 точки.
	if total < 170 || total > 210 {
		t.Errorf("unexpected point count: %d, expected 170-210", total)
	}
}

func TestGenerateDefaultGroundTrack_Nil(t *testing.T) {
	_, err := GenerateDefaultGroundTrack(nil, time.Now())
	if err == nil {
		t.Error("expected error for nil TLE")
	}
}

// --- Генерация трассы по окну долготы ---

// continuousLonSpan восстанавливает суммарный «развёрнутый» пролёт по долготе
// для упорядоченной по времени последовательности точек, где Lon ∈ [-180, 180).
// Учитывает «полюсные прыжки» (|lat| > 85°): при пролёте через полюс долгота
// скачкообразно меняется на ~180° (артефакт equirectangular-проекции, не реальный
// дрейф) — такие шаги пропускаются для корректного подсчёта продвижения по долготе.
func continuousLonSpan(points []TrackPoint) float64 {
	if len(points) < 2 {
		return 0
	}
	prev := points[0].Lon
	prevLat := points[0].Lat
	cont := points[0].Lon
	minC, maxC := cont, cont
	const polepass = 85.0
	for i := 1; i < len(points); i++ {
		raw := points[i].Lon
		lat := points[i].Lat
		isPole := math.Abs(lat) > polepass || math.Abs(prevLat) > polepass
		if !isPole {
			delta := raw - prev
			for delta > 180 {
				delta -= 360
			}
			for delta < -180 {
				delta += 360
			}
			cont += delta
			if cont < minC {
				minC = cont
			}
			if cont > maxC {
				maxC = cont
			}
		}
		prev = raw
		prevLat = lat
	}
	return maxC - minC
}

// TestGenerateGroundTrackByLonWindow_LEO_FullCoverage — для LEO (МКС) при
// observerLon=0 трасса должна покрывать ровно 360° по continuous lon (полное
// окно карты, ~360° − 2ε из-за boundary epsilon).
func TestGenerateGroundTrackByLonWindow_LEO_FullCoverage(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch

	gt, err := GenerateGroundTrackByLonWindow(tle, now, 0.0, DefaultGroundTrackStep)
	if err != nil {
		t.Fatalf("GenerateGroundTrackByLonWindow failed: %v", err)
	}

	span := continuousLonSpan(gt.Points())
	// Ожидаем ровно 360° − 2ε (по 0.01° с каждой стороны), допуск ±2° (дискретизация).
	if span < 358.0 || span > 360.5 {
		t.Errorf("LEO span = %.2f°, expected ~360° (358..360.5)", span)
	}
}

// continuousLonsFromAnchor восстанавливает continuous lon относительно точки-якоря
// (точка с TS == anchorMs или ближайшая к нему). Возвращает массив cont-lon
// (anchor имеет cont = 0). Обработка polepass'ов как в continuousLonSpan.
func continuousLonsFromAnchor(points []TrackPoint, anchorMs int64) []float64 {
	if len(points) == 0 {
		return nil
	}
	const polepass = 85.0
	out := make([]float64, len(points))
	out[0] = 0
	prev, prevLat := points[0].Lon, points[0].Lat
	cont := 0.0
	for i := 1; i < len(points); i++ {
		raw, lat := points[i].Lon, points[i].Lat
		if math.Abs(lat) <= polepass && math.Abs(prevLat) <= polepass {
			d := raw - prev
			for d > 180 {
				d -= 360
			}
			for d < -180 {
				d += 360
			}
			cont += d
		}
		out[i] = cont
		prev, prevLat = raw, lat
	}
	// Находим anchor (точку с минимальным |TS - anchorMs|), сдвигаем массив так,
	// чтобы её cont = 0.
	abs64 := func(v int64) int64 {
		if v < 0 {
			return -v
		}
		return v
	}
	bestIdx := 0
	bestDiff := abs64(points[0].TS - anchorMs)
	for i := 1; i < len(points); i++ {
		d := abs64(points[i].TS - anchorMs)
		if d < bestDiff {
			bestDiff = d
			bestIdx = i
		}
	}
	shift := out[bestIdx]
	for i := range out {
		out[i] -= shift
	}
	return out
}

// TestGenerateGroundTrackByLonWindow_LEO_OffsetObserver — observerLon=39° (Москва):
// окно [observerLon-180°, observerLon+180°] = [-141°, +219°] по continuous lon,
// первая точка near leftBound, последняя — near rightBound.
func TestGenerateGroundTrackByLonWindow_LEO_OffsetObserver(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch
	const observerLon = 39.0

	gt, err := GenerateGroundTrackByLonWindow(tle, now, observerLon, DefaultGroundTrackStep)
	if err != nil {
		t.Fatalf("GenerateGroundTrackByLonWindow failed: %v", err)
	}

	all := gt.Points()
	if len(all) < 2 {
		t.Fatalf("expected ≥ 2 points, got %d", len(all))
	}

	span := continuousLonSpan(all)
	if span < 358.0 || span > 360.5 {
		t.Errorf("LEO offset observer span = %.2f°, expected ~360°", span)
	}

	// Все точки в окне [observerLon−180°, observerLon+180°] по continuous lon
	// (anchor — точка с TS=nowMs, она имеет cont=0; «window-relative cont» = anchor + (sat_lon_now − observerLon)).
	// Проверяем span ≈ 360°: достаточно для гарантии что точки в окне.
	conts := continuousLonsFromAnchor(all, now.UnixMilli())
	minC, maxC := conts[0], conts[0]
	for _, c := range conts {
		if c < minC {
			minC = c
		}
		if c > maxC {
			maxC = c
		}
	}
	// Допуск 0.6° = ε + дискретизация на двух крайних шагах.
	const tol = 0.6
	if maxC-minC < 358.0 || maxC-minC > 360.0+tol {
		t.Errorf("span (max-min) cont = %.3f°, expected ~360° (358..%.2f)", maxC-minC, 360.0+tol)
	}
}

// TestGenerateGroundTrackByLonWindow_Polar_TimeFallback — polar TLE: lon-clip
// не сработает (continuous lon растёт медленно из-за polepass), но time-fallback
// гарантирует остановку в пределах period × 1.07 в каждую сторону.
func TestGenerateGroundTrackByLonWindow_Polar_TimeFallback(t *testing.T) {
	tle := parseTestTLE(t, gtPolarLines)
	now := tle.Epoch
	periodMin := tle.OrbitalPeriod()
	maxHalfMs := int64(periodMin * timeFallbackHalfPeriodFraction * 60 * 1000)
	nowMs := now.UnixMilli()

	gt, err := GenerateGroundTrackByLonWindow(tle, now, 0.0, DefaultGroundTrackStep)
	if err != nil {
		t.Fatalf("GenerateGroundTrackByLonWindow failed (polar): %v", err)
	}

	for i, seg := range gt.Past {
		for j, p := range seg {
			deltaMs := nowMs - p.TS
			if deltaMs < 0 || deltaMs > maxHalfMs+int64(DefaultGroundTrackStep/time.Millisecond) {
				t.Errorf("polar past[%d][%d] dt=%dms exceeds time-fallback %dms",
					i, j, deltaMs, maxHalfMs)
			}
		}
	}
	for i, seg := range gt.Future {
		for j, p := range seg {
			deltaMs := p.TS - nowMs
			if deltaMs < 0 || deltaMs > maxHalfMs+int64(DefaultGroundTrackStep/time.Millisecond) {
				t.Errorf("polar future[%d][%d] dt=%dms exceeds time-fallback %dms",
					i, j, deltaMs, maxHalfMs)
			}
		}
	}
}

// TestGenerateGroundTrackByLonWindow_GEO_PointsLimit — GEO: continuous lon почти
// не меняется, lon-clip не срабатывает; должен ограничить maxPointsPerSide.
func TestGenerateGroundTrackByLonWindow_GEO_PointsLimit(t *testing.T) {
	tle := parseTestTLE(t, gtGEOLines)
	now := tle.Epoch

	gt, err := GenerateGroundTrackByLonWindow(tle, now, 0.0, DefaultGroundTrackStep)
	if err != nil {
		t.Fatalf("GenerateGroundTrackByLonWindow failed (GEO): %v", err)
	}

	// Per-side limit = maxPointsPerSide; total ≤ 2 × (maxPointsPerSide+1) с учётом
	// nowPoint и возможной boundary-точки.
	total := gt.TotalPoints()
	if total > 2*(maxPointsPerSide+2) {
		t.Errorf("GEO total points = %d, expected ≤ %d (maxPointsPerSide × 2)",
			total, 2*(maxPointsPerSide+2))
	}
}

func TestGenerateGroundTrackByLonWindow_Nil(t *testing.T) {
	_, err := GenerateGroundTrackByLonWindow(nil, time.Now(), 0.0, DefaultGroundTrackStep)
	if err == nil {
		t.Error("expected error for nil TLE")
	}
}

func TestGenerateGroundTrackByLonWindow_InvalidStep(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	_, err := GenerateGroundTrackByLonWindow(tle, tle.Epoch, 0.0, 0)
	if err == nil {
		t.Error("expected error for zero step")
	}
}

// TestGenerateGroundTrackByLonWindow_PastFutureSplit — точки делятся по now.
func TestGenerateGroundTrackByLonWindow_PastFutureSplit(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch
	nowMs := now.UnixMilli()

	gt, err := GenerateGroundTrackByLonWindow(tle, now, 0.0, DefaultGroundTrackStep)
	if err != nil {
		t.Fatalf("GenerateGroundTrackByLonWindow failed: %v", err)
	}

	for i, seg := range gt.Past {
		for j, p := range seg {
			if p.TS >= nowMs {
				t.Errorf("past[%d][%d] TS=%d >= now=%d", i, j, p.TS, nowMs)
			}
		}
	}
	for i, seg := range gt.Future {
		for j, p := range seg {
			if p.TS < nowMs {
				t.Errorf("future[%d][%d] TS=%d < now=%d", i, j, p.TS, nowMs)
			}
		}
	}
}

// TestGenerateGroundTrackByLonWindow_BoundaryPoints — первая и последняя точки
// должны лежать близко к границам окна [observerLon−180°, observerLon+180°]
// по continuous lon. Окно — общее для всех KA (центр карты = observer), не
// симметричное относительно sat. Проверяем что суммарный span ≈ 360° и first
// и last на разных сторонах от sat.
func TestGenerateGroundTrackByLonWindow_BoundaryPoints(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch
	const observerLon = 0.0

	gt, err := GenerateGroundTrackByLonWindow(tle, now, observerLon, DefaultGroundTrackStep)
	if err != nil {
		t.Fatalf("GenerateGroundTrackByLonWindow failed: %v", err)
	}

	all := gt.Points()
	if len(all) < 4 {
		t.Fatalf("expected ≥ 4 points, got %d", len(all))
	}

	conts := continuousLonsFromAnchor(all, now.UnixMilli())
	first := conts[0]
	last := conts[len(conts)-1]

	// first и last должны быть на ПРОТИВОПОЛОЖНЫХ сторонах от anchor (одна < 0, другая > 0).
	if first*last > 0 {
		t.Errorf("first (%.2f) и last (%.2f) с одной стороны от anchor — ожидалось разные", first, last)
	}

	// Суммарный span (last - first) ≈ 360° с допуском ε + дискретизация.
	span := math.Abs(last - first)
	const tol = 0.6
	if span < 358.0 || span > 360.0+tol {
		t.Errorf("span first→last = %.3f°, expected ~360° (358..%.2f)", span, 360.0+tol)
	}
}

// --- Полный мат-симулятор фронтового project() ---

// projectXSim воспроизводит логику EarthView.project() для x-координаты с
// нормализацией dLon в [-180, 180). Используется в тестах чтобы проверить, на
// каком пикселе canvas окажется граничная точка трассы.
func projectXSim(lon, centerLon, width, zoom float64) float64 {
	if zoom == 0 {
		zoom = 1
	}
	dLon := lon - centerLon
	for dLon >= 180 {
		dLon -= 360
	}
	for dLon < -180 {
		dLon += 360
	}
	return (dLon/360.0)*width*zoom + width/2.0
}

// --- Табличные тесты для разных точек мира ---

// observerCases — представительный набор станций по всему миру для проверки
// что трасса корректна при любом observerLon.
var observerCases = []struct {
	name        string
	observerLon float64
}{
	{"Гринвич (0°)", 0.0},
	{"Лондон (0°W)", -0.13},
	{"Москва (37.6°E)", 37.6},
	{"Ростов-на-Дону (39.79°E)", 39.79},
	{"Дубай (55.3°E)", 55.3},
	{"Дели (77.2°E)", 77.2},
	{"Пекин (116.4°E)", 116.4},
	{"Токио (139.7°E)", 139.7},
	{"Сидней (151.2°E)", 151.2},
	{"Антимеридиан (180°)", 180.0},
	{"Антимеридиан (-180°)", -180.0},
	{"Сан-Франциско (-122.4°)", -122.4},
	{"Нью-Йорк (-74°)", -74.0},
	{"Рио (-43.2°)", -43.2},
}

// TestGenerateGroundTrackByLonWindow_AllObservers_LEO — для LEO (МКС) трасса
// должна правильно покрывать окно карты при ЛЮБОМ observerLon.
//
// Проверки:
//  1. span continuous lon ≈ 360° (полное покрытие окна).
//  2. first и last точки трассы (по time) на противоположных сторонах от sat_now.
//  3. boundary-точки проектируются ровно на края canvas (через симулятор project()).
//  4. past содержит только TS<now, future — TS≥now.
func TestGenerateGroundTrackByLonWindow_AllObservers_LEO(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch
	nowMs := now.UnixMilli()
	const canvasWidth = 1024.0

	for _, tc := range observerCases {
		t.Run(tc.name, func(t *testing.T) {
			gt, err := GenerateGroundTrackByLonWindow(tle, now, tc.observerLon, DefaultGroundTrackStep)
			if err != nil {
				t.Fatalf("GenerateGroundTrackByLonWindow failed: %v", err)
			}

			all := gt.Points()
			if len(all) < 4 {
				t.Fatalf("expected ≥ 4 points, got %d", len(all))
			}

			// 1. span ≈ 360°.
			span := continuousLonSpan(all)
			if span < 358.0 || span > 360.6 {
				t.Errorf("span = %.3f°, expected 358..360.6°", span)
			}

			// 2. first и last на противоположных сторонах от anchor (sat_now).
			conts := continuousLonsFromAnchor(all, nowMs)
			first, last := conts[0], conts[len(conts)-1]
			if first*last > 0 {
				t.Errorf("first cont=%.2f и last cont=%.2f с одной стороны от sat_now", first, last)
			}

			// 3. Boundary-точки на краях canvas (по проекции через project с center=observerLon).
			firstP := all[0]
			lastP := all[len(all)-1]
			xFirst := projectXSim(firstP.Lon, tc.observerLon, canvasWidth, 1.0)
			xLast := projectXSim(lastP.Lon, tc.observerLon, canvasWidth, 1.0)
			// Boundary должна быть очень близко (≤ 1px) к 0 или canvasWidth.
			isOnEdge := func(x float64) bool {
				return x <= 1.0 || x >= canvasWidth-1.0
			}
			if !isOnEdge(xFirst) {
				t.Errorf("first point lon=%.3f → x=%.2f (canvas %.0f), expected на краю (≤1 или ≥%.0f)",
					firstP.Lon, xFirst, canvasWidth, canvasWidth-1)
			}
			if !isOnEdge(xLast) {
				t.Errorf("last point lon=%.3f → x=%.2f (canvas %.0f), expected на краю",
					lastP.Lon, xLast, canvasWidth)
			}
			// Они должны быть на РАЗНЫХ краях.
			firstOnLeft := xFirst <= 1.0
			lastOnLeft := xLast <= 1.0
			if firstOnLeft == lastOnLeft {
				t.Errorf("first (x=%.2f) и last (x=%.2f) на одном крае canvas — ожидалось на разных",
					xFirst, xLast)
			}

			// 4. past — TS<now, future — TS≥now.
			for _, seg := range gt.Past {
				for _, p := range seg {
					if p.TS >= nowMs {
						t.Errorf("past point TS=%d ≥ now=%d", p.TS, nowMs)
					}
				}
			}
			for _, seg := range gt.Future {
				for _, p := range seg {
					if p.TS < nowMs {
						t.Errorf("future point TS=%d < now=%d", p.TS, nowMs)
					}
				}
			}
		})
	}
}

// TestGenerateGroundTrackByLonWindow_AllObservers_Polar — для полярного TLE
// (наклонение ~98°) наземная трасса пересекает полюса, и из-за polepass-фильтра
// continuous lon за один период покрывает не 360°, а ~90° + westingDeg.
// Поэтому здесь проверяем только консистентность данных и ограничения
// time-fallback (lon-clip не должен пропустить экстремальное переполнение).
func TestGenerateGroundTrackByLonWindow_AllObservers_Polar(t *testing.T) {
	tle := parseTestTLE(t, gtPolarLines)
	now := tle.Epoch
	nowMs := now.UnixMilli()
	maxHalfMs := int64(tle.OrbitalPeriod() * timeFallbackHalfPeriodFraction * 60 * 1000)

	for _, tc := range observerCases {
		t.Run(tc.name, func(t *testing.T) {
			gt, err := GenerateGroundTrackByLonWindow(tle, now, tc.observerLon, DefaultGroundTrackStep)
			if err != nil {
				t.Fatalf("GenerateGroundTrackByLonWindow failed: %v", err)
			}

			all := gt.Points()
			if len(all) < 4 {
				t.Fatalf("expected ≥ 4 points (polar), got %d", len(all))
			}

			// Все точки должны лежать в диапазоне [now-1.07period, now+1.07period]
			// (защита от выхода за time-fallback).
			tolMs := int64(DefaultGroundTrackStep / time.Millisecond)
			for _, seg := range gt.Past {
				for _, p := range seg {
					if nowMs-p.TS > maxHalfMs+tolMs {
						t.Errorf("polar past TS=%d — diff %dms > maxHalf %dms",
							p.TS, nowMs-p.TS, maxHalfMs)
					}
				}
			}
			for _, seg := range gt.Future {
				for _, p := range seg {
					if p.TS-nowMs > maxHalfMs+tolMs {
						t.Errorf("polar future TS=%d — diff %dms > maxHalf %dms",
							p.TS, p.TS-nowMs, maxHalfMs)
					}
				}
			}

			// Continuous lon span должен быть ≥ 90° (хотя бы половина витка
			// между полюсами) — иначе трасса будет совсем короткой.
			span := continuousLonSpan(all)
			if span < 90.0 {
				t.Errorf("polar continuous lon span = %.2f° < 90° — слишком короткая трасса", span)
			}

			// past — TS<now, future — TS≥now (та же инвариантность что и для LEO).
			for _, seg := range gt.Past {
				for _, p := range seg {
					if p.TS >= nowMs {
						t.Errorf("polar past point TS=%d ≥ now=%d", p.TS, nowMs)
					}
				}
			}
			for _, seg := range gt.Future {
				for _, p := range seg {
					if p.TS < nowMs {
						t.Errorf("polar future point TS=%d < now=%d", p.TS, nowMs)
					}
				}
			}
		})
	}
}

// TestGenerateGroundTrackByLonWindow_AllObservers_GEO — для GEO трасса не
// должна разрастаться до тысяч точек: maxPointsPerSide ограничивает.
func TestGenerateGroundTrackByLonWindow_AllObservers_GEO(t *testing.T) {
	tle := parseTestTLE(t, gtGEOLines)
	now := tle.Epoch

	for _, tc := range observerCases {
		t.Run(tc.name, func(t *testing.T) {
			gt, err := GenerateGroundTrackByLonWindow(tle, now, tc.observerLon, DefaultGroundTrackStep)
			if err != nil {
				t.Fatalf("GenerateGroundTrackByLonWindow failed: %v", err)
			}

			total := gt.TotalPoints()
			if total > 2*(maxPointsPerSide+2) {
				t.Errorf("GEO total points = %d > limit %d", total, 2*(maxPointsPerSide+2))
			}
		})
	}
}

// TestGenerateGroundTrackByLonWindow_NoExcessiveOrbits_LEO — span НЕ должен
// существенно превышать 360°: алгоритм не должен рисовать «полтора витка»
// (это и было исходной проблемой избыточности).
func TestGenerateGroundTrackByLonWindow_NoExcessiveOrbits_LEO(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch

	for _, tc := range observerCases {
		t.Run(tc.name, func(t *testing.T) {
			gt, err := GenerateGroundTrackByLonWindow(tle, now, tc.observerLon, DefaultGroundTrackStep)
			if err != nil {
				t.Fatalf("GenerateGroundTrackByLonWindow failed: %v", err)
			}

			span := continuousLonSpan(gt.Points())
			// Жёсткий потолок 365° — даже с дискретизацией и ε не должны
			// превышать. Если алгоритм рисует полтора витка, span будет ~540°.
			if span > 365.0 {
				t.Errorf("span = %.2f° > 365° — алгоритм рисует лишний виток", span)
			}
		})
	}
}

// --- Вспомогательные ---

func TestGroundTrack_Points(t *testing.T) {
	gt := &GroundTrack{
		Past: [][]TrackPoint{
			{{Lon: 1, Lat: 2, TS: 100}},
			{{Lon: 3, Lat: 4, TS: 200}},
		},
		Future: [][]TrackPoint{
			{{Lon: 5, Lat: 6, TS: 300}},
		},
	}

	points := gt.Points()
	if len(points) != 3 {
		t.Errorf("expected 3 points, got %d", len(points))
	}

	// Проверяем порядок: пройденный[0], пройденный[1], предстоящий[0].
	if points[0].Lon != 1 || points[1].Lon != 3 || points[2].Lon != 5 {
		t.Errorf("unexpected point order: %v", points)
	}
}

func TestGroundTrack_Points_Nil(t *testing.T) {
	var gt *GroundTrack
	points := gt.Points()

	if points != nil {
		t.Errorf("expected nil for nil GroundTrack, got %v", points)
	}
}

func TestGroundTrack_TotalPoints(t *testing.T) {
	gt := &GroundTrack{
		Past:   [][]TrackPoint{{{Lon: 1}, {Lon: 2}}},
		Future: [][]TrackPoint{{{Lon: 3}, {Lon: 4}, {Lon: 5}}},
	}

	if gt.TotalPoints() != 5 {
		t.Errorf("expected 5, got %d", gt.TotalPoints())
	}
}

func TestGenerateGroundTrack_InvalidStep(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch

	_, err := GenerateGroundTrack(tle, now, now.Add(time.Hour), now, 0)
	if err == nil {
		t.Error("expected error for zero step")
	}
}

func TestGenerateGroundTrack_EqualStartEnd(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch

	_, err := GenerateGroundTrack(tle, now, now, now, 30*time.Second)
	if err == nil {
		t.Error("expected error for equal start and end")
	}
}

func TestGenerateGroundTrack_ReversedStartEnd(t *testing.T) {
	tle := parseTestTLE(t, gtISSLines)
	now := tle.Epoch
	step := 30 * time.Second

	// start > end — должен автоматически поменять местами.
	gt, err := GenerateGroundTrack(tle, now.Add(time.Hour), now, now, step)
	if err != nil {
		t.Fatalf("expected auto-swap, got error: %v", err)
	}

	if gt.TotalPoints() == 0 {
		t.Error("expected non-empty track with reversed start/end")
	}
}

// --- Интерполяция ---

func TestInterpolateAntimeridian_EastToWest(t *testing.T) {
	p1 := TrackPoint{Lon: 175.0, Lat: 40.0, TS: 1000}
	p2 := TrackPoint{Lon: -175.0, Lat: 44.0, TS: 2000}

	b1, b2 := interpolateAntimeridian(p1, p2)

	if b1.Lon != 180.0 {
		t.Errorf("expected boundary lon +180, got %.2f", b1.Lon)
	}

	if b2.Lon != -180.0 {
		t.Errorf("expected boundary lon -180, got %.2f", b2.Lon)
	}

	// Широта должна быть между 40 и 44.
	if b1.Lat < 40.0 || b1.Lat > 44.0 {
		t.Errorf("interpolated lat %.4f out of range [40, 44]", b1.Lat)
	}

	// Обе граничные точки должны иметь одинаковую широту.
	if math.Abs(b1.Lat-b2.Lat) > 0.001 {
		t.Errorf("boundary latitudes differ: %.4f vs %.4f", b1.Lat, b2.Lat)
	}

	// Время должно быть между 1000 и 2000.
	if b1.TS < 1000 || b1.TS > 2000 {
		t.Errorf("interpolated ts %d out of range [1000, 2000]", b1.TS)
	}
}

func TestInterpolateAntimeridian_WestToEast(t *testing.T) {
	p1 := TrackPoint{Lon: -175.0, Lat: 40.0, TS: 1000}
	p2 := TrackPoint{Lon: 175.0, Lat: 44.0, TS: 2000}

	b1, b2 := interpolateAntimeridian(p1, p2)

	if b1.Lon != -180.0 {
		t.Errorf("expected boundary lon -180, got %.2f", b1.Lon)
	}

	if b2.Lon != 180.0 {
		t.Errorf("expected boundary lon +180, got %.2f", b2.Lon)
	}
}

// --- Benchmark ---

func BenchmarkGenerateDefaultGroundTrack_ISS(b *testing.B) {
	tle, err := ParseTLE(gtISSLines)
	if err != nil {
		b.Fatalf("ParseTLE failed: %v", err)
	}

	now := tle.Epoch

	b.ResetTimer()

	for b.Loop() {
		_, errGen := GenerateDefaultGroundTrack(tle, now)
		if errGen != nil {
			b.Fatalf("GenerateDefaultGroundTrack failed: %v", errGen)
		}
	}
}

func BenchmarkGenerateGroundTrack_SingleOrbit(b *testing.B) {
	tle, err := ParseTLE(gtISSLines)
	if err != nil {
		b.Fatalf("ParseTLE failed: %v", err)
	}

	now := tle.Epoch
	period := time.Duration(tle.OrbitalPeriod() * float64(time.Minute))
	step := 30 * time.Second

	b.ResetTimer()

	for b.Loop() {
		_, errGen := GenerateGroundTrack(tle, now, now.Add(period), now, step)
		if errGen != nil {
			b.Fatalf("GenerateGroundTrack failed: %v", errGen)
		}
	}
}
