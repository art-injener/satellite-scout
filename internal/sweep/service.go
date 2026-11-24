package sweep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

// EventSweepState — SSE с состоянием обхода тракта.
const EventSweepState = "sweep_state"

const (
	// snapshotInterval — как часто обновляется снимок текущего окна.
	snapshotInterval = 250 * time.Millisecond
	// snapshotAvg — сколько снимков в скользящем среднем: 2 с при snapshotInterval.
	snapshotAvg = 8
	// fftSize — БПФ снимка: 1 кГц на бин при 2,048 МС/с, узкие несущие различимы на панораме.
	fftSize = 2048
	// settleDiv — после перестройки пропускается 1/settleDiv секунды отсчётов (50 мс):
	// синтезатор тюнера встаёт, а буфер устройства ещё отдаёт отсчёты прежней частоты.
	settleDiv = 20
	// watchBuffer — снимков в очереди медленного зрителя панорамы; лишние выбрасываются.
	watchBuffer = 16
)

// ErrNotConfigured — у тракта нет диапазонов обхода в конфиге.
var ErrNotConfigured = errors.New("sweep is not configured for radio path")

// ErrNoTargets — политика tx, а в плане нет активных TX на горизонте.
var ErrNoTargets = errors.New("sweep: no active plan transmitters")

// Policy — как строить окна обхода.
type Policy string

const (
	// PolicySurvey — конфиг ∪ кластеры плана (центры span для панорамы).
	PolicySurvey Policy = "survey"
	// PolicyTX — по одному окну на частоту TX плана (Авто приём).
	PolicyTX Policy = "tx"
)

// Target — цель обхода по TX или эфиру (частота и подпись для UI).
type Target struct {
	FreqHz  uint64
	Label   string
	NoradID int
	Air     bool    // цель из band_signals («+эфир»), не TX плана
	Demod   string  // подсказка профиля с классификатора; пусто — «Авто»
	Baud    float64 // скорость для гибких профилей (OOK/зонды)
}

// StartOptions — запуск обхода: политика и опции Авто приёма.
type StartOptions struct {
	Policy     Policy
	IncludeAir bool // к TX плана добавить цели из эфира (band_signals)
}

// Горизонт плана для политики tx: как окно ленты группы (~1 мин), не час обзора.
// Иначе в обход попадают восходящие через десятки минут, которых нет в списке видимых КА.
const txHorizon = time.Minute

// Hub — хаб приёмников: поток тракта, приёмник и годная полоса.
type Hub interface {
	Subscribe(pathID string) (<-chan sdr.IQFrame, func(), error)
	Get(pathID string) (sdr.Receiver, error)
	UsableBand(pathID string) (uint32, error)
}

// Sink — получатель событий (рассылка SSE).
type Sink func(eventType string, data []byte)

// Clock — время и таймеры обхода; в тестах подменяется.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	Ticker(d time.Duration) (<-chan time.Time, func())
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (realClock) Ticker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// Window — окно обхода: одна настройка приёмника.
type Window struct {
	CenterHz uint64  `json:"center_hz"`
	SpanHz   uint32  `json:"span_hz"`
	Label    string  `json:"label,omitempty"`
	NoradID  int     `json:"norad_id,omitempty"`
	Air      bool    `json:"air,omitempty"`
	Demod    string  `json:"demod,omitempty"`
	Baud     float64 `json:"baud,omitempty"`
}

// State — SSE sweep_state и ответ GET /api/sdr/sweep.
type State struct {
	RadioPath     string   `json:"radio_path"`
	Running       bool     `json:"running"`
	Policy        Policy   `json:"policy,omitempty"`
	DwellS        int      `json:"dwell_s"`
	Windows       []Window `json:"windows"`
	Index         int      `json:"index"`
	Cycle         int      `json:"cycle"`
	WindowStartMs int64    `json:"window_start_ms"`
	WindowEndMs   int64    `json:"window_end_ms"`
}

// Snapshot — усреднённый спектр окна в dBFS, только годная середина; TimeMs — время снимка.
type Snapshot struct {
	CenterHz uint64
	SpanHz   uint32
	TimeMs   int64
	DBFS     []float32
}

