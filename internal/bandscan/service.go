package bandscan

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/signalclass"
)

// EventBandSignals — SSE со списком сигналов в полосе тракта.
const EventBandSignals = "band_signals"

// emitInterval — как часто клиенты получают сигналы в полосе.
const emitInterval = time.Second

// BandSource — ширина годной середины окна тракта (хаб приёмников).
type BandSource interface {
	UsableBand(pathID string) (uint32, error)
}

// ExpectedSource — передатчики КА, которые сейчас должны звучать в окне centerHz ± spanHz/2.
type ExpectedSource interface {
	ExpectedInBand(centerHz uint64, spanHz uint32) []Expected
}

// Sink — получатель событий (рассылка SSE).
type Sink func(eventType string, data []byte)

// BandEvent — SSE band_signals (ADR-009 §11.2).
type BandEvent struct {
	RadioPath   string   `json:"radio_path"`
	TimestampMs uint64   `json:"timestamp_ms"`
	NoiseDBFS   float32  `json:"noise_dbfs"`
	Signals     []Signal `json:"signals"`
}

// ClassifyFunc — разбор отрезка потока: участок со сдвигом offsetHz от центра и шириной bandwidthHz.
// Вторым и третьим значением — вырезка и её частота отсчётов, для пробы вида передачи.
type ClassifyFunc func(iq []complex64, rate, offsetHz, bandwidthHz float64) (signalclass.Result, []complex64, float64)

// Service обнаруживает сигналы в потоке тракта попутно со зрителями и разбирает их структуру.
// Участки трактов хранятся до перезапуска сервера: новый поток продолжает их статистику.
type Service struct {
	band     BandSource
	expected ExpectedSource
	emit     Sink
	now      func() time.Time
	classify ClassifyFunc

	mu       sync.Mutex
	trackers map[string]*pathTracker
}

// classifySlice — вырезка участка и распознавание по структуре; ширина участка — подсказка
// для широкой ЧМ, когда внутренний замер полосы уже из‑за неровностей спектра.
func classifySlice(iq []complex64, rate, offsetHz, bandwidthHz float64) (signalclass.Result, []complex64, float64) {
	slice, sliceRate := signalclass.Extract(iq, rate, offsetHz, bandwidthHz)
	return signalclass.ClassifyHint(slice, sliceRate, bandwidthHz), slice, sliceRate
}

// pathTracker — участки тракта; поток прежнего сеанса может ещё дочитывать кольцо, когда начался новый.
type pathTracker struct {
	mu sync.Mutex
	tr *Tracker
}

// NewService создаёт обнаружитель.
func NewService(band BandSource, emit Sink) *Service {
	return &Service{
		band: band, emit: emit, now: time.Now, classify: classifySlice,
		trackers: make(map[string]*pathTracker),
	}
}

// WithExpected даёт ожидаемые передатчики КА: без них происхождение «КА» не определяется.
func (s *Service) WithExpected(src ExpectedSource) *Service {
	s.expected = src
	return s
}

