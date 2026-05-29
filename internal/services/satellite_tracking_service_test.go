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

	svc := NewSatelliteTrackingService(hub, store, observer).
		WithPositionInterval(100 * time.Millisecond).
		WithTrackInterval(100 * time.Millisecond)

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
	if svc.positionInterval != DefaultTrackingInterval {
		t.Errorf("expected default position interval %v, got %v", DefaultTrackingInterval, svc.positionInterval)
	}
	if svc.trackInterval != DefaultTrackInterval {
		t.Errorf("expected default track interval %v, got %v", DefaultTrackInterval, svc.trackInterval)
	}
	if svc.TrackedCount() != 0 {
		t.Errorf("expected 0 tracked satellites, got %d", svc.TrackedCount())
	}
}

func TestWithPositionInterval(t *testing.T) {
	hub := handlers.NewSSEHub()
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(55.0, 37.0, 0.0)

	svc := NewSatelliteTrackingService(hub, store, observer).WithPositionInterval(500 * time.Millisecond)
	if svc.positionInterval != 500*time.Millisecond {
		t.Errorf("expected 500ms position interval, got %v", svc.positionInterval)
	}

	// Невалидный интервал не должен менять значение.
	svc.WithPositionInterval(0)
	if svc.positionInterval != 500*time.Millisecond {
		t.Errorf("expected 500ms position interval after invalid, got %v", svc.positionInterval)
	}
}

func TestWithTrackInterval(t *testing.T) {
	hub := handlers.NewSSEHub()
	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(55.0, 37.0, 0.0)

	svc := NewSatelliteTrackingService(hub, store, observer).WithTrackInterval(10 * time.Second)
	if svc.trackInterval != 10*time.Second {
		t.Errorf("expected 10s track interval, got %v", svc.trackInterval)
	}

	// Невалидный интервал не должен менять значение.
	svc.WithTrackInterval(0)
	if svc.trackInterval != 10*time.Second {
		t.Errorf("expected 10s track interval after invalid, got %v", svc.trackInterval)
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

func TestTrackSatelliteExcluded(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	svc.WithExcluder(fakeExcluder{issNoradID: true})

	err := svc.TrackSatellite(issNoradID)
	if err == nil {
		t.Fatal("expected error when tracking an excluded satellite")
	}
	var excludedErr *SatelliteExcludedError
	if !errors.As(err, &excludedErr) {
		t.Errorf("expected SatelliteExcludedError, got %T", err)
	}
	if svc.TrackedCount() != 0 {
		t.Errorf("excluded satellite must not be tracked, got %d", svc.TrackedCount())
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

	pos, err := svc.computePosition(sat, now)
	if err != nil {
		t.Fatalf("computePosition failed: %v", err)
	}

	if pos.NoradID != issNoradID {
		t.Errorf("expected norad_id %d, got %d", issNoradID, pos.NoradID)
	}

	if pos.Name != "ISS" {
		t.Errorf("expected name 'ISS', got '%s'", pos.Name)
	}

	// Широта ISS должна быть в пределах ±52° (наклонение 51.6°).
	if pos.Lat < -52 || pos.Lat > 52 {
		t.Errorf("ISS latitude out of range: %.4f", pos.Lat)
	}

	// Долгота: -180..180.
	if pos.Lon < -180 || pos.Lon > 180 {
		t.Errorf("ISS longitude out of range: %.4f", pos.Lon)
	}

	// Высота ISS: ~400-430 км.
	if pos.Alt < 350 || pos.Alt > 500 {
		t.Errorf("ISS altitude out of range: %.1f km", pos.Alt)
	}

	// Азимут: 0-360.
	if pos.Az < 0 || pos.Az > 360 {
		t.Errorf("azimuth out of range: %.1f", pos.Az)
	}

	// Элевация: -90..90.
	if pos.El < -90 || pos.El > 90 {
		t.Errorf("elevation out of range: %.1f", pos.El)
	}

	// Дальность: > 0.
	if pos.Range <= 0 {
		t.Errorf("range should be positive: %.1f", pos.Range)
	}

	if pos.MapMarkerFwdLon == nil || pos.MapMarkerFwdLat == nil {
		t.Fatal("MapMarkerFwdLon/Lat should be set (second propagation step)")
	}
	if pos.MapMarkerRotDeg == nil {
		t.Fatal("MapMarkerRotDeg should be set (plat carré chord fallback)")
	}
	if *pos.MapMarkerRotDeg < -180 || *pos.MapMarkerRotDeg > 180 {
		t.Errorf("MapMarkerRotDeg out of [-180,180]: %v", *pos.MapMarkerRotDeg)
	}

	// Зона видимости: не nil, сегменты содержат ~72 точки (+ граничные при антимеридиане).
	if pos.VisibilityZone == nil {
		t.Fatal("visibility zone should not be nil")
	}
	totalPts := 0
	for _, seg := range pos.VisibilityZone.Segments {
		totalPts += len(seg)
	}
	if totalPts < visibilityZonePoints {
		t.Errorf("expected >= %d zone points, got %d", visibilityZonePoints, totalPts)
	}
}

func TestPositionDataJSON(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	sat := svc.tracked[issNoradID]
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	pos, err := svc.computePosition(sat, now)
	if err != nil {
		t.Fatalf("computePosition failed: %v", err)
	}

	data, err := json.Marshal(pos)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if unmarshalErr := json.Unmarshal(data, &m); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal failed: %v", unmarshalErr)
	}

	requiredKeys := []string{"norad_id", "name", "lat", "lon", "alt", "az", "el", "range"}
	for _, key := range requiredKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key in positionData JSON: %s", key)
		}
	}

	if _, ok := m["visibility_zone"]; !ok {
		t.Error("missing visibility_zone in positionData JSON")
	}

	if _, ok := m["map_marker_fwd_lon"]; !ok {
		t.Error("missing map_marker_fwd_lon in positionData JSON")
	}
	if _, ok := m["map_marker_fwd_lat"]; !ok {
		t.Error("missing map_marker_fwd_lat in positionData JSON")
	}
	if _, ok := m["map_marker_rot_deg"]; !ok {
		t.Error("missing map_marker_rot_deg in positionData JSON")
	}

	// TS теперь не в positionData, а в satelliteStateUpdate.
	if _, ok := m["ts"]; ok {
		t.Error("positionData should not contain 'ts' field (moved to satelliteStateUpdate)")
	}
}

