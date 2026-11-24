package demod

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax100"
	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/payload"
	"github.com/art-injener/satellite-scout/internal/products"
	"github.com/art-injener/satellite-scout/internal/rfprofile"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/usp"
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

// Печатный info в UI-кадре AFSK: нули в конце срезаются, hex хранит поле info целиком.
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
		frame := ax25.Frame{Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "R1ABC"}, Info: c.info}
		ev := telemetryEvent("radio_path_0", "afsk1200", Packet{Frame: frame})
		if c.want != "" {
			if ev.Kind != "text" || ev.Info != c.want {
				t.Errorf("info % X: kind=%q text %q, want text/%q", c.info, ev.Kind, ev.Info, c.want)
			}
		} else if ev.Kind == "text" {
			t.Errorf("info % X: got text %q, want not text", c.info, ev.Info)
		}
		if ev.InfoHex != hex.EncodeToString(c.info) && ev.Kind == "text" {
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
	if ev.Kind != "beacon" || ev.Decoder != "geoscan.beacon" {
		t.Fatalf("kind/decoder = %q/%q", ev.Kind, ev.Decoder)
	}
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

func TestTelemetryEvent_USPBeaconParams(t *testing.T) {
	raw, err := usp.SampleGeneralAX25()
	if err != nil {
		t.Fatal(err)
	}
	ev := telemetryEvent("radio_path_0", "gmsk4800_usp", Packet{Raw: raw})
	if ev.Kind != "beacon" || ev.Decoder != "usp.beacon" {
		t.Fatalf("kind/decoder=%q/%q summary=%q", ev.Kind, ev.Decoder, ev.Summary)
	}
	if len(ev.Params) < 5 {
		t.Fatalf("params=%d", len(ev.Params))
	}
	found := false
	for _, p := range ev.Params {
		if p.ID == "uab" && p.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("params=%v, want uab", ev.Params)
	}
}

func TestTelemetryEvent_SuchaiBeaconParams(t *testing.T) {
	raw := ax100.PackSuchaiBeacon(ax100.SuchaiPort2, map[string]uint32{
		"eps_vbatt": 7500, "obc_hrs_alive": 10,
	})
	ev := telemetryEvent("radio_path_0", "gmsk4800_ax100", Packet{Raw: raw})
	if ev.Kind != "beacon" || ev.Decoder != "suchai.beacon" {
		t.Fatalf("kind/decoder=%q/%q", ev.Kind, ev.Decoder)
	}
	if !strings.Contains(ev.Summary, "SUCHAI-2") {
		t.Fatalf("summary=%q", ev.Summary)
	}
	found := false
	for _, p := range ev.Params {
		if p.ID == "eps_vbatt" && p.Value == "7,50" {
			found = true
		}
	}
	if !found {
		t.Fatalf("params=%v", ev.Params)
	}
}

func TestTelemetryEvent_GEOSCANImageKind(t *testing.T) {
	raw := make([]byte, 8+56)
	raw[0], raw[2], raw[4], raw[7] = 0x01, 0x3E, 0x09, 0x0B
	raw[1] = 0x00
	raw[5], raw[6] = 0x00, 0x80 // смещение 0x8000
	ev := telemetryEvent("radio_path_0", "fsk9600_geoscan", Packet{Raw: raw, FrequencyHz: 436_200_000})
	if ev.Kind != "image" || ev.Decoder != "geoscan.image" {
		t.Fatalf("kind/decoder = %q/%q", ev.Kind, ev.Decoder)
	}
	if ev.Summary == "" {
		t.Fatal("empty summary")
	}
}

func TestService_PublishGeoscanProduct(t *testing.T) {
	store, err := products.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &sinkStub{}
	svc := NewService(&channelsStub{s: fsk9600Settings(testOffset, -90)}, sink.emit).WithProducts(store)
	raw := make([]byte, 8+56)
	raw[0], raw[2], raw[4], raw[7] = 0x01, 0x3E, 0x09, 0x0B
	raw[1] = 0x00
	raw[5], raw[6] = 0x00, 0x80
	raw[8], raw[9], raw[10], raw[11] = 0xFF, 0xD8, 0xFF, 0xD9
	cs := fsk9600Settings(testOffset, -90)
	cs.Demod = "fsk9600_geoscan"
	svc.emitTelemetry("radio_path_0", cs, Packet{Raw: raw, FrequencyHz: 436_200_000, TimestampMs: 1})
	prods := sink.ofType(EventProduct)
	if len(prods) != 1 {
		t.Fatalf("product events=%d, want 1", len(prods))
	}
	if prods[0]["kind"] != "geoscan" || prods[0]["done"] != true {
		t.Fatalf("product=%v", prods[0])
	}
	tm := sink.ofType(EventTelemetry)
	if len(tm) != 1 || tm[0]["product_id"] == nil || tm[0]["product_id"] == "" {
		t.Fatalf("telemetry=%v, want product_id", tm)
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
		SyncCounted: true, SyncWords: 4, SyncBitErrors: 2,
		FECCounted: true, FECCorrected: 3}
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
		"sync": map[string]any{"words": 4.0, "bit_errors": 2.0},
		"fec":  map[string]any{"corrected": 3.0}}
	for k, v := range want {
		if fmt.Sprint(rx[k]) != fmt.Sprint(v) {
			t.Errorf("reception %s=%v, want %v", k, rx[k], v)
		}
	}

	// AFSK: нет девиации, синхрослова и FEC.
	st.DeviationHz, st.SyncCounted, st.FECCounted = 0, false, false
	ev := statusEvent("radio_path_0", true, 0, cs, st)
	if ev.Reception == nil || ev.Reception.Sync != nil || ev.Reception.FEC != nil {
		t.Errorf("reception=%+v, want stages without sync/FEC", ev.Reception)
	}
	if data, _ := json.Marshal(ev.Reception); string(data) != `{"carrier_hz":-1500,"clock_jitter":0.03}` {
		t.Errorf("reception json=%s, want no deviation/sync/FEC", data)
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

func TestService_TelemetryBindsFromPathSat(t *testing.T) {
	frames := toFrames(beaconAir(t, testRate, 1, time.Second), testRate, sdr.DefaultBlockSize, 0)
	sink := &sinkStub{}
	svc := NewService(&channelsStub{s: afskSettings(testOffset, -90)}, sink.emit).
		WithPathSats(PathSatMap{
			"radio_path_0": {NoradID: 53385, SatName: "GEOSCAN-EDELVEIS", Source: SatSourceSigMF},
		})
	ch := make(chan sdr.IQFrame, len(frames))
	for _, f := range frames {
		ch <- f
	}
	close(ch)
	svc.Run("radio_path_0", ch)

	evs := sink.ofType(EventTelemetry)
	if len(evs) == 0 {
		t.Fatal("no telemetry")
	}
	ev := evs[0]
	if ev["norad_id"] != float64(53385) {
		t.Errorf("norad_id=%v", ev["norad_id"])
	}
	if ev["sat_name"] != "GEOSCAN-EDELVEIS" || ev["sat_source"] != SatSourceSigMF {
		t.Errorf("name/source=%v/%v", ev["sat_name"], ev["sat_source"])
	}
}

func TestService_TelemetryBindsCallsign(t *testing.T) {
	info, err := geoscan.BuildBeacon(map[string]float64{"eps_cell_voltage_full": 7.412})
	if err != nil {
		t.Fatal(err)
	}
	frame := ax25.Frame{
		Dest: ax25.Address{Call: geoscan.BeaconDest},
		Src:  ax25.Address{Call: "RS23S"},
		Info: info,
	}
	pkt := Packet{Frame: frame, FrequencyHz: 436_200_000, TimestampMs: 1}
	svc := NewService(&channelsStub{}, nil).WithCallsigns(callsignsMap{
		"RS23S": {norad: 53385, name: "GEOSCAN-EDELVEIS"},
	})
	ev := svc.telemetryEvent("radio_path_0", sdr.ChannelSettings{
		BandwidthHz: 20_000, Demod: "fsk9600_geoscan",
	}, pkt)
	if ev.NoradID != 53385 || ev.SatSource != SatSourceCallsign || ev.SatName != "GEOSCAN-EDELVEIS" {
		t.Fatalf("got %+v", ev)
	}
}

type rfRecorderStub struct {
	norad int
	freq  int64
	demod string
	ts    int64
	n     int
}

func (r *rfRecorderStub) RecordFrame(norad int, freqHz int64, demodID string, tsMs int64) {
	r.n++
	r.norad, r.freq, r.demod, r.ts = norad, freqHz, demodID, tsMs
}

type airHearStub struct {
	n       int
	path    string
	freq    uint64
	kind    string
	norad   int
	frame   bool
	infoHex string
	params  int
}

func (a *airHearStub) RecordRx(pathID string, freqHz uint64, note AirNote) {
	a.n++
	a.path, a.freq, a.kind, a.norad, a.frame = pathID, freqHz, note.Kind, note.NoradID, note.CountFrame
	a.infoHex = note.InfoHex
	a.params = len(note.Params)
}

type rfProfileStub map[int]rfprofile.Profile

func (m rfProfileStub) RFProfile(norad int) (rfprofile.Profile, bool) {
	p, ok := m[norad]
	return p, ok
}

func TestAutoHints_FromRFProfile(t *testing.T) {
	svc := NewService(&channelsStub{}, nil).WithRFProfiles(rfProfileStub{
		53385: {NoradID: 53385, Tx: []rfprofile.Transmitter{{
			Key: "u1", UUID: "u1", FreqHz: 436_200_000,
			Accepted: AcceptedStub(),
			Detected: rfprofile.Detected{
				Modulation: "2FSK", Baud: 9600, Demod: "fsk9600_g3ruh",
				DemodAlt: []string{"fsk9600_geoscan"}, Confirmations: 5,
			},
		}}},
	})
	// Accepted wins.
	h := svc.autoHints("radio_path_1", sdr.ChannelSettings{DopplerNorad: 53385}, 436_200_000)
	if h.Accepted != "fsk9600_geoscan" || h.Blind != "" {
		t.Fatalf("accepted hints %+v", h)
	}
	svc = NewService(&channelsStub{}, nil).WithRFProfiles(rfProfileStub{
		53385: {NoradID: 53385, Tx: []rfprofile.Transmitter{{
			Key: "u1", FreqHz: 436_200_000,
			Detected: rfprofile.Detected{
				Modulation: "2FSK", Baud: 9600, Demod: "fsk9600_g3ruh",
				DemodAlt: []string{"fsk9600_geoscan"}, Confirmations: 3,
			},
		}}},
	})
	h = svc.autoHints("radio_path_1", sdr.ChannelSettings{DopplerNorad: 53385}, 436_200_000)
	if h.Blind != "fsk9600_g3ruh" || !slices.Equal(h.BlindAlt, []string{"fsk9600_geoscan"}) {
		t.Fatalf("detected hints %+v", h)
	}
}

func AcceptedStub() rfprofile.Accepted {
	return rfprofile.Accepted{DemodID: "fsk9600_geoscan", FramesTotal: 2}
}

func TestEmitTelemetry_RecordsRFProfile(t *testing.T) {
	rec := &rfRecorderStub{}
	sink := &sinkStub{}
	svc := NewService(&channelsStub{}, sink.emit).
		WithCallsigns(callsignsMap{"RS23S": {norad: 53385, name: "GEOSCAN-EDELVEIS"}}).
		WithRFFrames(rec)
	info, _ := hex.DecodeString("010203")
	pkt := Packet{
		Frame: ax25.Frame{
			Dest: ax25.Address{Call: geoscan.BeaconDest},
			Src:  ax25.Address{Call: "RS23S"},
			Info: info,
		},
		FrequencyHz: 436_200_000,
		TimestampMs: 42,
	}
	svc.emitTelemetry("radio_path_0", sdr.ChannelSettings{Demod: "fsk9600_geoscan"}, pkt)
	if rec.n != 1 || rec.norad != 53385 || rec.freq != 436_200_000 || rec.demod != "fsk9600_geoscan" || rec.ts != 42 {
		t.Fatalf("recorder %+v", rec)
	}
	if n := len(sink.ofType(EventTelemetry)); n != 1 {
		t.Fatalf("telemetry events=%d", n)
	}
}

func TestEmitTelemetry_NotesAirHear(t *testing.T) {
	air := &airHearStub{}
	sink := &sinkStub{}
	svc := NewService(&channelsStub{}, sink.emit).
		WithCallsigns(callsignsMap{"RS23S": {norad: 53385, name: "GEOSCAN-EDELVEIS"}}).
		WithAirHear(air)
	info, _ := hex.DecodeString("010203")
	pkt := Packet{
		Frame: ax25.Frame{
			Dest: ax25.Address{Call: geoscan.BeaconDest},
			Src:  ax25.Address{Call: "RS23S"},
			Info: info,
		},
		FrequencyHz: 436_200_000,
		TimestampMs: 42,
	}
	svc.emitTelemetry("radio_path_0", sdr.ChannelSettings{Demod: "fsk9600_geoscan"}, pkt)
	if air.n != 1 || air.path != "radio_path_0" || air.freq != 436_200_000 || !air.frame {
		t.Fatalf("air hear %+v", air)
	}
	if air.norad != 53385 || air.kind == "" {
		t.Fatalf("air kind/norad %+v", air)
	}
	if air.infoHex == "" {
		t.Fatalf("air payload hex missing: %+v", air)
	}
}

func TestNoteAirVoice_OpenSquelch(t *testing.T) {
	air := &airHearStub{}
	svc := NewService(&channelsStub{}, (&sinkStub{}).emit).WithAirHear(air)
	svc.noteAirVoice("radio_path_0", StatusEvent{
		Active: true, SquelchOpen: true, Demod: "fm",
		FrequencyHz: 145_800_000, DopplerNorad: 25544, LastPacketMs: 99,
	})
	if air.n != 1 || air.kind != payload.KindVoice || air.frame || air.norad != 25544 {
		t.Fatalf("voice air %+v", air)
	}
	air = &airHearStub{}
	svc = NewService(&channelsStub{}, (&sinkStub{}).emit).WithAirHear(air)
	svc.noteAirVoice("radio_path_0", StatusEvent{
		Active: true, SquelchOpen: true, Demod: "fsk9600", FrequencyHz: 436_200_000,
	})
	if air.n != 0 {
		t.Fatalf("digital demod must not write voice, got %+v", air)
	}
}