// DwellEndFunc вызывается в конце стоянки на окне, до перестройки на следующее.
type DwellEndFunc func(pathID string, w Window, startMs, endMs int64)

// PlanFreqsFunc — частоты TX плана на горизонте [now, until] для тракта.
// Не должна вызывать методы Service (окна считаются под мьютексом).
type PlanFreqsFunc func(pathID string, now, until time.Time) []uint64

// PlanTargetsFunc — цели TX плана с именами для политики tx.
type PlanTargetsFunc func(pathID string, now, until time.Time) []Target

// AirTargetsFunc — цели из обнаружителя/классификатора («+эфир»).
type AirTargetsFunc func(pathID string) []Target

// DwellFunc — до какого момента (Unix мс) стоять на окне.
// configDwellMs — dwell_s тракта. holdUntil <= now означает «уйти».
// Без зависимости от bandscan/freqplan: снимок эфира собирает wiring.
type DwellFunc func(pathID string, w Window, startMs, nowMs, configDwellMs int64) (holdUntilMs int64)

// AimFunc — наведение канала на пик вспышки во время стоянки (тикеры снимка, не каждый БПФ).
type AimFunc func(pathID string, w Window, startMs, nowMs int64)

// Service обходит тракты по диапазонам конфига ∪ кластерам плана
// или по TX плана (политика tx). Обход держит поток тракта как зритель.
// Снимки окон живут до перезапуска сервера. Список окон пересчитывается
// на границе цикла. Стоянка — DwellFunc или жёсткий dwell.
type Service struct {
	hub   Hub
	emit  Sink
	clock Clock

	onDwellEnd  DwellEndFunc
	planFreqs   PlanFreqsFunc
	planTargets PlanTargetsFunc
	airTargets  AirTargetsFunc
	dwellFn     DwellFunc
	aimFn       AimFunc

	mu    sync.Mutex
	paths map[string]*path
}

type path struct {
	ranges     []Range
	dwell      time.Duration
	policy     Policy
	includeAir bool
	run        *run
	state      State
	snaps      map[uint64]Snapshot
	watchers   map[chan Snapshot]struct{}
}

type run struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewService создаёт обход без настроенных трактов.
func NewService(hub Hub, emit Sink) *Service {
	return &Service{hub: hub, emit: emit, clock: realClock{}, paths: make(map[string]*path)}
}

// OnDwellEnd задаёт обработчик конца стоянки (журнал визитов панорамы).
func (s *Service) OnDwellEnd(fn DwellEndFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDwellEnd = fn
}

// WithPlanFreqs подключает частоты плана: окна survey = конфиг ∪ жадные кластеры.
func (s *Service) WithPlanFreqs(fn PlanFreqsFunc) *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.planFreqs = fn
	return s
}

// WithPlanTargets подключает цели TX с подписями для политики tx.
func (s *Service) WithPlanTargets(fn PlanTargetsFunc) *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.planTargets = fn
	return s
}

// WithAirTargets подключает цели эфира для опции «+эфир» при политике tx.
func (s *Service) WithAirTargets(fn AirTargetsFunc) *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.airTargets = fn
	return s
}

// WithDwell подключает гибкую стоянку; nil — жёсткий таймер dwell конфига.
func (s *Service) WithDwell(fn DwellFunc) *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dwellFn = fn
	return s
}

// WithAim — наведение OffsetHz на вспышку на каждом снимке окна (~4 Гц).
func (s *Service) WithAim(fn AimFunc) *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aimFn = fn
	return s
}

func (s *Service) withClock(c Clock) *Service {
	s.clock = c
	return s
}

// Configure задаёт диапазоны и стоянку тракта. Вызывается при старте сервера.
func (s *Service) Configure(pathID string, ranges []Range, dwell time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths[pathID] = &path{
		ranges: slices.Clone(ranges), dwell: dwell,
		snaps: make(map[uint64]Snapshot), watchers: make(map[chan Snapshot]struct{}),
	}
}

