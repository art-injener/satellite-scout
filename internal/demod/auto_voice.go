package demod

import (
	"log/slog"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

// Выбор голоса в «Авто»: у голоса нет CRC, поэтому признак — долго открытый порог
// при молчащих кадровых кандидатах.
const (
	voiceLockOpen   = 2 * time.Second // суммарно открытый порог в окне для выбора голоса
	voiceLockWindow = 5 * time.Second // окно, в котором считается открытый порог и синхрослова
)

// voiceListener — звук голосового профиля поверх общего грубого фильтра «Авто»:
// свой точный фильтр с полосой профиля и звуковой демодулятор, как в Chain.
type voiceListener struct {
	id         string
	mid        float64
	bandwidth  uint32
	passbandHz float64
	fine       *dsp.XlatingFIR
	rx         audioRx
	audio      []float32
}

func newVoiceListener(id string, mid float64, bandwidth uint32) *voiceListener {
	p, ok := profiles[id]
	if !ok || p.newAudio == nil || mid <= 0 {
		return nil
	}
	fine, cutoff := newFineFilter(p, mid, bandwidth)
	beatHz := p.toneHz + p.sideband*(ssbLowHz+cutoff)
	return &voiceListener{
		id: id, mid: mid, bandwidth: bandwidth, passbandHz: 2 * cutoff,
		fine: fine, rx: p.newAudio(mid/float64(p.fineDecim), beatHz),
	}
}

// push демодулирует блок грубого фильтра; под закрытым порогом идёт тишина той же длины.
func (l *voiceListener) push(coarse []complex64, open bool) []float32 {
	l.audio = l.rx.Demodulate(l.audio[:0], l.fine.Process(coarse))
	if !open {
		clear(l.audio)
	}
	return l.audio
}

// openSpan — открытый блок потока: конец и длительность, мс.
type openSpan struct {
	endMs, durMs uint64
}

// voiceState — мягкий выбор голоса в фазе подбора. Поиск кадров, VIS и CW идёт
// и под голосом: их закрепление его вытесняет. Время — по потоку, чтобы запись
// и эфир вели себя одинаково.
type voiceState struct {
	listener  *voiceListener
	open      []openSpan
	lastOpen  uint64 // время потока последнего открытого блока выбранного голоса
	syncs     int    // сумма синхрослов кадровых кандидатов на прошлом блоке
	syncAt    uint64 // время потока последнего нового синхрослова
	syncFresh bool
}

func (v *voiceState) reset() {
	v.listener = nil
	v.open = v.open[:0]
}

// voiceCandidate — первый по приоритету голос среди кандидатов.
func (a *AutoPicker) voiceCandidate() string {
	for _, id := range a.candidates {
		if isAutoVoiceProfile(id) {
			return id
		}
	}
	return ""
}

// activeVoice — выбранный голос; пусто, пока голос не выбран.
func (a *AutoPicker) activeVoice() string {
	if a.voice.listener == nil {
		return ""
	}
	return a.voice.listener.id
}

// noteSyncWords отмечает новые синхрослова кадровых кандидатов: цифровой сигнал не голос.
func (a *AutoPicker) noteSyncWords(nowMs uint64) {
	total := 0
	for _, df := range a.deframers {
		if sc, ok := df.(syncCounter); ok {
			total += sc.SyncWords()
		}
	}
	if total > a.voice.syncs {
		a.voice.syncAt, a.voice.syncFresh = nowMs, true
	}
	a.voice.syncs = total
}

// voiceStep ведёт выбор голоса на блоке фазы подбора и возвращает true, пока голос выбран.
// Голос выбирается, когда порог открыт voiceLockOpen из voiceLockWindow и за окно не было
// синхрослов; снимается после review без открытого порога.
func (a *AutoPicker) voiceStep(f sdr.IQFrame, fr frontendResult, st *Status) bool {
	id := a.voiceCandidate()
	if v := a.activeVoice(); id == "" || (v != "" && v != id) {
		a.voice.reset()
	}
	if id == "" || f.SampleRate == 0 {
		return false
	}
	nowMs := f.TimestampMs
	a.noteSyncWords(nowMs)
	if a.voice.listener == nil {
		if !a.voiceEvidence(f, fr.open, nowMs) {
			return false
		}
		a.voice.listener = newVoiceListener(id, a.frontend.midRate, a.frontend.bandwidth)
		if a.voice.listener == nil {
			return false
		}
		a.voice.lastOpen = nowMs
		slog.Info("demod auto: voice", slog.String("key", a.key), slog.String("profile", id))
	}
	if l := a.voice.listener; l.mid != a.frontend.midRate || l.bandwidth != a.frontend.bandwidth {
		a.voice.listener = newVoiceListener(id, a.frontend.midRate, a.frontend.bandwidth)
		if a.voice.listener == nil {
			return false
		}
	}
	if fr.open || nowMs < a.voice.lastOpen {
		a.voice.lastOpen = nowMs
	}
	if nowMs-a.voice.lastOpen >= uint64(a.review.Milliseconds()) { //nolint:gosec // окно пересмотра > 0
		slog.Info("demod auto: voice released", slog.String("key", a.key), slog.String("profile", id))
		a.voice.reset()
		return false
	}
	a.audio = append(a.audio[:0], a.voice.listener.push(fr.coarse, fr.open)...)
	st.PassbandHz = a.voice.listener.passbandHz
	return true
}

// voiceEvidence копит открытые блоки в окне и сообщает, что голос можно выбрать.
func (a *AutoPicker) voiceEvidence(f sdr.IQFrame, open bool, nowMs uint64) bool {
	winMs := uint64(voiceLockWindow.Milliseconds())
	if open {
		durMs := uint64(len(f.Samples)) * 1000 / uint64(f.SampleRate)
		a.voice.open = append(a.voice.open, openSpan{endMs: nowMs, durMs: durMs})
	}
	i := 0
	for i < len(a.voice.open) && (a.voice.open[i].endMs > nowMs || nowMs-a.voice.open[i].endMs > winMs) {
		i++
	}
	a.voice.open = append(a.voice.open[:0], a.voice.open[i:]...)
	if a.voice.syncFresh && nowMs >= a.voice.syncAt && nowMs-a.voice.syncAt <= winMs {
		return false
	}
	var sum uint64
	for _, s := range a.voice.open {
		sum += s.durMs
	}
	return sum >= uint64(voiceLockOpen.Milliseconds())
}
