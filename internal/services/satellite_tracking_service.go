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
	// Интервал обновления позиций по умолчанию (1 раз/сек).
	DefaultTrackingInterval = 1 * time.Second

	// Интервал обновления наземных трасс по умолчанию (1 раз/30 сек).
	DefaultTrackInterval = 30 * time.Second

	// Интервал проверки авто-трекинга (переключение на ближайший спутник).
	DefaultAutoTrackInterval = 10 * time.Second

	// Количество точек контура зоны видимости.
	visibilityZonePoints = 72
)

// satelliteStateUpdate — JSON-структура группового SSE-события "satellite_state_update".
// Объединяет позиции и (опционально) треки всех отслеживаемых спутников в одно событие.
type satelliteStateUpdate struct {
	Positions      []positionData         `json:"positions"`
	Tracks         []*tracker.GroundTrack `json:"tracks,omitempty"`
	TracksIncluded bool                   `json:"tracks_included"`
	TS             int64                  `json:"ts"`
}

// positionData — данные позиции одного спутника внутри группового события.
type positionData struct {
	NoradID        int                     `json:"norad_id"`
	Name           string                  `json:"name"`
	Lat            float64                 `json:"lat"`
	Lon            float64                 `json:"lon"`
	Alt            float64                 `json:"alt"`
	Az             float64                 `json:"az"`
	El             float64                 `json:"el"`
	Range          float64                 `json:"range"`
	VisibilityZone *tracker.VisibilityZone `json:"visibility_zone,omitempty"`
}

// satelliteChangeEvent — JSON-структура SSE-события "satellite_change".
type satelliteChangeEvent struct {
	NoradID     int     `json:"norad_id"`
	Name        string  `json:"name"`
	Reason      string  `json:"reason"`      // "auto_track", "pass_ended", "initial"
	Inclination float64 `json:"inclination"` // Наклонение орбиты (градусы).
	Period      float64 `json:"period"`      // Орбитальный период (минуты).
	TS          int64   `json:"ts"`
}

// PassProvider — интерфейс для получения пролётов (избегаем циклической зависимости).
type PassProvider interface {
	GetAllGroupsPasses(hours int, minEl float64) ([]*tracker.Pass, error)
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
// рассчитывает позиции, AER, зону видимости и наземные трассы,
// рассылает данные через SSE Hub.
// Три тикера: positions (1/сек), tracks (1/30 сек), autoTrack (1/10 сек).
type SatelliteTrackingService struct {
	hub      *handlers.SSEHub
	store    *tracker.TLEStore
	observer *tracker.Observer

	positionInterval  time.Duration // Интервал обновления позиций.
	trackInterval     time.Duration // Интервал обновления наземных трасс.
	autoTrackInterval time.Duration // Интервал авто-трекинга.

	mu      sync.RWMutex
	tracked map[int]*trackedSatellite // noradID → trackedSatellite.

	// Кеш последних трасс — включается в каждый satellite_state_update,
	// чтобы Hub-кеш всегда содержал полные данные для новых клиентов.
	// Доступ только из горутины Run (потокобезопасен без дополнительной синхронизации).
	lastTracks []*tracker.GroundTrack

	// Авто-трекинг: автоматическое переключение на ближайший спутник.
	passProvider    PassProvider // Сервис пролётов (устанавливается через SetPassProvider).
	currentNoradID  int          // NORAD ID текущего отслеживаемого спутника.
	autoTrackActive bool         // Флаг активности авто-трекинга.
}

// NewSatelliteTrackingService создаёт новый сервис отслеживания спутников.
func NewSatelliteTrackingService(
	hub *handlers.SSEHub,
	store *tracker.TLEStore,
	observer *tracker.Observer,
) *SatelliteTrackingService {
	return &SatelliteTrackingService{
		hub:               hub,
		store:             store,
		observer:          observer,
		positionInterval:  DefaultTrackingInterval,
		trackInterval:     DefaultTrackInterval,
		autoTrackInterval: DefaultAutoTrackInterval,
		tracked:           make(map[int]*trackedSatellite),
	}
}

// WithPositionInterval устанавливает интервал обновления позиций.
func (s *SatelliteTrackingService) WithPositionInterval(
	d time.Duration,
) *SatelliteTrackingService {
	if d > 0 {
		s.positionInterval = d
	}
	return s
}

// WithTrackInterval устанавливает интервал обновления наземных трасс.
func (s *SatelliteTrackingService) WithTrackInterval(
	d time.Duration,
) *SatelliteTrackingService {
	if d > 0 {
		s.trackInterval = d
	}
	return s
}

// WithAutoTrackInterval устанавливает интервал авто-трекинга.
func (s *SatelliteTrackingService) WithAutoTrackInterval(
	d time.Duration,
) *SatelliteTrackingService {
	if d > 0 {
		s.autoTrackInterval = d
	}
	return s
}

// SetPassProvider устанавливает провайдер пролётов и активирует авто-трекинг.
// Вызывается после создания PassService (избегаем циклической зависимости).
func (s *SatelliteTrackingService) SetPassProvider(provider PassProvider) {
	s.mu.Lock()
	s.passProvider = provider
	s.autoTrackActive = true
	s.mu.Unlock()

	slog.Info("auto-track enabled")
}

// Run запускает основной цикл отслеживания спутников.
// Три тикера:
//   - positionTicker (1/сек) — текущие позиции спутников, AER, зона видимости
//   - trackTicker (1/30 сек) — наземные трассы орбит
//   - autoTrackTicker (1/10 сек) — авто-переключение на ближайший спутник
//
// Трассы отправляются немедленно при старте, затем по тикеру.
// Завершается при отмене ctx.
func (s *SatelliteTrackingService) Run(ctx context.Context) {
	slog.InfoContext(ctx, "satellite tracking service started",
		"position_interval", s.positionInterval,
		"track_interval", s.trackInterval,
		"auto_track_interval", s.autoTrackInterval,
	)

	posTicker := time.NewTicker(s.positionInterval)
	defer posTicker.Stop()

	trackTicker := time.NewTicker(s.trackInterval)
	defer trackTicker.Stop()

	autoTrackTicker := time.NewTicker(s.autoTrackInterval)
	defer autoTrackTicker.Stop()

	// Немедленный выбор начального спутника и отправка данных (не ждём 10 сек до первого тика авто-трекинга).
	s.updateAutoTrack()
	// Первые позиции + трассы — сразу после выбора спутника.
	s.computeAndBroadcastState(true)

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "satellite tracking service stopped")
			return
		case <-posTicker.C:
			s.computeAndBroadcastState(false)
		case <-trackTicker.C:
			s.computeAndBroadcastState(true)
		case <-autoTrackTicker.C:
			s.updateAutoTrack()
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

// CurrentNoradID возвращает NORAD ID текущего отслеживаемого спутника (авто-трекинг).
func (s *SatelliteTrackingService) CurrentNoradID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentNoradID
}

