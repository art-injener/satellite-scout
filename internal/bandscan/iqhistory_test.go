package bandscan

import (
	"testing"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

// countingFrames: 1 kHz rate, 100-sample frames, each sample holds its index, so time in ms equals the index.
func countingFrames(h *iqHistory, from, to int, centerHz uint64) {
	for start := from; start < to; start += 100 {
		s := make([]complex64, 100)
		for i := range s {
			s[i] = complex(float32(start+i), 0)
		}
		h.push(sdr.IQFrame{Samples: s, CenterHz: centerHz, SampleRate: 1000, TimestampMs: t0 + uint64(start)})
	}
}

func TestIQHistory_WindowByStreamTime(t *testing.T) {
	h := newIQHistory()
	countingFrames(h, 0, 3000, testCenter)
	iq, ok := h.window(t0+1500, t0+2000)
	if !ok || len(iq) != 500 {
		t.Fatalf("window ok=%v len=%d, want 500 samples", ok, len(iq))
	}
	if real(iq[0]) != 1500 || real(iq[499]) != 1999 {
		t.Errorf("window spans %v..%v, want 1500..1999", real(iq[0]), real(iq[499]))
	}
}

func TestIQHistory_KeepsOnlyLastSeconds(t *testing.T) {
	h := newIQHistory()
	// 4 с при кольце 3 с: начало истории сдвигается на t0+1000.
	countingFrames(h, 0, 4000, testCenter)
	if got := h.startMs(); got != t0+1000 {
		t.Errorf("start %d, want t0+1000", got-t0)
	}
	if _, ok := h.window(t0+500, t0+1200); ok {
		t.Error("window reaching before the history is given")
	}
}

func TestIQHistory_RetuneStartsOver(t *testing.T) {
	h := newIQHistory()
	countingFrames(h, 0, 1000, testCenter)
	countingFrames(h, 1000, 1500, testCenter+1_000_000)
	if _, ok := h.window(t0+800, t0+1200); ok {
		t.Error("window across a retune is given")
	}
	if h.centerHz != testCenter+1_000_000 {
		t.Errorf("center %d after retune", h.centerHz)
	}
	if iq, ok := h.window(t0+1100, t0+1400); !ok || real(iq[0]) != 1100 {
		t.Errorf("window after retune ok=%v", ok)
	}
}
