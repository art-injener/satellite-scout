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
	CenterHz uint64 `json:"center_hz"`
	SpanHz   uint32 `json:"span_hz"`
}

// State — SSE sweep_state и ответ GET /api/sdr/sweep.
type State struct {
	RadioPath     string   `json:"radio_path"`
	Running       bool     `json:"running"`
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

// Service обходит тракты по окнам из конфига. Обход держит поток тракта как зритель,
// поэтому попутные получатели (обнаружитель, демодулятор) работают и без открытой страницы.
// Снимки окон живут до перезапуска сервера.
type Service struct {
	hub   Hub
	emit  Sink
	clock Clock

	onDwellEnd DwellEndFunc

	mu    sync.Mutex
	paths map[string]*path
}

type path struct {
	ranges   []Range
	dwell    time.Duration
	run      *run
	state    State
	snaps    map[uint64]Snapshot
	watchers map[chan Snapshot]struct{}
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

// windows — окна тракта при текущей годной полосе.
func (s *Service) windows(pathID string, p *path) ([]Window, uint32, error) {
	band, err := s.hub.UsableBand(pathID)
	if err != nil {
		return nil, 0, err
	}
	centers := Windows(p.ranges, band)
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

// Start ставит приёмник на первое окно и запускает обход. Идущий обход не перезапускается.
func (s *Service) Start(pathID string) error {
	s.mu.Lock()
	p, err := s.path(pathID)
	if err != nil || p.run != nil {
		s.mu.Unlock()
		return err
	}
	ws, band, err := s.windows(pathID, p)
	if err == nil && len(ws) == 0 {
		err = fmt.Errorf("radio path %q: no sweep windows", pathID)
	}
	if err != nil {
		s.mu.Unlock()
		return err
	}
	receiver, err := s.hub.Get(pathID)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	frames, unsubscribe, err := s.hub.Subscribe(pathID)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if err := receiver.Tune(ws[0].CenterHz); err != nil {
		s.mu.Unlock()
		unsubscribe()
		return fmt.Errorf("radio path %q: tune: %w", pathID, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{cancel: cancel, done: make(chan struct{})}
	p.run = r
	now := s.clock.Now()
	p.state = State{
		RadioPath: pathID, Running: true, DwellS: int(p.dwell / time.Second), Windows: ws,
		Index: 0, Cycle: 1, WindowStartMs: now.UnixMilli(), WindowEndMs: now.Add(p.dwell).UnixMilli(),
	}
	st := cloneState(p.state)
	s.mu.Unlock()

	slog.Info("sweep started", slog.String("radio_path", pathID), slog.Int("windows", len(ws)),
		slog.Duration("dwell", p.dwell))
	s.sendState(st)
	go s.loop(ctx, pathID, p, r, loopParams{
		receiver: receiver, frames: frames, unsubscribe: unsubscribe, windows: ws, band: band,
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
	p.state.Running = false
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
}

// loop стоит на окне dwell, затем перестраивает приёмник на следующее, по кругу.
func (s *Service) loop(ctx context.Context, pathID string, p *path, r *run, lp loopParams) {
	defer close(r.done)
	defer lp.unsubscribe()
	tick, stopTick := s.clock.Ticker(snapshotInterval)
	defer stopTick()
	dwell := s.clock.After(p.dwell)
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
			s.publish(p, acc.snapshot(s.clock.Now()))
		case <-dwell:
			s.publish(p, acc.snapshot(s.clock.Now()))
			s.notifyDwellEnd(pathID, p, lp.windows[idx])
			idx = (idx + 1) % len(lp.windows)
			if idx == 0 {
				cycle++
			}
			if err := lp.receiver.Tune(lp.windows[idx].CenterHz); err != nil {
				slog.Warn("sweep: tune failed", slog.String("radio_path", pathID), slog.Any("error", err))
				s.finish(pathID, p, r)
				return
			}
			acc.retune(lp.windows[idx].CenterHz)
			dwell = s.clock.After(p.dwell)
			s.advance(p, r, idx, cycle)
		}
	}
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
func (s *Service) advance(p *path, r *run, idx, cycle int) {
	s.mu.Lock()
	if p.run != r {
		s.mu.Unlock()
		return
	}
	now := s.clock.Now()
	p.state.Index, p.state.Cycle = idx, cycle
	p.state.WindowStartMs, p.state.WindowEndMs = now.UnixMilli(), now.Add(p.dwell).UnixMilli()
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
	p.state.Running = false
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
