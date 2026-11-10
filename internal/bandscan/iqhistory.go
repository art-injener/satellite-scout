package bandscan

import (
	"math"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

// historySeconds — сколько секунд потока хранится для разбора участков.
// Запас над sliceMs (2 с): иначе из‑за Ceil startMs окно «ровно 2 с» не влезает.
// При 2,56 МС/с это ~60 МБ на тракт (complex64).
const historySeconds = 3

// iqHistory — кольцо последних отсчётов тракта со временем потока. Перестройка начинает
// его заново: отрезок через перестройку смешал бы две полосы. Вызывать из одной горутины.
type iqHistory struct {
	buf      []complex64
	pos      int // куда пишется следующий отсчёт
	filled   int
	rate     uint32
	centerHz uint64
	endMs    float64 // время потока сразу после последнего отсчёта
}

func newIQHistory() *iqHistory {
	return &iqHistory{}
}

func (h *iqHistory) push(f sdr.IQFrame) {
	if f.SampleRate == 0 {
		return
	}
	if f.SampleRate != h.rate || f.CenterHz != h.centerHz {
		if f.SampleRate != h.rate {
			h.buf = make([]complex64, historySeconds*int(f.SampleRate))
		}
		h.rate, h.centerHz = f.SampleRate, f.CenterHz
		h.pos, h.filled = 0, 0
	}
	n := len(h.buf)
	src := f.Samples
	if len(src) > n {
		src = src[len(src)-n:]
	}
	for len(src) > 0 {
		c := copy(h.buf[h.pos:], src)
		src = src[c:]
		h.pos = (h.pos + c) % n
	}
	h.filled = min(n, h.filled+len(f.Samples))
	h.endMs = float64(f.TimestampMs) + float64(len(f.Samples))*1000/float64(f.SampleRate)
}

// startMs — время потока самого старого отсчёта в кольце.
func (h *iqHistory) startMs() uint64 {
	if h.rate == 0 {
		return 0
	}
	return uint64(math.Ceil(h.endMs - float64(h.filled)*1000/float64(h.rate)))
}

// window — копия отсчётов [fromMs, toMs); false — отрезка уже или ещё нет в кольце.
func (h *iqHistory) window(fromMs, toMs uint64) ([]complex64, bool) {
	if h.rate == 0 || toMs <= fromMs || float64(toMs) > h.endMs+1 {
		return nil, false
	}
	perMs := float64(h.rate) / 1000
	back := int(math.Round((h.endMs - float64(fromMs)) * perMs))
	count := min(int(math.Round(float64(toMs-fromMs)*perMs)), back)
	if back > h.filled || count <= 0 {
		return nil, false
	}
	n := len(h.buf)
	start := ((h.pos-back)%n + n) % n
	out := make([]complex64, count)
	c := copy(out, h.buf[start:])
	copy(out[c:], h.buf)
	return out, true
}
