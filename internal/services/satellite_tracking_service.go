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

	// Интервал обновления группы (проверка скользящего окна и смена primary).
	DefaultGroupUpdateInterval = 5 * time.Second

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

// satelliteGroupUpdate — JSON-структура SSE-события "satellite_group_update".
// Отправляется при изменении состава группы скользящего окна или смене primary.
type satelliteGroupUpdate struct {
	Satellites []groupSatInfo `json:"satellites"`
	PrimaryID  int            `json:"primary_id"`
	TrackingID int            `json:"tracking_id"` // NORAD ID спутника на сопровождении (0 = нет).
	TimeWindow groupTimeWin   `json:"time_window"`
	TS         int64          `json:"ts"`
}

// groupSatInfo — данные одного спутника внутри события satellite_group_update.
type groupSatInfo struct {
	NoradID   int     `json:"norad_id"`
	SatName   string  `json:"sat_name"`
	AOS       int64   `json:"aos"`
	LOS       int64   `json:"los"`
	Duration  float64 `json:"duration"`
	IsVisible bool    `json:"is_visible"`
}

// groupTimeWin — временное окно группы для SSE-события.
type groupTimeWin struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
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
// Три тикера: positions (1/сек), tracks (1/30 сек), group update (5/сек).
type SatelliteTrackingService struct {
	hub      *handlers.SSEHub
	store    *tracker.TLEStore
	observer *tracker.Observer

	positionInterval    time.Duration // Интервал обновления позиций.
	trackInterval       time.Duration // Интервал обновления наземных трасс.
	groupUpdateInterval time.Duration // Интервал обновления группы (скользящее окно).

	mu      sync.RWMutex
	tracked map[int]*trackedSatellite // noradID → trackedSatellite.

	// Кеш последних трасс — включается в каждый satellite_state_update,
	// чтобы Hub-кеш всегда содержал полные данные для новых клиентов.
	// Доступ только из горутины Run (потокобезопасен без дополнительной синхронизации).
	lastTracks []*tracker.GroundTrack

	// Сервис пролётов и параметры группы.
	passProvider    PassProvider  // Сервис пролётов (устанавливается через SetPassProvider).
	windowForward   time.Duration // Окно вперёд для скользящего окна.
	currentNoradID  int           // NORAD ID текущего primary спутника.
	autoTrackActive bool          // Флаг активности авто-трекинга (скользящее окно).
	manualSelection *int          // Ручной выбор пользователя (nil = авто).

	// Состояние текущей группы для change detection.
	currentGroupIDs []int // Отсортированные NORAD ID текущей группы.

	// pendingManualBroadcast — флаг форсированной рассылки group_update при следующем updateGroup.
	// Устанавливается в SetManualSelection / ResetManualSelection, чтобы tracking_id всегда
	// доходил до клиентов, даже если состав группы и primary не изменились.
	pendingManualBroadcast bool

	// notifyOnConnect — при получении сигнала выполняем немедленный updateGroup + broadcast.
	// Канал создаётся в конструкторе (буфер 1), чтобы select в Run() не блокировался на nil.
	notifyOnConnect chan struct{}
}