// Configured сообщает, есть ли у тракта диапазоны обхода.
func (s *Service) Configured(pathID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paths[pathID] != nil
}

func (s *Service) path(pathID string) (*path, error) {
	p := s.paths[pathID]
	if p == nil {
		return nil, fmt.Errorf("%w %q", ErrNotConfigured, pathID)
	}
	return p, nil
}

// windows — окна тракта при политике survey (конфиг ∪ план).
func (s *Service) windows(pathID string, p *path) ([]Window, uint32, error) {
	return s.windowsHorizon(pathID, p, 0)
}

// windowsFor выбирает построение окон по политике.
func (s *Service) windowsFor(pathID string, p *path, policy Policy, horizonN int) ([]Window, uint32, error) {
	if policy == PolicyTX {
		return s.windowsTX(pathID)
	}
	return s.windowsHorizon(pathID, p, horizonN)
}

// windowsTX — по одному окну на частоту TX плана; тюнер на частоту передатчика.
// При includeAir — плюс цели из эфира; если целей нет — середины ranges (разведка).
func (s *Service) windowsTX(pathID string) ([]Window, uint32, error) {
	band, err := s.hub.UsableBand(pathID)
	if err != nil {
		return nil, 0, err
	}
	p := s.paths[pathID]
	now := s.clock.Now()
	until := now.Add(txHorizon)
	var targets []Target
	if s.planTargets != nil {
		targets = s.planTargets(pathID, now, until)
	} else if s.planFreqs != nil {
		for _, hz := range s.planFreqs(pathID, now, until) {
			targets = append(targets, Target{FreqHz: hz})
		}
	}
	if p != nil && p.includeAir && s.airTargets != nil {
		targets = mergeTargets(targets, s.airTargets(pathID))
	}
	if len(targets) == 0 && p != nil && p.includeAir {
		targets = exploreRangeTargets(p.ranges, band)
	}
	if len(targets) == 0 {
		return nil, band, nil
	}
	out := make([]Window, len(targets))
	for i, t := range targets {
		out[i] = Window{
			CenterHz: t.FreqHz, SpanHz: band,
			Label: t.Label, NoradID: t.NoradID,
			Air: t.Air, Demod: t.Demod, Baud: t.Baud,
		}
	}
	return out, band, nil
}

// airMergeHz — участок эфира ближе к TX плана сливается в hop TX (наведение добьёт Offset).
// Шире типичного Доплера/ошибки каталога на UHF, чтобы импульс у номинала не плодил второй hop.
const airMergeHz = 100_000

// mergeTargets: сначала TX плана (КА), затем эфир без дублей у TX.
func mergeTargets(tx, air []Target) []Target {
	out := append([]Target(nil), tx...)
	for _, a := range air {
		dup := false
		for _, t := range tx {
			if absU64(t.FreqHz, a.FreqHz) <= airMergeHz {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, a)
		}
	}
	return out
}

func exploreRangeTargets(ranges []Range, band uint32) []Target {
	var out []Target
	for _, r := range ranges {
		var hz uint64
		switch {
		case r.CenterHz > 0:
			hz = r.CenterHz
		case r.LoHz > 0 && r.HiHz > r.LoHz:
			hz = r.LoHz + (r.HiHz-r.LoHz)/2
		default:
			continue
		}
		out = append(out, Target{FreqHz: hz, Label: "разведка", Air: true})
	}
	_ = band
	return out
}

func absU64(a, b uint64) uint64 {
	if a >= b {
		return a - b
	}
	return b - a
}

// windowsHorizon режет диапазоны на окна. horizonN > 0 — оценка длины цикла
// для горизонта плана; иначе берётся число окон только из ranges конфига.
func (s *Service) windowsHorizon(pathID string, p *path, horizonN int) ([]Window, uint32, error) {
	band, err := s.hub.UsableBand(pathID)
	if err != nil {
		return nil, 0, err
	}
	ranges := p.ranges
	if s.planFreqs != nil {
		n := horizonN
		if n <= 0 {
			n = len(Windows(p.ranges, band))
		}
		if n <= 0 {
			n = 1
		}
		now := s.clock.Now()
		until := now.Add(time.Duration(n) * p.dwell)
		ranges = MergeRanges(p.ranges, Clusters(s.planFreqs(pathID, now, until), band))
	}
	centers := Windows(ranges, band)
	out := make([]Window, len(centers))
	for i, c := range centers {
		out[i] = Window{CenterHz: c, SpanHz: band}
	}
	return out, band, nil
}

