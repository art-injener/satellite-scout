package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
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

	// Совпадает с tracker.DefaultGroundTrackStep — направление маркера = касательная к отрезкам трека на карте.
	mapMarkerRotPropagateStep = tracker.DefaultGroundTrackStep
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
	TrackingID int            `json:"tracking_id"` // NORAD ID спутника под наблюдением (0 = нет).
	TimeWindow groupTimeWin   `json:"time_window"`
	TS         int64          `json:"ts"`
}

// groupSatInfo — данные одного спутника внутри события satellite_group_update.
type groupSatInfo struct {
	NoradID   int     `json:"norad_id"`
	SatName   string  `json:"sat_name"`
	SatAlias  string  `json:"sat_alias,omitempty"`
	AOS       int64   `json:"aos"`
	LOS       int64   `json:"los"`
	Duration  float64 `json:"duration"`
	IsVisible bool    `json:"is_visible"`
	// Снимок столбцов «Длит.» / «До AOS» на момент рассылки.
	UIColDuration string `json:"ui_col_duration"`
	UIColUntil    string `json:"ui_col_until"`
	// Траектория пролёта на небесной сфере (az/el/time) для отрисовки SkyView.
	SkyPath []tracker.AzElPoint `json:"sky_path,omitempty"`
	// Радиочастотные характеристики из SatNOGS (опционально, только при доступности данных).
	// FreqMHz — нижний край downlink в формате "145.825".
	// Modulation — короткая подпись для UI: "FM" или "AFSK 1200".
	FreqMHz    string `json:"freq_mhz,omitempty"`
	Modulation string `json:"modulation,omitempty"`
	// TCAEl — максимальный угол места пролёта (градусы).
	TCAEl float64 `json:"tca_el"`
	// OrbitAltKm — средняя высота орбиты по TLE (апогей+перигей)/2, км.
	OrbitAltKm float64 `json:"orbit_alt_km"`
}

