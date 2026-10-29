package demod

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

type channelsStub struct {
	mu   sync.Mutex
	s    sdr.ChannelSettings
	gain float32
}

func (c *channelsStub) GainDB(pathID string) (float32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pathID != "radio_path_0" {
		return 0, fmt.Errorf("unknown path %q", pathID)
	}
	return c.gain, nil
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

func TestService_DemodOffIgnoresTheSignal(t *testing.T) {
	s := afskSettings(testOffset, -90)
	s.DemodOff = true
	frames := toFrames(beaconAir(t, testRate, 2, time.Second), testRate, sdr.DefaultBlockSize, 5_000)
	sink := runService(t, s, frames)

	if n := len(sink.ofType(EventTelemetry)); n != 0 {
		t.Fatalf("telemetry events=%d, want none while the demodulator is off", n)
	}
	st := sink.ofType(EventStatus)
	if len(st) == 0 {
		t.Fatal("no status")
	}
	last := st[len(st)-1]
	if last["demod_off"] != true || last["frames_good"] != float64(0) || last["squelch_open"] != false {
		t.Fatalf("status=%v, want demod off, no frames, squelch closed", last)
	}
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

// GEOSCAN pads the frame with zeros up to its fixed size: the text is still shown, hex keeps all bytes.
func TestTelemetryEvent_TextIgnoresZeroPadding(t *testing.T) {
	cases := []struct {
		info []byte
		want string
	}{
		{[]byte("beacon\x00\x00\x00"), "beacon"},
		{[]byte("bea\x00con"), ""},
		{[]byte("beacon\x01"), ""},
	}
	for _, c := range cases {
		ev := telemetryEvent("radio_path_0", "fsk9600_geoscan", Packet{Frame: ax25.Frame{Info: c.info}})
		if ev.Info != c.want {
			t.Errorf("info % X: text %q, want %q", c.info, ev.Info, c.want)
		}
		if ev.InfoHex != hex.EncodeToString(c.info) {
			t.Errorf("info % X: hex %s, want all bytes", c.info, ev.InfoHex)
		}
	}
}

func TestTelemetryEvent_GEOSCANBeaconParams(t *testing.T) {
	info, err := geoscan.BuildBeacon(map[string]float64{"eps_cell_voltage_full": 7.412})
	if err != nil {
		t.Fatal(err)
	}
	frame := ax25.Frame{Dest: ax25.Address{Call: geoscan.BeaconDest}, Src: ax25.Address{Call: "RS23S"}, Info: info}

	ev := telemetryEvent("radio_path_0", "fsk9600_geoscan", Packet{Frame: frame})
	want, _ := geoscan.BeaconParams(frame)
	if len(ev.Params) != len(want) {
		t.Fatalf("params=%d, want %d", len(ev.Params), len(want))
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Params []map[string]any `json:"params"`
	}
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	var voltage map[string]any
	for _, p := range decoded.Params {
		if p["id"] == "eps_cell_voltage_full" {
			voltage = p
		}
	}
	wantJSON := map[string]any{
		"id": "eps_cell_voltage_full", "name": "Напряжение АКБ", "value": "7,412", "unit": "В", "key": true,
	}
	for k, v := range wantJSON {
		if voltage[k] != v {
			t.Errorf("voltage param %s=%v, want %v", k, voltage[k], v)
		}
	}

	if afsk := telemetryEvent("radio_path_0", "afsk1200", Packet{Frame: frame}); afsk.Params != nil {
		t.Errorf("AFSK frame got GEOSCAN params: %v", afsk.Params)
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

// Other clients follow the channel by the status: it carries all channel settings.
func TestStatusEvent_CarriesChannelSettings(t *testing.T) {
	cs := sdr.ChannelSettings{OffsetHz: -40_000, BandwidthHz: 18_000, SquelchDBFS: -85, Demod: DemodSSTV}
	data, err := json.Marshal(statusEvent("radio_path_0", true, 436_460_000, cs, Status{Demod: DemodSSTV}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{"offset_hz": -40_000.0, "bandwidth_hz": 18_000.0, "squelch_dbfs": -85.0, "demod": "sstv"} {
		if got[k] != want {
			t.Errorf("%s=%v, want %v", k, got[k], want)
		}
	}
}

// Other clients follow the receiver gain by the status as well: the dBm units depend on it.
func TestService_StatusCarriesReceiverGain(t *testing.T) {
	sink := &sinkStub{}
	svc := NewService(&channelsStub{s: afskSettings(testOffset, -90), gain: 32.8}, sink.emit)
	ch := make(chan sdr.IQFrame, 1)
	ch <- sdr.IQFrame{Samples: make([]complex64, 4096), CenterHz: testCenter, SampleRate: testRate}
	close(ch)
	svc.Run("radio_path_0", ch)

	statuses := sink.ofType(EventStatus)
	if len(statuses) == 0 {
		t.Fatal("no status events")
	}
	for i, st := range statuses {
		if st["gain_db"] != 32.8 {
			t.Errorf("status %d: gain_db=%v, want 32.8", i, st["gain_db"])
		}
	}
}

func TestStatusEvent_ReceptionStages(t *testing.T) {
	cs := sdr.ChannelSettings{BandwidthHz: 18_000, Demod: demodGEOSCAN}
	blank := statusEvent("radio_path_0", true, 0, cs, Status{Demod: demodGEOSCAN})
	if blank.Reception != nil || blank.BandwidthHz != 18_000 {
		t.Errorf("before the signal: reception=%+v bandwidth=%d, want none and 18000", blank.Reception, blank.BandwidthHz)
	}

	st := Status{Measured: true, CarrierHz: -1500, DeviationHz: 2800, ClockJitter: 0.03,
		SyncCounted: true, SyncWords: 4, SyncBitErrors: 2}
	data, err := json.Marshal(statusEvent("radio_path_0", true, 0, cs, st))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	rx, _ := m["reception"].(map[string]any)
	want := map[string]any{"carrier_hz": -1500.0, "deviation_hz": 2800.0, "clock_jitter": 0.03,
		"sync": map[string]any{"words": 4.0, "bit_errors": 2.0}}
	for k, v := range want {
		if fmt.Sprint(rx[k]) != fmt.Sprint(v) {
			t.Errorf("reception %s=%v, want %v", k, rx[k], v)
		}
	}

	// AFSK: no deviation meter and no sync word counter.
	st.DeviationHz, st.SyncCounted = 0, false
	ev := statusEvent("radio_path_0", true, 0, cs, st)
	if ev.Reception == nil || ev.Reception.Sync != nil {
		t.Errorf("reception=%+v, want stages without sync", ev.Reception)
	}
	if data, _ := json.Marshal(ev.Reception); string(data) != `{"carrier_hz":-1500,"clock_jitter":0.03}` {
		t.Errorf("reception json=%s, want no deviation and no sync", data)
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