// State возвращает состояние обхода; без обхода — окна при текущей полосе.
func (s *Service) State(pathID string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(pathID)
	if err != nil {
		return State{}, err
	}
	if p.run != nil {
		return cloneState(p.state), nil
	}
	ws, _, err := s.windows(pathID, p)
	if err != nil {
		return State{}, err
	}
	return State{RadioPath: pathID, DwellS: int(p.dwell / time.Second), Windows: ws}, nil
}

// Start — обход survey (конфиг ∪ кластеры). Идущий обход не перезапускается.
func (s *Service) Start(pathID string) error {
	return s.StartWith(pathID, StartOptions{Policy: PolicySurvey})
}

// StartPolicy ставит приёмник на первое окно и запускает обход выбранной политики.
func (s *Service) StartPolicy(pathID string, policy Policy) error {
	return s.StartWith(pathID, StartOptions{Policy: policy})
}

// StartWith — обход с опциями (политика tx и «+эфир»).
func (s *Service) StartWith(pathID string, opt StartOptions) error {
	policy := opt.Policy
	if policy == "" {
		policy = PolicySurvey
	}
	s.mu.Lock()
	p, err := s.path(pathID)
	if err != nil || p.run != nil {
		s.mu.Unlock()
		return err
	}
	p.includeAir = opt.IncludeAir && policy == PolicyTX
	ws, band, err := s.windowsFor(pathID, p, policy, 0)
	if err == nil && len(ws) == 0 {
		if policy == PolicyTX {
			err = fmt.Errorf("%w for %q", ErrNoTargets, pathID)
		} else {
			err = fmt.Errorf("radio path %q: no sweep windows", pathID)
		}
	}
	if err != nil {
		p.includeAir = false
		s.mu.Unlock()
		return err
	}
	receiver, err := s.hub.Get(pathID)
	if err != nil {
		p.includeAir = false
		s.mu.Unlock()
		return err
	}
	frames, unsubscribe, err := s.hub.Subscribe(pathID)
	if err != nil {
		p.includeAir = false
		s.mu.Unlock()
		return err
	}
	if err := receiver.Tune(ws[0].CenterHz); err != nil {
		p.includeAir = false
		s.mu.Unlock()
		unsubscribe()
		return fmt.Errorf("radio path %q: tune: %w", pathID, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{cancel: cancel, done: make(chan struct{})}
	p.run = r
	p.policy = policy
	now := s.clock.Now()
	startMs := now.UnixMilli()
	endMs := evalHold(s.dwellFn, pathID, ws[0], startMs, startMs, p.dwell)
	p.state = State{
		RadioPath: pathID, Running: true, Policy: policy,
		DwellS: int(p.dwell / time.Second), Windows: ws,
		Index: 0, Cycle: 1, WindowStartMs: startMs, WindowEndMs: endMs,
	}
	st := cloneState(p.state)
	s.mu.Unlock()

	slog.Info("sweep started", slog.String("radio_path", pathID), slog.String("policy", string(policy)),
		slog.Bool("include_air", opt.IncludeAir),
		slog.Int("windows", len(ws)), slog.Duration("dwell", p.dwell))
	s.sendState(st)
	go s.loop(ctx, pathID, p, r, loopParams{
		receiver: receiver, frames: frames, unsubscribe: unsubscribe, windows: ws, band: band,
		holdUntilMs: endMs, policy: policy,
	})
	return nil
}

// Stop останавливает обход и ждёт его завершения: после возврата обход приёмник не трогает.
// Частота остаётся на последнем окне. false — обход не шёл.
func (s *Service) Stop(pathID string) bool {
	s.mu.Lock()
	p := s.paths[pathID]
	if p == nil || p.run == nil {
		s.mu.Unlock()
		return false
	}
	r := p.run
	p.run = nil
	p.policy = ""
	p.includeAir = false
	p.state.Running = false
	p.state.Policy = ""
	st := cloneState(p.state)
	s.mu.Unlock()

	r.cancel()
	<-r.done
	slog.Info("sweep stopped", slog.String("radio_path", pathID))
	s.sendState(st)
	return true
}

// Watch возвращает сохранённые снимки окон по возрастанию частоты и канал новых снимков.
// Медленный зритель теряет снимки, не задерживая обход.
func (s *Service) Watch(pathID string) ([]Snapshot, <-chan Snapshot, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(pathID)
	if err != nil {
		return nil, nil, nil, err
	}
	snaps := make([]Snapshot, 0, len(p.snaps))
	for _, sn := range p.snaps {
		snaps = append(snaps, sn)
	}
	slices.SortFunc(snaps, func(a, b Snapshot) int { return compareHz(a.CenterHz, b.CenterHz) })
	ch := make(chan Snapshot, watchBuffer)
	p.watchers[ch] = struct{}{}
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(p.watchers, ch)
		})
	}
	return snaps, ch, cancel, nil
}