// groupTimeWin — временное окно группы для SSE-события.
type groupTimeWin struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// positionData — данные позиции одного спутника внутри группового события.
type positionData struct {
	NoradID int     `json:"norad_id"`
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Alt     float64 `json:"alt"`
	Az      float64 `json:"az"`
	El      float64 `json:"el"`
	Range   float64 `json:"range"`
	// Радиальная скорость спутника относительно наблюдателя, м/с.
	// «+» — удаляется (red shift), «−» — приближается (blue shift).
	// Используется фронтом для расчёта доплеровского сдвига приёмника.
	RangeRate      float64                 `json:"range_rate"`
	VisibilityZone *tracker.VisibilityZone `json:"visibility_zone,omitempty"`
	// Вторая точка подспутниковой трассы (now + шаг трассы) — клиент считает угол через project().
	MapMarkerFwdLon *float64 `json:"map_marker_fwd_lon,omitempty"`
	MapMarkerFwdLat *float64 `json:"map_marker_fwd_lat,omitempty"`
	// Запасной угол при скачке долготы между точками на canvas (хорда в lat/lon, как трек).
	MapMarkerRotDeg *float64 `json:"map_marker_rot_deg,omitempty"`
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

// TransmitterInfo — узкая выжимка для UI, не зависящая от пакета satnogs.
// Поля совпадают с satnogs.TransmitterSummary — sat-tracking использует только их.
type TransmitterInfo struct {
	FreqMHz    string
	Modulation string
}

// TransmitterProvider — источник данных о передатчиках (SatNOGS или мок в тестах).
// Интерфейс намеренно узкий, чтобы не тащить пакет satnogs в services.
type TransmitterProvider interface {
	GetPrimaryTransmitter(noradID int) *TransmitterInfo
	RequestFetch(noradIDs []int)
}

// SatelliteNotFoundError — спутник не найден в TLEStore.
type SatelliteNotFoundError struct {
	NoradID int
}

func (e *SatelliteNotFoundError) Error() string {
	return fmt.Sprintf("satellite not found in TLE store: %d", e.NoradID)
}

// SatelliteExcludedError — спутник в списке исключений и не может отслеживаться.
type SatelliteExcludedError struct {
	NoradID int
}

func (e *SatelliteExcludedError) Error() string {
	return fmt.Sprintf("satellite is excluded: %d", e.NoradID)
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
	hub   *handlers.SSEHub
	store *tracker.TLEStore
	// observer хранится в atomic.Pointer для горячей смены координат из UI
	// настроек: тиковые горутины читают через Load(), модалка настроек
	// меняет через SetObserver().
	observer atomic.Pointer[tracker.Observer]

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
	manualSelection *int          // Ручной выбор: primary (legacy, для авто-трекинга).

	// Источник данных о передатчиках (частота/модуляция). Опционально, nil = выключено.
	transmitterProvider TransmitterProvider

	// Набор исключённых NORAD ID. Опционально, nil = исключений нет.
	excluder Excluder

	// Per-client состояние наблюдения
	clientState *ClientStateStore
	// clientID последнего SetManualSelection — для направленного SSE-уведомления.
	lastManualClientID string

	// Состояние текущей группы для change detection.
	currentGroupIDs     []int        // Отсортированные NORAD ID текущей группы (legacy).
	currentGroupEntries []GroupEntry // (NoradID, IsVisible, AOS, LOS) для полного change detection.

	// pendingManualBroadcast — флаг форсированной рассылки group_update при следующем updateGroup.
	pendingManualBroadcast bool
	// pendingDirectedClientID — clientID для направленного уведомления вместо broadcast.
	pendingDirectedClientID string

	// notifyOnConnect — при получении сигнала выполняем немедленный updateGroup + broadcast.
	notifyOnConnect chan struct{}

	// forceUpdate — сигнал немедленного пересчёта группы (например, после изменения исключений).
	forceUpdate chan struct{}
}

// NewSatelliteTrackingService создаёт новый сервис отслеживания спутников.
func NewSatelliteTrackingService(
	hub *handlers.SSEHub,
	store *tracker.TLEStore,
	observer *tracker.Observer,
) *SatelliteTrackingService {
	s := &SatelliteTrackingService{
		hub:                 hub,
		store:               store,
		positionInterval:    DefaultTrackingInterval,
		trackInterval:       DefaultTrackInterval,
		groupUpdateInterval: DefaultGroupUpdateInterval,
		windowForward:       DefaultWindowForward,
		tracked:             make(map[int]*trackedSatellite),
		clientState:         NewClientStateStore(24 * time.Hour),
		forceUpdate:         make(chan struct{}, 1),
	}
	s.observer.Store(observer)
	return s
}

// Observer возвращает текущую точку наблюдения (потокобезопасно).
func (s *SatelliteTrackingService) Observer() *tracker.Observer {
	return s.observer.Load()
}

// SetObserver атомарно меняет точку наблюдения. Используется hot-reload-ом
// единого конфига при правке Observer в модалке настроек: тиковые горутины
// со следующего цикла будут считать AER/трассы относительно новой точки.
func (s *SatelliteTrackingService) SetObserver(o *tracker.Observer) {
	if o == nil {
		return
	}
	s.observer.Store(o)
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

// SetTransmitterProvider устанавливает источник данных о передатчиках (SatNOGS).
// Опционально: можно вызывать или не вызывать в зависимости от config.SatNOGSEnabled.
func (s *SatelliteTrackingService) SetTransmitterProvider(provider TransmitterProvider) {
	s.mu.Lock()
	s.transmitterProvider = provider
	s.mu.Unlock()
}

// WithExcluder подключает набор исключённых NORAD ID (жёсткий запрет на ручное наблюдение).
func (s *SatelliteTrackingService) WithExcluder(e Excluder) *SatelliteTrackingService {
	s.excluder = e
	return s
}

// ForceGroupUpdate запрашивает немедленный пересчёт и рассылку группы.
// Используется после изменения списка исключений, чтобы спутник пропадал сразу.
// Не блокирует: при уже стоящем в очереди сигнале повтор отбрасывается.
func (s *SatelliteTrackingService) ForceGroupUpdate() {
	s.mu.Lock()
	s.pendingManualBroadcast = true
	s.mu.Unlock()
	select {
	case s.forceUpdate <- struct{}{}:
	default:
	}
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
		case <-s.forceUpdate:
			// Внешний запрос немедленного пересчёта группы (например, после изменения исключений).
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
	if s.excluder != nil && s.excluder.Contains(noradID) {
		return &SatelliteExcludedError{NoradID: noradID}
	}

	tle, ok := s.store.Get(noradID)
	if !ok {
		return &SatelliteNotFoundError{NoradID: noradID}
	}

	prop, err := tracker.NewPropagator(tle)
	if err != nil {
		return &PropagationError{NoradID: noradID, Err: err}
	}

	s.mu.Lock()
	primary, _ := tracker.ParseSatName(tle.Name)
	s.tracked[noradID] = &trackedSatellite{
		noradID:    noradID,
		name:       primary,
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

// GroupNoradIDs возвращает NORAD ID активной группы скользящего окна
// в стабильном порядке (отсортирован по NoradID, как currentGroupEntries).
// Используется внешними потребителями (mock SSE-каналов и пр.).
// Возвращает nil, если группа ещё не сформирована.
func (s *SatelliteTrackingService) GroupNoradIDs() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.currentGroupEntries) == 0 {
		return nil
	}
	ids := make([]int, len(s.currentGroupEntries))
	for i, e := range s.currentGroupEntries {
		ids[i] = e.NoradID
	}
	return ids
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
	snap := s.snapshotGroupState()
	if !snap.active || snap.provider == nil {
		return
	}

	passes, satellites, rawSatellites, now, ok := s.resolveGroupSatellites(snap)
	if !ok {
		return
	}

	trackingExpired, manualSel := s.checkTrackingExpiry(
		snap.manualSel, rawSatellites, satellites, passes, now,
	)

	newPrimaryID := s.choosePrimary(snap.currentID, manualSel, satellites, now)

	newGroupEntries := GroupEntries(satellites)
	groupChanged := GroupEntriesChanged(snap.oldGroupEntries, newGroupEntries)
	primaryChanged := newPrimaryID != snap.currentID

	if !groupChanged && !primaryChanged && !snap.forceBroadcast && !trackingExpired {
		return
	}

	s.applyGroupState(newGroupEntries, newPrimaryID, satellites)

	slog.Info("group-update: group changed",
		"satellites", len(satellites),
		"primary", newPrimaryID,
		"primary_changed", primaryChanged,
		"group_data_changed", groupChanged,
	)

	group := BuildConcurrentPassGroup(satellites, newPrimaryID)
	trackingID := 0
	if manualSel != nil {
		trackingID = *manualSel
	}
	s.broadcastGroupUpdate(group, now, trackingID)

	s.sendPostGroupNotifications(
		snap, manualSel, trackingExpired, groupChanged, primaryChanged,
		newPrimaryID, satellites,
	)

	s.computeAndBroadcastState(true)
}

// groupSnapshot — снимок состояния, необходимый для одного цикла updateGroup.
type groupSnapshot struct {
	provider         PassProvider
	active           bool
	currentID        int
	oldGroupEntries  []GroupEntry
	windowFwd        time.Duration
	manualSel        *int
	forceBroadcast   bool
	directedClientID string
}

func (s *SatelliteTrackingService) snapshotGroupState() groupSnapshot {
	s.mu.Lock()
	snap := groupSnapshot{
		provider:         s.passProvider,
		active:           s.autoTrackActive,
		currentID:        s.currentNoradID,
		oldGroupEntries:  s.currentGroupEntries,
		windowFwd:        s.windowForward,
		manualSel:        s.manualSelection,
		forceBroadcast:   s.pendingManualBroadcast,
		directedClientID: s.pendingDirectedClientID,
	}
	s.pendingManualBroadcast = false
	s.pendingDirectedClientID = ""
	s.mu.Unlock()
	return snap
}

func (s *SatelliteTrackingService) resolveGroupSatellites(
	snap groupSnapshot,
) ([]*tracker.Pass, []PassInfo, []PassInfo, time.Time, bool) {
	horizonHours := int(DefaultGroupPassHorizon.Hours())
	passes, err := snap.provider.GetAllGroupsPasses(horizonHours, tracker.DefaultMinElevation)
	if err != nil {
		slog.Debug("group-update: failed to get passes", "error", err)
		return nil, nil, nil, time.Time{}, false
	}

	now := time.Now().UTC()
	rawSatellites := FindConcurrentPasses(passes, now, snap.windowFwd)
	satellites := rawSatellites

	if len(satellites) == 0 {
		nearest := findNearestFuturePass(passes, now)
		if nearest == nil {
			slog.Debug("group-update: no passes available at all")
			return nil, nil, nil, time.Time{}, false
		}
		satellites = []PassInfo{{
			NoradID:      nearest.NoradID,
			SatName:      nearest.SatName,
			Pass:         *nearest,
			IsVisible:    false,
			IsSubstitute: true,
		}}
		slog.Debug("group-update: window empty, using nearest future pass",
			"norad_id", nearest.NoradID,
			"sat_name", nearest.SatName,
			"aos_in", time.Until(time.UnixMilli(nearest.AOS)).Round(time.Second),
		)
	}

	return passes, satellites, rawSatellites, now, true
}

func (s *SatelliteTrackingService) checkTrackingExpiry(
	manualSel *int, rawSatellites, satellites []PassInfo, passes []*tracker.Pass, now time.Time,
) (bool, *int) {
	if manualSel == nil {
		return false, nil
	}

	nowMs := now.UnixMilli()
	inGroup := false
	for _, s2 := range satellites {
		if s2.NoradID == *manualSel {
			inGroup = true
			break
		}
	}
	onlySubstitutedNextPass := len(rawSatellites) == 0 && len(satellites) == 1 &&
		satellites[0].NoradID == *manualSel && !satellites[0].IsVisible &&
		nowMs < satellites[0].Pass.AOS
	endAfterFinishedPass := onlySubstitutedNextPass && NoradInPostLosGap(passes, *manualSel, now)

	if inGroup && !endAfterFinishedPass {
		return false, manualSel
	}

	expiredNorad := *manualSel
	s.clientState.ClearTrackingForNorad(expiredNorad)
	s.mu.Lock()
	s.manualSelection = nil
	s.mu.Unlock()
	slog.Info("group-update: tracking ended (left window or pass ended before next AOS)",
		"norad_id", expiredNorad,
		"after_los_next_pass_row", endAfterFinishedPass,
	)
	return true, nil
}

func (s *SatelliteTrackingService) choosePrimary(
	currentID int, manualSel *int, satellites []PassInfo, now time.Time,
) int {
	if currentID > 0 && manualSel == nil {
		shouldSwitch, newID := ShouldSwitchPrimary(currentID, satellites, now)
		if shouldSwitch {
			return newID
		}
		return currentID
	}
	return SelectPrimarySatellite(satellites, manualSel, now)
}

func (s *SatelliteTrackingService) applyGroupState(
	entries []GroupEntry, primaryID int, satellites []PassInfo,
) {
	s.mu.Lock()
	s.currentGroupEntries = entries
	s.currentGroupIDs = GroupIDs(satellites)
	s.currentNoradID = primaryID
	s.tracked = make(map[int]*trackedSatellite, len(satellites))
	s.lastTracks = nil
	s.mu.Unlock()

	for _, sat := range satellites {
		if err := s.TrackSatellite(sat.NoradID); err != nil {
			slog.Debug("group-update: failed to add satellite",
				"norad_id", sat.NoradID, "name", sat.SatName, "error", err,
			)
		}
	}
}

func (s *SatelliteTrackingService) sendPostGroupNotifications(
	snap groupSnapshot, manualSel *int, trackingExpired, groupChanged, primaryChanged bool,
	newPrimaryID int, satellites []PassInfo,
) {
	if snap.forceBroadcast && snap.directedClientID != "" {
		trackingID := 0
		if manualSel != nil {
			trackingID = *manualSel
		}
		s.sendClientStateRestore(snap.directedClientID, trackingID)
	}

	switch {
	case trackingExpired:
		s.broadcastSatelliteChange(newPrimaryID, s.getSatName(newPrimaryID, satellites), "tracking_ended")
		if snap.directedClientID != "" {
			s.sendClientStateRestore(snap.directedClientID, 0)
		}
	case primaryChanged || snap.currentID == 0:
		reason := "auto"
		if manualSel != nil {
			reason = "manual"
		}
		if snap.currentID == 0 {
			reason = "initial"
		}
		s.broadcastSatelliteChange(newPrimaryID, s.getSatName(newPrimaryID, satellites), reason)
	case groupChanged:
		s.broadcastSatelliteChange(newPrimaryID, s.getSatName(newPrimaryID, satellites), "auto")
	}
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
// clientID — идентификатор клиента для per-client state (TRACK-STATE-003).
func (s *SatelliteTrackingService) SetManualSelection(noradID int, clientID string) {
	s.mu.Lock()
	s.manualSelection = &noradID
	s.pendingManualBroadcast = true
	s.pendingDirectedClientID = clientID
	s.lastManualClientID = clientID
	s.mu.Unlock()

	// Per-client state.
	if clientID != "" {
		s.clientState.SetTracking(clientID, noradID)
	}

	slog.Info("manual selection set", "norad_id", noradID, "client_id", clientID)

	s.updateGroup()
}

// ResetManualSelection сбрасывает ручной выбор (возврат к авто-режиму).
// clientID — идентификатор клиента для per-client state (TRACK-STATE-003).
func (s *SatelliteTrackingService) ResetManualSelection(clientID string) {
	s.mu.Lock()
	s.manualSelection = nil
	s.pendingManualBroadcast = true
	s.pendingDirectedClientID = clientID
	s.mu.Unlock()

	if clientID != "" {
		s.clientState.ClearTracking(clientID)
	}

	slog.Info("manual selection reset to auto", "client_id", clientID)
	s.updateGroup()
}

// broadcastGroupUpdate отправляет SSE-событие satellite_group_update.
func (s *SatelliteTrackingService) broadcastGroupUpdate(group ConcurrentPassGroup, now time.Time, trackingID int) {
	// Снимок провайдера передатчиков под RLock — поле выставляется один раз при старте,
	// но если когда-то будем менять в рантайме, это уже потокобезопасно.
	s.mu.RLock()
	txProvider := s.transmitterProvider
	s.mu.RUnlock()

	// Триггерим background-prefetch SatNOGS для всех NORAD группы.
	// Не блокирует: если данных нет — поля FreqMHz/Modulation будут пустыми,
	// при следующем group_update (~5с) подтянутся из кеша.
	if txProvider != nil && len(group.Satellites) > 0 {
		ids := make([]int, len(group.Satellites))
		for i, sat := range group.Satellites {
			ids[i] = sat.NoradID
		}
		txProvider.RequestFetch(ids)
	}

	sats := make([]groupSatInfo, len(group.Satellites))
	for i, sat := range group.Satellites {
		uiDur, uiUntil := FormatSessionTableColumns(sat.Pass.AOS, sat.Pass.LOS, now.UTC())
		info := groupSatInfo{
			NoradID:       sat.NoradID,
			SatName:       sat.SatName,
			SatAlias:      sat.Pass.SatAlias,
			AOS:           sat.Pass.AOS,
			LOS:           sat.Pass.LOS,
			Duration:      sat.Pass.Duration,
			IsVisible:     sat.IsVisible,
			UIColDuration: uiDur,
			UIColUntil:    uiUntil,
			SkyPath:       sat.Pass.SkyPath,
			TCAEl:         roundTo(sat.Pass.TCAEl, 1),
		}
		if tle, ok := s.store.Get(sat.NoradID); ok && tle != nil {
			info.OrbitAltKm = roundTo(tle.MeanAltitudeKm(), 0)
		}
		if txProvider != nil {
			if tx := txProvider.GetPrimaryTransmitter(sat.NoradID); tx != nil {
				info.FreqMHz = tx.FreqMHz
				info.Modulation = tx.Modulation
			}
		}
		sats[i] = info
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

// sendClientStateRestore отправляет per-client событие client_state_restore.
func (s *SatelliteTrackingService) sendClientStateRestore(clientID string, trackingID int) {
	event := struct {
		TrackingID int   `json:"tracking_id"`
		TS         int64 `json:"ts"`
	}{
		TrackingID: trackingID,
		TS:         time.Now().UTC().UnixMilli(),
	}
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal client_state_restore", "error", err)
		return
	}
	s.hub.SendToClient(clientID, "client_state_restore", data)
}

// GetClientStateStore возвращает per-client хранилище для настройки Hub.
func (s *SatelliteTrackingService) GetClientStateStore() *ClientStateStore {
	return s.clientState
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

			// Трасса генерируется ровно по окну карты с центром в долготе станции:
			// от observerLon−180° до observerLon+180° по continuous lon. См. doc у
			// GenerateGroundTrackByLonWindow — устраняет «избыточность» (несколько
			// витков на карте) и «обрывы» в середине, даёт сплошную линию от края до края.
			obsLon := 0.0
			if obs := s.observer.Load(); obs != nil {
				obsLon = obs.Lon
			}
			track, err := tracker.GenerateGroundTrackByLonWindow(tle, now, obsLon, tracker.DefaultGroundTrackStep)
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

	obs := s.observer.Load()
	// AER (азимут, элевация, дальность от наблюдателя).
	aer := obs.GetAER(eci)
	// Радиальная скорость (км/с) → м/с для удобства фронта (избегаем дробных километров).
	rangeRateMps := tracker.RangeRateKmps(eci, obs) * 1000.0

	// Зона видимости (72 точки контура).
	zone := tracker.GenerateVisibilityZoneFromLLA(lla, sat.noradID, visibilityZonePoints)

	pd := &positionData{
		NoradID:        sat.noradID,
		Name:           sat.name,
		Lat:            roundTo(lla.LatDeg(), 4),
		Lon:            roundTo(lla.LonDeg(), 4),
		Alt:            roundTo(lla.Alt, 1),
		Az:             roundTo(aer.AzDeg(), 1),
		El:             roundTo(aer.ElDeg(), 1),
		Range:          roundTo(aer.Range, 1),
		RangeRate:      roundTo(rangeRateMps, 1),
		VisibilityZone: zone,
	}
	if flon, flat, rot := computeMapMarkerOrientation(sat, now, lla); flon != nil && flat != nil && rot != nil {
		pd.MapMarkerFwdLon = flon
		pd.MapMarkerFwdLat = flat
		r := roundTo(*rot, 2)
		pd.MapMarkerRotDeg = &r
	}
	return pd, nil
}

// computeMapMarkerOrientation — вторая точка пропагации (как шаг наземного трека) и запасной угол
// по хорде lat/lon на платовской карте (не азимут большого круга — совпадает с отрезками canvas).
func computeMapMarkerOrientation(
	sat *trackedSatellite, now time.Time, lla *tracker.LLA,
) (*float64, *float64, *float64) {
	if sat == nil || lla == nil {
		return nil, nil, nil
	}
	eci2, err := sat.propagator.Propagate(now.Add(mapMarkerRotPropagateStep))
	if err != nil {
		return nil, nil, nil
	}
	ecef2 := tracker.ECIToECEF(eci2)
	if ecef2 == nil {
		return nil, nil, nil
	}
	lla2 := tracker.ECEFToLLA(ecef2)
	if lla2 == nil {
		return nil, nil, nil
	}
	if tracker.GreatCircleAngularDistanceRad(lla, lla2) < 1e-9 {
		return nil, nil, nil
	}
	lonF := roundTo(lla2.LonDeg(), 4)
	latF := roundTo(lla2.LatDeg(), 4)
	r := tracker.MapMarkerRotDegPlatCarreChord(lla.LatDeg(), lla.LonDeg(), latF, lonF, 45)
	return &lonF, &latF, &r
}

// roundTo округляет число до заданного количества десятичных знаков.
func roundTo(val float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}
