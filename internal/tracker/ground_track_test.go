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

	// 4 периода × 92 мин / 30 сек ≈ 736 точек.
	if total < 700 || total > 800 {
		t.Errorf("unexpected point count: %d", total)
	}
}

func TestGenerateDefaultGroundTrack_Nil(t *testing.T) {
	_, err := GenerateDefaultGroundTrack(nil, time.Now())
	if err == nil {
		t.Error("expected error for nil TLE")
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