type loopParams struct {
	receiver    sdr.Receiver
	frames      <-chan sdr.IQFrame
	unsubscribe func()
	windows     []Window
	band        uint32
	holdUntilMs int64
	policy      Policy
}

// loop стоит на окне до holdUntil, при продлении шлёт новый sweep_state, иначе — следующее окно.
func (s *Service) loop(ctx context.Context, pathID string, p *path, r *run, lp loopParams) {
	defer close(r.done)
	defer lp.unsubscribe()
	tick, stopTick := s.clock.Ticker(snapshotInterval)
	defer stopTick()
	dwell := s.armHold(lp.holdUntilMs)
	acc := newAccumulator(lp.windows[0].CenterHz, lp.band)
	idx, cycle := 0, 1

	for {
		select {
		case <-ctx.Done():
			return
		case f, ok := <-lp.frames:
			if !ok {
				s.finish(pathID, p, r)
				return
			}
			acc.push(f)
		case <-tick:
			now := s.clock.Now()
			s.publish(p, acc.snapshot(now))
			s.runAim(pathID, p, r, lp.windows[idx], now.UnixMilli())
		case <-dwell:
			s.publish(p, acc.snapshot(s.clock.Now()))
			// Дедлайн сработал: даже если тестовые часы не тикнули, считаем время = holdUntil.
			nowMs := s.clock.Now().UnixMilli()
			if nowMs < lp.holdUntilMs {
				nowMs = lp.holdUntilMs
			}
			startMs := s.windowStartMs(p, r)
			next := s.holdUntil(pathID, lp.windows[idx], startMs, nowMs, p.dwell)
			if next > nowMs {
				lp.holdUntilMs = next
				s.extendHold(p, r, next)
				dwell = s.armHold(next)
				continue
			}
			s.notifyDwellEnd(pathID, p, lp.windows[idx])
			prevHz := lp.windows[idx].CenterHz
			if lp.policy == PolicyTX {
				// Авто приём: на каждом hop заново сверяем план с лентой видимых КА.
				ws, band, ok := s.rebuildWindows(pathID, p, r, len(lp.windows))
				if !ok {
					s.finish(pathID, p, r)
					return
				}
				lp.windows, lp.band = ws, band
				next, wrapped := nextAfterHz(lp.windows, prevHz)
				if wrapped {
					cycle++
				}
				idx = next
			} else {
				idx = (idx + 1) % len(lp.windows)
				if idx == 0 {
					cycle++
					ws, band, ok := s.rebuildWindows(pathID, p, r, len(lp.windows))
					if !ok {
						s.finish(pathID, p, r)
						return
					}
					lp.windows, lp.band = ws, band
				}
			}
			if err := lp.receiver.Tune(lp.windows[idx].CenterHz); err != nil {
				slog.Warn("sweep: tune failed", slog.String("radio_path", pathID), slog.Any("error", err))
				s.finish(pathID, p, r)
				return
			}
			acc.retune(lp.windows[idx].CenterHz)
			now := s.clock.Now()
			startMs = now.UnixMilli()
			lp.holdUntilMs = s.holdUntil(pathID, lp.windows[idx], startMs, startMs, p.dwell)
			dwell = s.armHold(lp.holdUntilMs)
			s.advance(p, r, idx, cycle, startMs, lp.holdUntilMs)
		}
	}
}