// updateAutoTrack проверяет и переключает отслеживание на ближайший спутник.
// Логика:
//  1. Получаем список пролётов (отсортированы по AOS)
//  2. Находим первый активный (AOS <= now <= LOS) или первый предстоящий (now < AOS)
//  3. Если текущий спутник изменился — переключаем отслеживание
func (s *SatelliteTrackingService) updateAutoTrack() {
	s.mu.RLock()
	provider := s.passProvider
	active := s.autoTrackActive
	currentID := s.currentNoradID
	s.mu.RUnlock()

	if !active || provider == nil {
		return
	}

	// Получаем пролёты на ближайшие 24 часа.
	passes, err := provider.GetAllGroupsPasses(24, tracker.DefaultMinElevation)
	if err != nil {
		slog.Debug("auto-track: failed to get passes", "error", err)
		return
	}

	if len(passes) == 0 {
		slog.Debug("auto-track: no passes available")
		return
	}

	// Находим ближайший спутник для отслеживания.
	now := time.Now().UTC().UnixMilli()

	// Если уже отслеживаем спутник, проверяем: его пролёт ещё активен?
	if currentID != 0 {
		for _, p := range passes {
			if p.NoradID == currentID && p.AOS <= now && now <= p.LOS {
				// Текущий пролёт ещё идёт — не переключаемся.
				return
			}
		}
	}

	targetPass := s.findNearestPass(passes, now)

	if targetPass == nil {
		slog.Debug("auto-track: no suitable pass found")
		return
	}

	// Проверяем, нужно ли переключаться.
	if targetPass.NoradID == currentID {
		return // Уже отслеживаем этот спутник.
	}

	// Определяем причину переключения.
	reason := "pass_ended"
	if currentID == 0 {
		reason = "initial"
	}

	// Переключаем отслеживание.
	s.switchToSatellite(targetPass.NoradID, targetPass.SatName, reason)
}

// findNearestPass находит ближайший подходящий пролёт.
// Приоритет: активный (сейчас виден) > ближайший предстоящий.
func (s *SatelliteTrackingService) findNearestPass(
	passes []*tracker.Pass,
	nowMs int64,
) *tracker.Pass {
	// Сначала ищем активный пролёт (сейчас виден).
	for _, p := range passes {
		if p.AOS <= nowMs && nowMs <= p.LOS {
			return p
		}
	}

	// Если нет активных — берём первый предстоящий.
	for _, p := range passes {
		if nowMs < p.AOS {
			return p
		}
	}

	return nil
}