// SignalsInBand — участки, которые звучат в окне centerHz ± spanHz/2 прямо сейчас.
// Нужен журналу визитов обхода: снимок на конец стоянки, без часовой памяти таблицы.
func (s *Service) SignalsInBand(pathID string, centerHz uint64, spanHz uint32) []Signal {
	if spanHz == 0 {
		return nil
	}
	var expected []Expected
	if s.expected != nil {
		expected = s.expected.ExpectedInBand(centerHz, spanHz)
	}
	pt := s.tracker(pathID)
	pt.mu.Lock()
	defer pt.mu.Unlock()
	all := pt.tr.active(expected)
	if len(all) == 0 {
		return nil
	}
	lo := float64(centerHz) - float64(spanHz)/2
	hi := float64(centerHz) + float64(spanHz)/2
	out := make([]Signal, 0, len(all))
	for _, sig := range all {
		c := float64(sig.CenterHz)
		if c >= lo && c <= hi {
			out = append(out, sig)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Service) tracker(pathID string) *pathTracker {
	s.mu.Lock()
	defer s.mu.Unlock()
	pt := s.trackers[pathID]
	if pt == nil {
		pt = &pathTracker{tr: NewTracker()}
		s.trackers[pathID] = pt
	}
	return pt
}

// Run обнаруживает сигналы до закрытия потока. Подходит как sdr.StreamTap.
func (s *Service) Run(pathID string, frames <-chan sdr.IQFrame) {
	r := &pathRun{
		svc: s, pathID: pathID, pt: s.tracker(pathID), det: NewDetector(),
		hist: newIQHistory(), results: make(chan classified, 1),
	}
	for f := range frames {
		r.frame(f)
	}
	// Разбор мог ещё идти: кадры теста/обрыва приходят быстрее, чем Extract на 2 с IQ.
	r.finishClassify()
	// Поток встал: участки не наблюдаются, клиенты убирают маркеры.
	s.send(pathID, r.last, []Signal{})
}

// classified — ответ разбора участка id.
type classified struct {
	id   int
	res  signalclass.Result
	iq   []complex64
	rate float64
}

// pathRun — состояние одного потока тракта. Разбор идёт в своей горутине, по одному
// на тракт: он дольше блока потока, а кольцо IQ копируется заранее.
type pathRun struct {
	svc      *Service
	pathID   string
	pt       *pathTracker
	det      *Detector
	hist     *iqHistory
	rate     uint32
	usableHz uint32
	last     Decision
	lastEmit time.Time
	results  chan classified
	busy     bool
}

func (r *pathRun) frame(f sdr.IQFrame) {
	if f.SampleRate != r.rate {
		r.rate = f.SampleRate
		var err error
		if r.usableHz, err = r.svc.band.UsableBand(r.pathID); err != nil {
			slog.Warn("bandscan: usable band", slog.String("radio_path", r.pathID), slog.Any("error", err))
			r.usableHz = r.rate
		}
	}
	r.hist.push(f)
	r.collect()
	dec, ok := r.det.Push(f, r.usableHz)
	if !ok {
		return
	}
	r.last = dec
	r.pt.mu.Lock()
	r.pt.tr.Update(dec)
	r.pt.mu.Unlock()
	if now := r.svc.now(); r.lastEmit.IsZero() || now.Sub(r.lastEmit) >= emitInterval {
		r.schedule()
		r.svc.send(r.pathID, dec, r.svc.signals(r.pt, dec, r.usableHz))
		r.lastEmit = now
	}
}

// collect забирает готовый ответ разбора, если он есть.
func (r *pathRun) collect() {
	select {
	case c := <-r.results:
		r.applyClassified(c)
	default:
	}
}

// finishClassify ждёт ответ текущего разбора и шлёт band_signals уже со structure.
func (r *pathRun) finishClassify() {
	if !r.busy {
		return
	}
	select {
	case c := <-r.results:
		r.applyClassified(c)
		r.svc.send(r.pathID, r.last, r.svc.signals(r.pt, r.last, r.usableHz))
	case <-time.After(15 * time.Second):
		slog.Warn("bandscan: classify still busy when stream ended",
			slog.String("radio_path", r.pathID))
	}
}

func (r *pathRun) applyClassified(c classified) {
	r.busy = false
	r.pt.mu.Lock()
	r.pt.tr.setStructure(c.id, c.res, c.iq, c.rate)
	r.pt.mu.Unlock()
}

// schedule ставит следующий участок на разбор, если прежний разбор закончен.
func (r *pathRun) schedule() {
	if r.busy {
		return
	}
	r.pt.mu.Lock()
	job, ok := r.pt.tr.nextJob(r.hist.startMs())
	r.pt.mu.Unlock()
	if !ok {
		return
	}
	iq, ok := r.hist.window(job.fromMs, job.toMs)
	if !ok {
		// nextJob уже поставил classifiedMs — без сброса участок молчит 10 с.
		r.pt.mu.Lock()
		for _, s := range r.pt.tr.segs {
			if s.id == job.id {
				s.classifiedMs = 0
				break
			}
		}
		r.pt.mu.Unlock()
		return
	}
	r.busy = true
	rate := float64(r.hist.rate)
	offset := job.centerHz - float64(r.hist.centerHz)
	// В плане ЧМ вырезка не уже канала с RDS, иначе узкий горб срезает 57 кГц.
	bw := signalclass.ExtractBandwidthAt(job.centerHz, job.bandwidthHz)
	classify := r.svc.classify
	go func() {
		res, slice, sliceRate := classify(iq, rate, offset, bw)
		r.results <- classified{id: job.id, res: res, iq: slice, rate: sliceRate}
	}()
}

func (s *Service) signals(pt *pathTracker, dec Decision, usableHz uint32) []Signal {
	var expected []Expected
	if s.expected != nil {
		expected = s.expected.ExpectedInBand(dec.CenterHz, usableHz)
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if sigs := pt.tr.Signals(expected); sigs != nil {
		return sigs
	}
	return []Signal{}
}

func (s *Service) send(pathID string, dec Decision, sigs []Signal) {
	data, err := json.Marshal(BandEvent{
		RadioPath: pathID, TimestampMs: dec.TimeMs, NoiseDBFS: dec.NoiseDBFS, Signals: sigs,
	})
	if err != nil {
		slog.Error("bandscan: marshal event", slog.Any("error", err))
		return
	}
	s.emit(EventBandSignals, data)
}