// holdUntil спрашивает политику стоянки; без неё — start + dwell конфига.
func (s *Service) holdUntil(pathID string, w Window, startMs, nowMs int64, dwell time.Duration) int64 {
	s.mu.Lock()
	fn := s.dwellFn
	s.mu.Unlock()
	return evalHold(fn, pathID, w, startMs, nowMs, dwell)
}

// runAim — наведение канала на тике снимка (~4 Гц), пока обход на окне.
func (s *Service) runAim(pathID string, p *path, r *run, w Window, nowMs int64) {
	s.mu.Lock()
	fn := s.aimFn
	startMs := int64(0)
	if p.run == r {
		startMs = p.state.WindowStartMs
	}
	s.mu.Unlock()
	if fn == nil || startMs == 0 {
		return
	}
	fn(pathID, w, startMs, nowMs)
}

func evalHold(fn DwellFunc, pathID string, w Window, startMs, nowMs int64, dwell time.Duration) int64 {
	cfgMs := dwell.Milliseconds()
	if fn == nil {
		end := startMs + cfgMs
		if nowMs >= end {
			return nowMs
		}
		return end
	}
	return fn(pathID, w, startMs, nowMs, cfgMs)
}

func (s *Service) armHold(untilMs int64) <-chan time.Time {
	now := s.clock.Now()
	d := time.UnixMilli(untilMs).Sub(now)
	if d < 0 {
		d = 0
	}
	return s.clock.After(d)
}

func (s *Service) windowStartMs(p *path, r *run) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.run != r {
		return 0
	}
	return p.state.WindowStartMs
}

// extendHold продлевает стоянку и шлёт sweep_state с новым window_end_ms.
func (s *Service) extendHold(p *path, r *run, untilMs int64) {
	s.mu.Lock()
	if p.run != r {
		s.mu.Unlock()
		return
	}
	p.state.WindowEndMs = untilMs
	st := cloneState(p.state)
	s.mu.Unlock()
	s.sendState(st)
}

// nextAfterHz — индекс окна с частотой строго выше afterHz; иначе 0 и wrapped.
// Окна считаются упорядоченными по возрастанию частоты (как ActiveTargets).
func nextAfterHz(windows []Window, afterHz uint64) (idx int, wrapped bool) {
	for i, w := range windows {
		if w.CenterHz > afterHz {
			return i, false
		}
	}
	return 0, true
}

// rebuildWindows пересчитывает окна на границе цикла (survey) или каждом hop (tx).
// false — обход уже остановлен, ошибка полосы или окон не осталось.
func (s *Service) rebuildWindows(pathID string, p *path, r *run, prevN int) ([]Window, uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.run != r {
		return nil, 0, false
	}
	ws, band, err := s.windowsFor(pathID, p, p.policy, prevN)
	if err != nil {
		slog.Warn("sweep: rebuild windows", slog.String("radio_path", pathID), slog.Any("error", err))
		return nil, 0, false
	}
	if len(ws) == 0 {
		return nil, 0, false
	}
	p.state.Windows = slices.Clone(ws)
	return ws, band, true
}

// notifyDwellEnd сообщает о завершённой стоянке на w (до смены index).
func (s *Service) notifyDwellEnd(pathID string, p *path, w Window) {
	s.mu.Lock()
	fn := s.onDwellEnd
	startMs := p.state.WindowStartMs
	s.mu.Unlock()
	if fn == nil {
		return
	}
	fn(pathID, w, startMs, s.clock.Now().UnixMilli())
}