// switchToSatellite переключает отслеживание на указанный спутник.
func (s *SatelliteTrackingService) switchToSatellite(noradID int, name, reason string) {
	// Очищаем предыдущее отслеживание и кеш трасс.
	s.mu.Lock()
	oldID := s.currentNoradID
	s.tracked = make(map[int]*trackedSatellite)
	s.currentNoradID = noradID
	s.lastTracks = nil
	s.mu.Unlock()

	// Добавляем новый спутник.
	if err := s.TrackSatellite(noradID); err != nil {
		slog.Error("auto-track: failed to track satellite",
			"norad_id", noradID,
			"name", name,
			"error", err,
		)
		return
	}

	slog.Info("auto-track: switched satellite",
		"from", oldID,
		"to", noradID,
		"name", name,
		"reason", reason,
	)

	// Отправляем событие смены спутника.
	s.broadcastSatelliteChange(noradID, name, reason)

	// Немедленно отправляем позиции + трассу нового спутника.
	s.computeAndBroadcastState(true)
}

// broadcastSatelliteChange отправляет SSE-событие о смене спутника.
func (s *SatelliteTrackingService) broadcastSatelliteChange(noradID int, name, reason string) {
	event := satelliteChangeEvent{
		NoradID: noradID,
		Name:    name,
		Reason:  reason,
		TS:      time.Now().UTC().UnixMilli(),
	}

	// Получаем TLE для орбитальных параметров.
	if tle, ok := s.store.Get(noradID); ok {
		event.Inclination = roundTo(tle.Inclination, 2)
		event.Period = roundTo(tle.OrbitalPeriod(), 1)
	}

	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal satellite_change event", "error", err)
		return
	}

	s.hub.Broadcast("satellite_change", data)
}

// computeAndBroadcastState рассчитывает позиции (и опционально треки)
// всех отслеживаемых спутников, собирает в одно групповое событие
// "satellite_state_update" и отправляет через SSE Hub.
// refreshTracks=true — пересчитать наземные трассы (каждые 30 секунд).
// Кешированные треки включаются в каждое событие, чтобы Hub-кеш
// всегда содержал полные данные для вновь подключающихся клиентов.
func (s *SatelliteTrackingService) computeAndBroadcastState(refreshTracks bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.tracked) == 0 {
		return
	}

	now := time.Now().UTC()

	positions := make([]positionData, 0, len(s.tracked))

	// Пересчёт трасс при запросе (каждые 30 секунд или при смене спутника).
	if refreshTracks {
		freshTracks := make([]*tracker.GroundTrack, 0, len(s.tracked))
		for _, sat := range s.tracked {
			tle, ok := s.store.Get(sat.noradID)
			if !ok {
				slog.Debug("TLE not found for track generation",
					"norad_id", sat.noradID,
					"name", sat.name,
				)
				continue
			}

			track, err := tracker.GenerateDefaultGroundTrack(tle, now)
			if err != nil {
				slog.Debug("failed to generate ground track",
					"norad_id", sat.noradID,
					"name", sat.name,
					"error", err,
				)
				continue
			}
			freshTracks = append(freshTracks, track)
		}
		s.lastTracks = freshTracks
	}

	for _, sat := range s.tracked {
		pos, err := s.computePosition(sat, now)
		if err != nil {
			slog.Debug("failed to compute position",
				"norad_id", sat.noradID,
				"name", sat.name,
				"error", err,
			)
			continue
		}
		positions = append(positions, *pos)
	}

	if len(positions) == 0 {
		return
	}

	update := satelliteStateUpdate{
		Positions:      positions,
		Tracks:         s.lastTracks,
		TracksIncluded: len(s.lastTracks) > 0,
		TS:             now.UnixMilli(),
	}

	data, err := json.Marshal(update)
	if err != nil {
		slog.Error("failed to marshal satellite_state_update", "error", err)
		return
	}

	s.hub.Broadcast("satellite_state_update", data)
}

// computePosition рассчитывает позицию одного спутника.
func (s *SatelliteTrackingService) computePosition(sat *trackedSatellite, now time.Time) (*positionData, error) {
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

	return &positionData{
		NoradID:        sat.noradID,
		Name:           sat.name,
		Lat:            roundTo(lla.LatDeg(), 4),
		Lon:            roundTo(lla.LonDeg(), 4),
		Alt:            roundTo(lla.Alt, 1),
		Az:             roundTo(aer.AzDeg(), 1),
		El:             roundTo(aer.ElDeg(), 1),
		Range:          roundTo(aer.Range, 1),
		VisibilityZone: zone,
	}, nil
}

// roundTo округляет число до заданного количества десятичных знаков.
func roundTo(val float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}