// NewSatelliteTrackingService создаёт новый сервис отслеживания спутников.
func NewSatelliteTrackingService(
	hub *handlers.SSEHub,
	store *tracker.TLEStore,
	observer *tracker.Observer,
) *SatelliteTrackingService {
	return &SatelliteTrackingService{
		hub:                 hub,
		store:               store,
		observer:            observer,
		positionInterval:    DefaultTrackingInterval,
		trackInterval:       DefaultTrackInterval,
		groupUpdateInterval: DefaultGroupUpdateInterval,
		windowForward:       DefaultWindowForward,
		tracked:             make(map[int]*trackedSatellite),
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

// WithGroupUpdateInterval устанавливает интервал обновления группы (скользящее окно).
func (s *SatelliteTrackingService) WithGroupUpdateInterval(
	d time.Duration,
) *SatelliteTrackingService {
	if d > 0 {
		s.groupUpdateInterval = d
	}
	return s
}

// WithWindowForward устанавливает размер окна вперёд для скользящего окна.
func (s *SatelliteTrackingService) WithWindowForward(
	d time.Duration,
) *SatelliteTrackingService {
	if d > 0 {
		s.windowForward = d
	}
	return s
}

// NotifyOnConnectChannel возвращает канал, в который Hub отправляет сигнал при регистрации нового клиента.
// Передать в SSEHub.SetNotifyOnConnect(), чтобы данные рассылались сразу после подключения (без ожидания тикера).
func (s *SatelliteTrackingService) NotifyOnConnectChannel() chan struct{} {
	return s.notifyOnConnect
}

// SetPassProvider устанавливает провайдер пролётов и активирует скользящее окно.
// Вызывается после создания PassService (избегаем циклической зависимости).
func (s *SatelliteTrackingService) SetPassProvider(provider PassProvider) {
	s.mu.Lock()
	s.passProvider = provider
	s.autoTrackActive = true
	s.mu.Unlock()

	slog.Info("group tracking enabled", "window_forward", s.windowForward)
}

// Run запускает основной цикл отслеживания спутников.
// Три тикера:
//   - positionTicker (1/сек) — текущие позиции спутников, AER, зона видимости
//   - trackTicker (1/30 сек) — наземные трассы орбит
//   - groupTicker (5 сек) — обновление группы скользящего окна, смена primary
//
// Трассы отправляются немедленно при старте, затем по тикеру.
// Завершается при отмене ctx.
func (s *SatelliteTrackingService) Run(ctx context.Context) {
	slog.InfoContext(ctx, "satellite tracking service started",
		"position_interval", s.positionInterval,
		"track_interval", s.trackInterval,
		"group_update_interval", s.groupUpdateInterval,
		"window_forward", s.windowForward,
	)

	posTicker := time.NewTicker(s.positionInterval)
	defer posTicker.Stop()

	trackTicker := time.NewTicker(s.trackInterval)
	defer trackTicker.Stop()

	groupTicker := time.NewTicker(s.groupUpdateInterval)
	defer groupTicker.Stop()

	// Немедленная инициализация группы и отправка данных при старте.
	s.updateGroup()
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
		case <-groupTicker.C:
			s.updateGroup()
		case <-s.notifyOnConnect:
			// Новый клиент подключился — рассылаем свежие позиции и треки немедленно.
			// Группа (satellite_group_update) уже доставлена Hub из кеша через sendCachedEvents,
			// повторный updateGroup() здесь создавал бы второй group_update и путал фронтенд.
			s.computeAndBroadcastState(true)
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

// updateGroup обновляет группу спутников скользящего окна.
//
// Алгоритм:
//  1. Получить пролёты из passProvider (кешированные).
//  2. Применить скользящее окно [now, now + windowForward] → список PassInfo.
//  3. Выбрать primary (с учётом ручного выбора или авто-логики).
//  4. Если состав группы изменился → обновить tracked, отправить satellite_group_update.
//  5. Если primary изменился → отправить satellite_change.
func (s *SatelliteTrackingService) updateGroup() {
	s.mu.Lock()
	provider := s.passProvider
	active := s.autoTrackActive
	currentID := s.currentNoradID
	oldGroupIDs := s.currentGroupIDs
	windowFwd := s.windowForward
	manualSel := s.manualSelection
	forceBroadcast := s.pendingManualBroadcast
	s.pendingManualBroadcast = false // сбрасываем флаг
	s.mu.Unlock()

	if !active || provider == nil {
		return
	}

	// Горизонт расчёта: окно вперёд + запас 10% (но не менее 2ч).
	horizonHours := int(DefaultGroupPassHorizon.Hours())
	passes, err := provider.GetAllGroupsPasses(horizonHours, tracker.DefaultMinElevation)
	if err != nil {
		slog.Debug("group-update: failed to get passes", "error", err)
		return
	}

	now := time.Now().UTC()

	// Фильтрация по скользящему окну — без вызова SGP4/PassPredictor.
	satellites := FindConcurrentPasses(passes, now, windowFwd)

	if len(satellites) == 0 {
		// Окно пустое — fallback на ближайший будущий пролёт (pre-pass режим).
		// Ищем ближайший пролёт с AOS > now во всём горизонте расчёта.
		nearest := findNearestFuturePass(passes, now)
		if nearest == nil {
			slog.Debug("group-update: no passes available at all")
			return
		}
		satellites = []PassInfo{{
			NoradID:   nearest.NoradID,
			SatName:   nearest.SatName,
			Pass:      *nearest,
			IsVisible: false,
		}}
		slog.Debug("group-update: window empty, using nearest future pass",
			"norad_id", nearest.NoradID,
			"sat_name", nearest.SatName,
			"aos_in", time.Until(time.UnixMilli(nearest.AOS)).Round(time.Second),
		)
	}

	// Проверяем, нужно ли сбросить ручной выбор (спутник вышел из окна).
	trackingExpired := false
	if manualSel != nil {
		inGroup := false
		for _, s2 := range satellites {
			if s2.NoradID == *manualSel {
				inGroup = true
				break
			}
		}
		if !inGroup {
			trackingExpired = true
			s.mu.Lock()
			s.manualSelection = nil
			s.mu.Unlock()
			manualSel = nil
			slog.Info("group-update: tracking ended, satellite left group")
		}
	}

	// Проверяем, нужно ли сменить primary после LOS.
	if shouldSwitch, newID := ShouldSwitchPrimary(currentID, satellites, now); shouldSwitch {
		currentID = newID
	}

	// Выбор primary с учётом ручного выбора.
	newPrimaryID := SelectPrimarySatellite(satellites, manualSel)

	// Новые NORAD ID для change detection.
	newGroupIDs := GroupIDs(satellites)

	groupChanged := GroupChanged(oldGroupIDs, newGroupIDs)
	primaryChanged := newPrimaryID != currentID

	// Если ничего не изменилось И не было ручного выбора — рассылка не нужна.
	if !groupChanged && !primaryChanged && !forceBroadcast {
		return
	}

	// Обновляем состояние: tracked и currentNoradID.
	s.mu.Lock()
	s.currentGroupIDs = newGroupIDs
	s.currentNoradID = newPrimaryID
	s.tracked = make(map[int]*trackedSatellite, len(satellites))
	s.lastTracks = nil
	s.mu.Unlock()

	// Добавляем все спутники группы в отслеживание.
	for _, sat := range satellites {
		if err2 := s.TrackSatellite(sat.NoradID); err2 != nil {
			slog.Debug("group-update: failed to add satellite",
				"norad_id", sat.NoradID,
				"name", sat.SatName,
				"error", err2,
			)
		}
	}

	slog.Info("group-update: group changed",
		"satellites", len(satellites),
		"primary", newPrimaryID,
		"primary_changed", primaryChanged,
	)

	// Текущий tracking_id для SSE (0 = нет сопровождения).
	trackingID := 0
	if manualSel != nil {
		trackingID = *manualSel
	}

	// Отправляем SSE-событие обновления группы.
	group := BuildConcurrentPassGroup(satellites, newPrimaryID)
	s.broadcastGroupUpdate(group, now, trackingID)

	// satellite_change: уведомление фронтенда о смене состояния.
	if trackingExpired {
		s.broadcastSatelliteChange(newPrimaryID, s.getSatName(newPrimaryID, satellites), "tracking_ended")
	} else if primaryChanged || currentID == 0 {
		reason := "auto"
		if manualSel != nil {
			reason = "manual"
		}
		if currentID == 0 {
			reason = "initial"
		}
		s.broadcastSatelliteChange(newPrimaryID, s.getSatName(newPrimaryID, satellites), reason)
	}

	// Немедленно отправляем позиции + трассы новой группы.
	s.computeAndBroadcastState(true)
}

// getSatName возвращает название спутника из группы по NORAD ID.
func (s *SatelliteTrackingService) getSatName(noradID int, satellites []PassInfo) string {
	for _, sat := range satellites {
		if sat.NoradID == noradID {
			return sat.SatName
		}
	}
	return ""
}

// SetManualSelection устанавливает ручной выбор спутника пользователем.
// Флаг pendingManualBroadcast гарантирует рассылку group_update с новым tracking_id,
// даже если состав группы и primary не изменились.
func (s *SatelliteTrackingService) SetManualSelection(noradID int) {
	s.mu.Lock()
	s.manualSelection = &noradID
	s.pendingManualBroadcast = true
	s.mu.Unlock()

	slog.Info("manual selection set", "norad_id", noradID)

	// Немедленное применение (не ждём следующего тика).
	s.updateGroup()
}

// ResetManualSelection сбрасывает ручной выбор (возврат к авто-режиму).
// Флаг pendingManualBroadcast гарантирует рассылку group_update с tracking_id=0.
func (s *SatelliteTrackingService) ResetManualSelection() {
	s.mu.Lock()
	s.manualSelection = nil
	s.pendingManualBroadcast = true
	s.mu.Unlock()

	slog.Info("manual selection reset to auto")
	s.updateGroup()
}

// broadcastGroupUpdate отправляет SSE-событие satellite_group_update.
func (s *SatelliteTrackingService) broadcastGroupUpdate(group ConcurrentPassGroup, now time.Time, trackingID int) {
	sats := make([]groupSatInfo, len(group.Satellites))
	for i, sat := range group.Satellites {
		sats[i] = groupSatInfo{
			NoradID:   sat.NoradID,
			SatName:   sat.SatName,
			AOS:       sat.Pass.AOS,
			LOS:       sat.Pass.LOS,
			Duration:  sat.Pass.Duration,
			IsVisible: sat.IsVisible,
		}
	}

	event := satelliteGroupUpdate{
		Satellites: sats,
		PrimaryID:  group.PrimarySatID,
		TrackingID: trackingID,
		TimeWindow: groupTimeWin{
			Start: group.TimeWindow.Start,
			End:   group.TimeWindow.End,
		},
		TS: now.UnixMilli(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal satellite_group_update", "error", err)
		return
	}

	s.hub.Broadcast("satellite_group_update", data)
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
