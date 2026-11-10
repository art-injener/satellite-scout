package demod

import (
	"math"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// dopplerByNorad gives each satellite its own range rate; unknown satellites have no orbit.
type dopplerByNorad map[int]float64

func (d dopplerByNorad) RangeRateKmps(norad int, _ time.Time) (float64, bool) {
	rr, ok := d[norad]
	return rr, ok
}

type transmittersStub []ExpectedTransmitter

func (t transmittersStub) ExpectedTransmitters() []ExpectedTransmitter { return t }

func TestMarkersInBand_KeepsTransmittersInsideTheReceiverWindow(t *testing.T) {
	// Window 436.5 MHz ± 512 kHz.
	txs := []ExpectedTransmitter{
		{NoradID: 1, UUID: "in", FreqHz: 436_600_000},
		{NoradID: 2, UUID: "vhf", FreqHz: 145_800_000},
		{NoradID: 3, UUID: "no-orbit", FreqHz: 436_400_000},
		// 7.3 kHz of approach Doppler pushes it past the upper edge.
		{NoradID: 4, UUID: "edge", FreqHz: 437_011_000},
	}
	doppler := dopplerByNorad{1: 3, 2: 0, 4: -5}

	got := MarkersInBand(txs, doppler, time.Now(), testCenter, testRate)
	if len(got) != 1 {
		t.Fatalf("markers = %+v, want only the in-band transmitter", got)
	}
	m := got[0]
	wantShift := tracker.DopplerShiftHz(436_600_000, 3)
	if m.NoradID != 1 || m.UUID != "in" {
		t.Errorf("marker = %+v, want norad 1 uuid in", m)
	}
	if math.Abs(m.DopplerHz-wantShift) > 0.5 {
		t.Errorf("doppler_hz = %.1f, want %.1f (same formula as the channel)", m.DopplerHz, wantShift)
	}
	if want := int64(math.Round(436_600_000 + wantShift)); m.FreqHz != want {
		t.Errorf("freq_hz = %d, want %d", m.FreqHz, want)
	}
}

func TestMarkersInBand_NothingWithoutOrbitsOrTransmitters(t *testing.T) {
	txs := []ExpectedTransmitter{{NoradID: 1, UUID: "in", FreqHz: 436_600_000}}
	if got := MarkersInBand(txs, nil, time.Now(), testCenter, testRate); got != nil {
		t.Errorf("without orbits: %+v, want none", got)
	}
	if got := MarkersInBand(nil, dopplerByNorad{1: 0}, time.Now(), testCenter, testRate); got != nil {
		t.Errorf("without transmitters: %+v, want none", got)
	}
}

func TestService_StatusCarriesTxMarkers(t *testing.T) {
	sink := &sinkStub{}
	svc := NewService(&channelsStub{s: voiceSettings("fm", 15_000, -90)}, sink.emit).
		WithDoppler(dopplerByNorad{25544: -5}).
		WithTransmitters(transmittersStub{{NoradID: 25544, UUID: "tx-iss", FreqHz: 436_600_000}})
	frames := toFrames(fmToneAir(0.3), testRate, sdr.DefaultBlockSize, 0)
	ch := make(chan sdr.IQFrame, len(frames))
	for _, f := range frames {
		ch <- f
	}
	close(ch)
	svc.Run("radio_path_0", ch)

	st := sink.ofType(EventStatus)
	first, last := st[0], st[len(st)-1]
	markers, ok := first["tx_markers"].([]any)
	if !ok || len(markers) != 1 {
		t.Fatalf("tx_markers = %v, want one marker", first["tx_markers"])
	}
	m := markers[0].(map[string]any)
	if m["norad_id"] != float64(25544) || m["uuid"] != "tx-iss" {
		t.Errorf("marker = %v", m)
	}
	want := 436_600_000 + tracker.DopplerShiftHz(436_600_000, -5)
	if f, _ := m["freq_hz"].(float64); math.Abs(f-want) > 1 {
		t.Errorf("freq_hz = %v, want %.0f", m["freq_hz"], want)
	}
	if _, has := last["tx_markers"]; has {
		t.Errorf("stream closed: tx_markers = %v, want none", last["tx_markers"])
	}
}
