package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// ISS (ZARYA) — реальные TLE строки (68 символов + контрольная сумма = 69).
var issLines = []string{
	"ISS (ZARYA)",
	"1 25544U 98067A   24001.50000000  .00016717  00000-0  10270-3 0  9997",
	"2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.49815571423401",
}

const issNoradID = 25544

// setupTLEStore создаёт TLEStore с ISS TLE для тестов.
func setupTLEStore(t *testing.T) *tracker.TLEStore {
	t.Helper()

	cfg := tracker.DefaultTLEStoreConfig()
	cfg.Groups = []string{} // Не загружаем с Celestrak.
	store := tracker.NewTLEStore(cfg)

	tle, err := tracker.ParseTLE(issLines)
	if err != nil {
		t.Fatalf("failed to parse ISS TLE: %v", err)
	}

	store.Add(tle)
	return store
}

// setupTrackingService создаёт SatelliteTrackingService для тестов.
func setupTrackingService(t *testing.T) (*SatelliteTrackingService, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	observer := tracker.NewObserver(47.315813, 39.788243, 0.070) // Ростов-на-Дону.

	svc := NewSatelliteTrackingService(hub, store, observer).WithInterval(100 * time.Millisecond)

	return svc, cancel
}

func TestNewSatelliteTrackingService(t *testing.T) {
	hub := handlers.NewSSEHub()
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(55.0, 37.0, 0.0)

	svc := NewSatelliteTrackingService(hub, store, observer)
	if svc == nil {
		t.Fatal("expected non-nil SatelliteTrackingService")
	}
	if svc.interval != DefaultTrackingInterval {
		t.Errorf("expected default interval %v, got %v", DefaultTrackingInterval, svc.interval)
	}
	if svc.TrackedCount() != 0 {
		t.Errorf("expected 0 tracked satellites, got %d", svc.TrackedCount())
	}
}

func TestWithInterval(t *testing.T) {
	hub := handlers.NewSSEHub()
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(55.0, 37.0, 0.0)

	svc := NewSatelliteTrackingService(hub, store, observer).WithInterval(500 * time.Millisecond)
	if svc.interval != 500*time.Millisecond {
		t.Errorf("expected 500ms interval, got %v", svc.interval)
	}

	// Невалидный интервал не должен менять значение.
	svc.WithInterval(0)
	if svc.interval != 500*time.Millisecond {
		t.Errorf("expected 500ms interval after invalid, got %v", svc.interval)
	}
}

func TestTrackSatellite(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	if svc.TrackedCount() != 1 {
		t.Errorf("expected 1 tracked satellite, got %d", svc.TrackedCount())
	}

	ids := svc.TrackedIDs()
	if len(ids) != 1 || ids[0] != issNoradID {
		t.Errorf("expected [%d], got %v", issNoradID, ids)
	}
}

func TestTrackSatelliteNotFound(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	err := svc.TrackSatellite(99999)
	if err == nil {
		t.Fatal("expected error for unknown satellite")
	}

	var notFound *SatelliteNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected SatelliteNotFoundError, got %T: %v", err, err)
	}
}

func TestUntrackSatellite(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	svc.UntrackSatellite(issNoradID)

	if svc.TrackedCount() != 0 {
		t.Errorf("expected 0 tracked satellites after untrack, got %d", svc.TrackedCount())
	}
}

