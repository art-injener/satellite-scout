package demod

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

type channelsStub struct {
	mu sync.Mutex
	s  sdr.ChannelSettings
}

func (c *channelsStub) Channel(pathID string) (sdr.ChannelSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pathID != "radio_path_0" {
		return sdr.ChannelSettings{}, fmt.Errorf("unknown path %q", pathID)
	}
	return c.s, nil
}

type event struct {
	Type string
	Data map[string]any
}

type sinkStub struct {
	mu     sync.Mutex
	events []event
}

func (s *sinkStub) emit(eventType string, data []byte) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		panic(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event{eventType, m})
}

func (s *sinkStub) ofType(eventType string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, e := range s.events {
		if e.Type == eventType {
			out = append(out, e.Data)
		}
	}
	return out
}

// runService feeds frames through Service.Run as the hub tap would and waits for it to finish.
func runService(t *testing.T, s sdr.ChannelSettings, frames []sdr.IQFrame) *sinkStub {
	t.Helper()
	sink := &sinkStub{}
	svc := NewService(&channelsStub{s: s}, sink.emit)
	ch := make(chan sdr.IQFrame)
	done := make(chan struct{})
	go func() {
		svc.Run("radio_path_0", ch)
		close(done)
	}()
	for _, f := range frames {
		ch <- f
	}
	close(ch)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the stream closed")
	}
	return sink
}

func TestService_EmitsTelemetryPerFrame(t *testing.T) {
	frames := toFrames(beaconAir(t, testRate, 2, time.Second), testRate, sdr.DefaultBlockSize, 5_000)
	sink := runService(t, afskSettings(testOffset, -90), frames)

	tm := sink.ofType(EventTelemetry)
	if len(tm) != 2 {
		t.Fatalf("telemetry events=%d, want 2", len(tm))
	}
	want := map[string]any{
		"radio_path":   "radio_path_0",
		"frequency_hz": float64(testCenter + testOffset),
		"demod":        "afsk1200",
		"src":          "RS0SIM",
		"dest":         "CQ",
		"info":         "chain test beacon",
		"info_hex":     "636861696e207465737420626561636f6e",
	}
	for k, v := range want {
		if tm[0][k] != v {
			t.Errorf("telemetry %s=%v, want %v", k, tm[0][k], v)
		}
	}
	if ts, _ := tm[1]["timestamp_ms"].(float64); ts < 6_000 || ts > 7_000 {
		t.Errorf("second packet timestamp_ms=%v, want within the second burst", tm[1]["timestamp_ms"])
	}
}

func TestService_StatusWhileStreamingAndOnStop(t *testing.T) {
	frames := toFrames(beaconAir(t, testRate, 2, time.Second), testRate, sdr.DefaultBlockSize, 0)
	sink := runService(t, afskSettings(testOffset, -90), frames)

	st := sink.ofType(EventStatus)
	if len(st) < 2 {
		t.Fatalf("status events=%d, want one on the first block and one on stop", len(st))
	}
	first, last := st[0], st[len(st)-1]
	if first["active"] != true || first["supported"] != true || first["squelch_dbfs"] != -90.0 {
		t.Errorf("first status=%v, want active, supported, squelch -90", first)
	}
	if last["active"] != false {
		t.Errorf("last status active=%v, want false after the stream closed", last["active"])
	}
	if last["frames_good"] != 2.0 || last["frames_bad"] != 0.0 {
		t.Errorf("last status frames good=%v bad=%v, want 2 and 0", last["frames_good"], last["frames_bad"])
	}
	if last["frequency_hz"] != float64(testCenter+testOffset) {
		t.Errorf("last status frequency_hz=%v, want %d", last["frequency_hz"], testCenter+testOffset)
	}
	if lvl, _ := last["level_dbfs"].(float64); lvl > -55 || lvl < -65 {
		t.Errorf("last status level_dbfs=%v, want about -60 on noise", last["level_dbfs"])
	}
}

func TestService_UsesCurrentChannelSettings(t *testing.T) {
	frames := toFrames(beaconAir(t, testRate, 1, time.Second), testRate, sdr.DefaultBlockSize, 0)
	sink := runService(t, afskSettings(testOffset, testLevel+20), frames)
	if n := len(sink.ofType(EventTelemetry)); n != 0 {
		t.Errorf("telemetry events=%d, want 0 with the squelch above the signal", n)
	}
}

func TestService_StatusInterval(t *testing.T) {
	sink := &sinkStub{}
	svc := NewService(&channelsStub{s: afskSettings(testOffset, -90)}, sink.emit)
	// The clock advances 400 ms per block: Run reads it once per block.
	calls := 0
	svc.now = func() time.Time {
		calls++
		return time.Unix(0, 0).Add(time.Duration(calls-1) * 400 * time.Millisecond)
	}

	ch := make(chan sdr.IQFrame, 8)
	frame := sdr.IQFrame{Samples: make([]complex64, 1024), CenterHz: testCenter, SampleRate: testRate}
	for range 4 {
		ch <- frame
	}
	close(ch)
	svc.Run("radio_path_0", ch)

	// Blocks at 0, 0.4, 0.8, 1.2 s: status at 0 and 1.2 s, then one on stop.
	if n := len(sink.ofType(EventStatus)); n != 3 {
		t.Errorf("status events=%d, want 3", n)
	}
}
