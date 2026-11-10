package bandscan

import (
	"encoding/json"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/signalclass"
)

type fixedBand uint32

func (b fixedBand) UsableBand(string) (uint32, error) { return uint32(b), nil }

type expectedAt []Expected

func (e expectedAt) ExpectedInBand(uint64, uint32) []Expected { return e }

type sentEvent struct {
	typ string
	ev  BandEvent
}

// runStream plays seconds of the generator into Service.Run; the wall clock follows stream time.
func runStream(t *testing.T, svc *Service, g *iqGen, seconds float64) []sentEvent {
	t.Helper()
	var events []sentEvent
	var nowMs atomic.Int64
	svc.emit = func(typ string, data []byte) {
		var ev BandEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("bad %s payload: %v", typ, err)
		}
		events = append(events, sentEvent{typ, ev})
	}
	svc.now = func() time.Time { return time.UnixMilli(nowMs.Load()) }

	frames := make(chan sdr.IQFrame)
	done := make(chan struct{})
	go func() {
		svc.Run("path1", frames)
		close(done)
	}()
	blocks := int(seconds * testRate / sdr.DefaultBlockSize)
	for range blocks {
		f := g.block(true)
		nowMs.Store(int64(f.TimestampMs))
		frames <- f
	}
	close(frames)
	<-done
	return events
}

func TestService_SignalsInBand_FiltersByWindow(t *testing.T) {
	svc := NewService(fixedBand(testUsableHz), nil)
	pt := svc.tracker("path1")
	inBand := testCenter + 100_000
	outBand := testCenter + 5_000_000
	feed(pt.tr, t0, t0+3_000, testCenter, func(uint64) []Peak {
		return []Peak{carrier(float64(inBand), 2_000, 15), carrier(float64(outBand), 2_000, 15)}
	})

	got := svc.SignalsInBand("path1", testCenter, uint32(testUsableHz))
	if len(got) != 1 {
		t.Fatalf("got %d signals %+v, want the in-band carrier only", len(got), got)
	}
	if d := got[0].CenterHz - int64(inBand); d > int64(testBinHz) || d < -int64(testBinHz) {
		t.Errorf("center %d, want ~%d", got[0].CenterHz, inBand)
	}
	if svc.SignalsInBand("path1", testCenter, 0) != nil {
		t.Fatal("zero span must return nil")
	}
}

func TestService_SignalsInBand_IgnoresSignalQuietLongerThanTheDwell(t *testing.T) {
	svc := NewService(fixedBand(testUsableHz), nil)
	pt := svc.tracker("path1")
	sig := float64(testCenter + 100_000)
	feed(pt.tr, t0, t0+3_000, testCenter, func(uint64) []Peak {
		return []Peak{carrier(sig, 2_000, 15)}
	})
	feed(pt.tr, t0+3_000, t0+3_000+HoldMs+stepMs, testCenter, func(uint64) []Peak { return nil })
	if len(pt.tr.Signals(nil)) != 1 {
		t.Fatal("the hour list dropped a signal that only just went quiet")
	}
	if got := svc.SignalsInBand("path1", testCenter, uint32(testUsableHz)); len(got) != 0 {
		t.Fatalf("visit snapshot %+v, want empty: the signal was quiet longer than a dwell cares about", got)
	}
}

func TestService_BroadcastsBandSignalsAndClearsOnStreamEnd(t *testing.T) {
	svc := NewService(fixedBand(testUsableHz), nil)
	g := newIQGen(tone{offsetHz: 100_000, amp: 0.003})
	events := runStream(t, svc, g, 2.5)

	if len(events) < 3 {
		t.Fatalf("got %d events, want about one per second and a final one", len(events))
	}
	var found bool
	for _, e := range events[:len(events)-1] {
		if e.typ != EventBandSignals || e.ev.RadioPath != "path1" {
			t.Fatalf("event %q for %q, want band_signals for path1", e.typ, e.ev.RadioPath)
		}
		for _, s := range e.ev.Signals {
			if s.CenterHz > testCenter+99_000 && s.CenterHz < testCenter+101_000 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("carrier at +100 kHz never reported: %+v", events)
	}
	if last := events[len(events)-1].ev; last.Signals == nil || len(last.Signals) != 0 {
		t.Errorf("final event signals = %v, want an empty list when the stream stops", last.Signals)
	}
}

func TestService_ClassifiesSegmentFromStream(t *testing.T) {
	svc := NewService(fixedBand(testUsableHz), nil)
	g := newIQGen(tone{offsetHz: 100_000, amp: 0.003})
	// Continuous slice is 2 s; frames may finish before Extract, Run waits for classify.
	events := runStream(t, svc, g, 3.5)
	for _, e := range events {
		for _, s := range e.ev.Signals {
			if s.Structure != nil {
				if s.Structure.Modulation != signalclass.ModCarrier {
					t.Errorf("structure %+v, want a carrier", s.Structure)
				}
				return
			}
		}
	}
	t.Errorf("no structure in %d events", len(events))
}

// Segments survive the stream: a new stream on the same path continues their statistics.
func TestService_KeepsSegmentsAcrossStreams(t *testing.T) {
	svc := NewService(fixedBand(testUsableHz), nil).WithExpected(expectedAt(nil))
	g := newIQGen(tone{offsetHz: 100_000, amp: 0.003})
	first := runStream(t, svc, g, 2.5)
	second := runStream(t, svc, g, 2.5)

	idOf := func(events []sentEvent) (string, float64) {
		for _, e := range slices.Backward(events) {
			if sigs := e.ev.Signals; len(sigs) == 1 {
				return sigs[0].ID, sigs[0].Stats.ObservedS
			}
		}
		t.Fatalf("no single-signal event in %+v", events)
		return "", 0
	}
	id1, obs1 := idOf(first)
	id2, obs2 := idOf(second)
	if id1 != id2 {
		t.Errorf("id %s in the second stream, want %s", id2, id1)
	}
	if obs2 <= obs1 {
		t.Errorf("observed %.1f s after the second stream, want more than %.1f s", obs2, obs1)
	}
}