// advance отмечает переход на окно idx; остановленный обход не трогает.
func (s *Service) advance(p *path, r *run, idx, cycle int, startMs, endMs int64) {
	s.mu.Lock()
	if p.run != r {
		s.mu.Unlock()
		return
	}
	p.state.Index, p.state.Cycle = idx, cycle
	p.state.WindowStartMs, p.state.WindowEndMs = startMs, endMs
	st := cloneState(p.state)
	s.mu.Unlock()
	s.sendState(st)
}

// finish — обход кончился сам: поток устройства встал или перестройка не удалась.
func (s *Service) finish(pathID string, p *path, r *run) {
	s.mu.Lock()
	if p.run != r {
		s.mu.Unlock()
		return
	}
	p.run = nil
	p.policy = ""
	p.includeAir = false
	p.state.Running = false
	p.state.Policy = ""
	st := cloneState(p.state)
	s.mu.Unlock()
	slog.Info("sweep ended with stream", slog.String("radio_path", pathID))
	s.sendState(st)
}

// publish сохраняет снимок окна и раздаёт зрителям панорамы.
func (s *Service) publish(p *path, sn *Snapshot) {
	if sn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p.snaps[sn.CenterHz] = *sn
	for ch := range p.watchers {
		select {
		case ch <- *sn:
		default:
		}
	}
}

func (s *Service) sendState(st State) {
	data, err := json.Marshal(st)
	if err != nil {
		slog.Error("sweep: marshal state", slog.Any("error", err))
		return
	}
	s.emit(EventSweepState, data)
}

func cloneState(st State) State {
	st.Windows = slices.Clone(st.Windows)
	return st
}

func compareHz(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// accumulator копит спектр текущего окна из блоков его частоты.
type accumulator struct {
	center uint64
	band   uint32
	rate   uint32
	skip   int // отсчётов до конца установления; -1 — ещё не было блока окна
	pipe   *dsp.SpectrumPipeline
	buf    []complex64
	n      int
}

func newAccumulator(center uint64, band uint32) *accumulator {
	pipe := dsp.NewSpectrumPipeline(fftSize, fftSize, dsp.ModeAvg, dsp.WindowBlackmanHarris)
	pipe.SetAvgFrames(snapshotAvg)
	return &accumulator{center: center, band: band, skip: -1, pipe: pipe, buf: make([]complex64, fftSize)}
}

// retune начинает новое окно: накопленное и недособранный блок относятся к прежнему.
func (a *accumulator) retune(center uint64) {
	a.center, a.skip, a.n = center, -1, 0
	a.pipe.SetAvgFrames(snapshotAvg)
}

func (a *accumulator) push(f sdr.IQFrame) {
	if f.CenterHz != a.center || f.SampleRate == 0 {
		return
	}
	if f.SampleRate != a.rate {
		a.rate, a.n = f.SampleRate, 0
		a.pipe.SetAvgFrames(snapshotAvg)
	}
	samples := f.Samples
	if a.skip < 0 {
		a.skip = int(a.rate / settleDiv)
	}
	if a.skip > 0 {
		k := min(a.skip, len(samples))
		a.skip -= k
		samples = samples[k:]
	}
	for len(samples) > 0 {
		c := copy(a.buf[a.n:], samples)
		a.n += c
		samples = samples[c:]
		if a.n == len(a.buf) {
			a.pipe.Push(a.buf)
			a.n = 0
		}
	}
}

// snapshot — среднее за последние snapshotAvg снимков; nil — с прошлого снимка блоков окна не было.
func (a *accumulator) snapshot(now time.Time) *Snapshot {
	dbfs := a.pipe.Flush()
	if dbfs == nil {
		return nil
	}
	start, keep, span := dsp.CropBand(len(dbfs), a.rate, a.band)
	return &Snapshot{
		CenterHz: a.center,
		SpanHz:   uint32(span),
		TimeMs:   now.UnixMilli(),
		DBFS:     slices.Clone(dbfs[start : start+keep]),
	}
}