func TestComputePosition(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	sat := svc.tracked[issNoradID]
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC) // Близко к эпохе TLE.

	event, err := svc.computePosition(sat, now)
	if err != nil {
		t.Fatalf("computePosition failed: %v", err)
	}

	// Проверяем базовые поля.
	if event.NoradID != issNoradID {
		t.Errorf("expected norad_id %d, got %d", issNoradID, event.NoradID)
	}

	if event.Name != "ISS (ZARYA)" {
		t.Errorf("expected name 'ISS (ZARYA)', got '%s'", event.Name)
	}

	// Широта ISS должна быть в пределах ±52° (наклонение 51.6°).
	if event.Lat < -52 || event.Lat > 52 {
		t.Errorf("ISS latitude out of range: %.4f", event.Lat)
	}

	// Долгота: -180..180.
	if event.Lon < -180 || event.Lon > 180 {
		t.Errorf("ISS longitude out of range: %.4f", event.Lon)
	}

	// Высота ISS: ~400-430 км.
	if event.Alt < 350 || event.Alt > 500 {
		t.Errorf("ISS altitude out of range: %.1f km", event.Alt)
	}

	// Азимут: 0-360.
	if event.Az < 0 || event.Az > 360 {
		t.Errorf("azimuth out of range: %.1f", event.Az)
	}

	// Элевация: -90..90.
	if event.El < -90 || event.El > 90 {
		t.Errorf("elevation out of range: %.1f", event.El)
	}

	// Дальность: > 0.
	if event.Range <= 0 {
		t.Errorf("range should be positive: %.1f", event.Range)
	}

	// Зона видимости: не nil, 72 точки.
	if event.VisibilityZone == nil {
		t.Fatal("visibility zone should not be nil")
	}
	if len(event.VisibilityZone.Points) != visibilityZonePoints {
		t.Errorf("expected %d zone points, got %d", visibilityZonePoints, len(event.VisibilityZone.Points))
	}

	// Timestamp.
	if event.TS <= 0 {
		t.Errorf("timestamp should be positive: %d", event.TS)
	}
}

func TestPositionEventJSON(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	sat := svc.tracked[issNoradID]
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	event, err := svc.computePosition(sat, now)
	if err != nil {
		t.Fatalf("computePosition failed: %v", err)
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Проверяем что JSON содержит все необходимые ключи.
	var m map[string]any
	if unmarshalErr := json.Unmarshal(data, &m); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal failed: %v", unmarshalErr)
	}

	requiredKeys := []string{"norad_id", "name", "lat", "lon", "alt", "az", "el", "range", "ts"}
	for _, key := range requiredKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key in JSON: %s", key)
		}
	}

	// visibility_zone должна присутствовать.
	if _, ok := m["visibility_zone"]; !ok {
		t.Error("missing visibility_zone in JSON")
	}
}

func TestRunBroadcastsPositions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	observer := tracker.NewObserver(47.315813, 39.788243, 0.070)

	svc := NewSatelliteTrackingService(hub, store, observer).WithInterval(50 * time.Millisecond)

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	// Запускаем сервис в фоне.
	var wg sync.WaitGroup
	wg.Go(func() {
		svc.Run(ctx)
	})

	// Ждём несколько тиков — проверяем что hub получил клиентов / broadcast работает.
	// Без подключённых SSE-клиентов broadcast просто отправляет в пустоту,
	// но сам факт вызова без паники — достаточный тест.
	time.Sleep(200 * time.Millisecond)

	cancel()
	wg.Wait()

	// Проверяем что сервис корректно завершился.
	if svc.TrackedCount() != 1 {
		t.Errorf("expected 1 tracked satellite after stop, got %d", svc.TrackedCount())
	}
}

func TestRunEmptyTrackedNoBroadcast(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(55.0, 37.0, 0.0)

	svc := NewSatelliteTrackingService(hub, store, observer).WithInterval(50 * time.Millisecond)

	// Запускаем без спутников.
	var wg sync.WaitGroup
	wg.Go(func() {
		svc.Run(ctx)
	})

	time.Sleep(150 * time.Millisecond)
	cancel()
	wg.Wait()
	// Если дошли без паники — тест пройден.
}

func TestSatelliteNotFoundError(t *testing.T) {
	err := &SatelliteNotFoundError{NoradID: 12345}
	expected := "satellite not found in TLE store: 12345"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestPropagationError(t *testing.T) {
	inner := errors.New("invalid TLE")
	err := &PropagationError{NoradID: 25544, Err: inner}
	if err.Error() != "propagation error for satellite 25544: invalid TLE" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
	if !errors.Is(err, inner) {
		t.Error("Unwrap should return inner error")
	}
}

func TestRoundTo(t *testing.T) {
	tests := []struct {
		val      float64
		decimals int
		expected float64
	}{
		{47.31581, 4, 47.3158},
		{39.78824, 4, 39.7882},
		{418.123, 1, 418.1},
		{215.678, 1, 215.7},
		{-42.555, 1, -42.6},
		{0.0, 4, 0.0},
	}

	for _, tt := range tests {
		result := roundTo(tt.val, tt.decimals)
		if result != tt.expected {
			t.Errorf("roundTo(%.5f, %d) = %.5f, expected %.5f", tt.val, tt.decimals, result, tt.expected)
		}
	}
}