func TestSatelliteStateUpdateJSON(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	sat := svc.tracked[issNoradID]
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	pos, err := svc.computePosition(sat, now)
	if err != nil {
		t.Fatalf("computePosition failed: %v", err)
	}

	update := satelliteStateUpdate{
		Positions:      []positionData{*pos},
		TracksIncluded: false,
		TS:             now.UnixMilli(),
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if unmarshalErr := json.Unmarshal(data, &m); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal failed: %v", unmarshalErr)
	}

	requiredKeys := []string{"positions", "tracks_included", "ts"}
	for _, key := range requiredKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key in satelliteStateUpdate JSON: %s", key)
		}
	}

	// positions должен быть массивом с одним элементом.
	positions, ok := m["positions"].([]any)
	if !ok || len(positions) != 1 {
		t.Fatalf("expected 1 position, got %v", m["positions"])
	}

	// tracks_included = false → tracks отсутствует (omitempty).
	if _, hasTracks := m["tracks"]; hasTracks {
		t.Error("tracks should be omitted when tracks_included=false and tracks is nil")
	}
}

func TestSatelliteStateUpdateWithTracks(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	sat := svc.tracked[issNoradID]
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	pos, err := svc.computePosition(sat, now)
	if err != nil {
		t.Fatalf("computePosition failed: %v", err)
	}

	tle, ok := svc.store.Get(issNoradID)
	if !ok {
		t.Fatal("ISS TLE not found in store")
	}

	track, err := tracker.GenerateDefaultGroundTrack(tle, now)
	if err != nil {
		t.Fatalf("GenerateDefaultGroundTrack failed: %v", err)
	}

	update := satelliteStateUpdate{
		Positions:      []positionData{*pos},
		Tracks:         []*tracker.GroundTrack{track},
		TracksIncluded: true,
		TS:             now.UnixMilli(),
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if unmarshalErr := json.Unmarshal(data, &m); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal failed: %v", unmarshalErr)
	}

	// tracks_included = true → tracks присутствует.
	tracks, ok := m["tracks"].([]any)
	if !ok || len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %v", m["tracks"])
	}

	tracksIncluded, ok := m["tracks_included"].(bool)
	if !ok || !tracksIncluded {
		t.Errorf("expected tracks_included=true, got %v", m["tracks_included"])
	}
}

func TestRunBroadcastsPositions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	observer := tracker.NewObserver(47.315813, 39.788243, 0.070)

	svc := NewSatelliteTrackingService(hub, store, observer).
		WithPositionInterval(50 * time.Millisecond).
		WithTrackInterval(100 * time.Millisecond)

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

	svc := NewSatelliteTrackingService(hub, store, observer).
		WithPositionInterval(50 * time.Millisecond).
		WithTrackInterval(50 * time.Millisecond)

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

func TestComputeAndBroadcastStatePositionsOnly(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	// includeTracks=false — только позиции, не должно паниковать.
	svc.computeAndBroadcastState(false)
}

func TestComputeAndBroadcastStateWithTracks(t *testing.T) {
	svc, cancel := setupTrackingService(t)
	defer cancel()

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	// includeTracks=true — позиции + трассы, не должно паниковать.
	svc.computeAndBroadcastState(true)
}

func TestComputeAndBroadcastStateEmpty(t *testing.T) {
	ctx := t.Context()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := tracker.NewTLEStore(nil)
	observer := tracker.NewObserver(55.0, 37.0, 0.0)

	svc := NewSatelliteTrackingService(hub, store, observer)

	// Без отслеживаемых спутников — не должно паниковать.
	svc.computeAndBroadcastState(false)
	svc.computeAndBroadcastState(true)
}

func TestRunBroadcastsBothPositionsAndTracks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	observer := tracker.NewObserver(47.315813, 39.788243, 0.070)

	svc := NewSatelliteTrackingService(hub, store, observer).
		WithPositionInterval(50 * time.Millisecond).
		WithTrackInterval(80 * time.Millisecond)

	if err := svc.TrackSatellite(issNoradID); err != nil {
		t.Fatalf("failed to track ISS: %v", err)
	}

	// Запускаем сервис — оба тикера должны сработать без паники.
	var wg sync.WaitGroup
	wg.Go(func() {
		svc.Run(ctx)
	})

	// Ждём достаточно для срабатывания обоих тикеров.
	time.Sleep(200 * time.Millisecond)

	cancel()
	wg.Wait()

	if svc.TrackedCount() != 1 {
		t.Errorf("expected 1 tracked satellite, got %d", svc.TrackedCount())
	}
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
