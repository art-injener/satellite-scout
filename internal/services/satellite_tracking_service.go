package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// Константы SatelliteTrackingService.
const (
	// Интервал обновления позиций по умолчанию.
	DefaultTrackingInterval = 1 * time.Second

	// Количество точек контура зоны видимости.
	visibilityZonePoints = 72
)

// positionEvent — JSON-структура SSE-события "position".
type positionEvent struct {
	NoradID        int                     `json:"norad_id"`
	Name           string                  `json:"name"`
	Lat            float64                 `json:"lat"`
	Lon            float64                 `json:"lon"`
	Alt            float64                 `json:"alt"`
	Az             float64                 `json:"az"`
	El             float64                 `json:"el"`
	Range          float64                 `json:"range"`
	VisibilityZone *tracker.VisibilityZone `json:"visibility_zone,omitempty"`
	TS             int64                   `json:"ts"`
}

// SatelliteNotFoundError — спутник не найден в TLEStore.
type SatelliteNotFoundError struct {
	NoradID int
}

func (e *SatelliteNotFoundError) Error() string {
	return fmt.Sprintf("satellite not found in TLE store: %d", e.NoradID)
}

// PropagationError — ошибка создания пропагатора.
type PropagationError struct {
	NoradID int
	Err     error
}

func (e *PropagationError) Error() string {
	return fmt.Sprintf("propagation error for satellite %d: %v", e.NoradID, e.Err)
}

func (e *PropagationError) Unwrap() error {
	return e.Err
}

// trackedSatellite — отслеживаемый спутник с кешированным пропагатором.
type trackedSatellite struct {
	noradID    int
	name       string
	propagator *tracker.Propagator
	tleEpoch   time.Time // Эпоха TLE для обнаружения обновлений.
}

// SatelliteTrackingService отслеживает спутники в реальном времени:
// рассчитывает позиции, AER, зону видимости и рассылает данные через SSE Hub.
type SatelliteTrackingService struct {
	hub      *handlers.SSEHub
	store    *tracker.TLEStore
	observer *tracker.Observer
	interval time.Duration

	mu      sync.RWMutex
	tracked map[int]*trackedSatellite // noradID → trackedSatellite.
}

// NewSatelliteTrackingService создаёт новый сервис отслеживания спутников.
func NewSatelliteTrackingService(
	hub *handlers.SSEHub,
	store *tracker.TLEStore,
	observer *tracker.Observer,
) *SatelliteTrackingService {
	return &SatelliteTrackingService{
		hub:      hub,
		store:    store,
		observer: observer,
		interval: DefaultTrackingInterval,
		tracked:  make(map[int]*trackedSatellite),
	}
}

// WithInterval устанавливает пользовательский интервал обновления.
func (s *SatelliteTrackingService) WithInterval(d time.Duration) *SatelliteTrackingService {
	if d > 0 {
		s.interval = d
	}
	return s
}

// Run запускает основной цикл отслеживания спутников.
// Каждый tick рассчитываются позиции всех отслеживаемых спутников
// и результаты отправляются через SSE Hub.
// Завершается при отмене ctx.
func (s *SatelliteTrackingService) Run(ctx context.Context) {
	slog.InfoContext(ctx, "satellite tracking service started", "interval", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "satellite tracking service stopped")
			return
		case <-ticker.C:
			s.computeAndBroadcast()
		}
	}
}

// TrackSatellite добавляет спутник в отслеживание.
// Загружает TLE из хранилища и создаёт пропагатор.
func (s *SatelliteTrackingService) TrackSatellite(noradID int) error {
	tle, ok := s.store.Get(noradID)
	if !ok {
		return &SatelliteNotFoundError{NoradID: noradID}
	}

	prop, err := tracker.NewPropagator(tle)
	if err != nil {
		return &PropagationError{NoradID: noradID, Err: err}
	}

	s.mu.Lock()
	s.tracked[noradID] = &trackedSatellite{
		noradID:    noradID,
		name:       tle.Name,
		propagator: prop,
		tleEpoch:   tle.Epoch,
	}
	s.mu.Unlock()

	slog.Info("tracking satellite", "norad_id", noradID, "name", tle.Name)
	return nil
}

// UntrackSatellite убирает спутник из отслеживания.
func (s *SatelliteTrackingService) UntrackSatellite(noradID int) {
	s.mu.Lock()
	delete(s.tracked, noradID)
	s.mu.Unlock()

	slog.Info("untracked satellite", "norad_id", noradID)
}

// TrackedIDs возвращает список NORAD ID отслеживаемых спутников.
func (s *SatelliteTrackingService) TrackedIDs() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]int, 0, len(s.tracked))
	for id := range s.tracked {
		ids = append(ids, id)
	}
	return ids
}

// TrackedCount возвращает количество отслеживаемых спутников.
func (s *SatelliteTrackingService) TrackedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tracked)
}

// computeAndBroadcast рассчитывает позиции всех отслеживаемых спутников
// и отправляет результаты через SSE Hub.
func (s *SatelliteTrackingService) computeAndBroadcast() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.tracked) == 0 {
		return
	}

	now := time.Now().UTC()

	for _, sat := range s.tracked {
		event, err := s.computePosition(sat, now)
		if err != nil {
			slog.Debug("failed to compute position",
				"norad_id", sat.noradID,
				"name", sat.name,
				"error", err,
			)
			continue
		}

		data, err := json.Marshal(event)
		if err != nil {
			slog.Error("failed to marshal position event",
				"norad_id", sat.noradID,
				"error", err,
			)
			continue
		}

		s.hub.Broadcast("position", data)
	}
}

// computePosition рассчитывает позицию одного спутника.
func (s *SatelliteTrackingService) computePosition(sat *trackedSatellite, now time.Time) (*positionEvent, error) {
	// SGP4 → ECI.
	eci, err := sat.propagator.Propagate(now)
	if err != nil {
		return nil, err
	}

	// ECI → ECEF → LLA.
	ecef := tracker.ECIToECEF(eci)
	lla := tracker.ECEFToLLA(ecef)

	// AER (азимут, элевация, дальность от наблюдателя).
	aer := s.observer.GetAER(eci)

	// Зона видимости (72 точки контура).
	zone := tracker.GenerateVisibilityZoneFromLLA(lla, sat.noradID, visibilityZonePoints)

	return &positionEvent{
		NoradID:        sat.noradID,
		Name:           sat.name,
		Lat:            roundTo(lla.LatDeg(), 4),
		Lon:            roundTo(lla.LonDeg(), 4),
		Alt:            roundTo(lla.Alt, 1),
		Az:             roundTo(aer.AzDeg(), 1),
		El:             roundTo(aer.ElDeg(), 1),
		Range:          roundTo(aer.Range, 1),
		VisibilityZone: zone,
		TS:             now.UnixMilli(),
	}, nil
}

// roundTo округляет число до заданного количества десятичных знаков.
func roundTo(val float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}
