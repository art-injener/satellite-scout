package main

import (
	"context"
	"testing"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sweep"
)

type aimStubRX struct {
	name string
	rate uint32
}

func (r *aimStubRX) Name() string { return r.name }
func (r *aimStubRX) Capabilities() sdr.Capabilities {
	return sdr.Capabilities{
		RecommendedSampleRate: r.rate,
		SampleRateRanges:      []sdr.SampleRateRange{{MinHz: 225_001, MaxHz: 3_200_000}},
		UsableFraction:        0.8,
	}
}
func (r *aimStubRX) Tune(uint64) error     { return nil }
func (r *aimStubRX) SetGain(float32) error { return nil }
func (r *aimStubRX) Status() sdr.ReceiverStatus {
	return sdr.ReceiverStatus{}
}
func (r *aimStubRX) StartStream(context.Context, sdr.StreamConfig) (<-chan sdr.IQFrame, error) {
	ch := make(chan sdr.IQFrame)
	close(ch)
	return ch, nil
}
func (r *aimStubRX) Stop() error { return nil }

func TestMaybeAimHoldChannel_SetsOffsetOnceTowardPeak(t *testing.T) {
	const pathID = "QFH RTL-SDR"
	const lo uint64 = 434_000_000
	const peak int64 = 434_008_000

	hub := sdr.NewReceiverHub()
	hub.Register(pathID, sdr.PathConfig{SampleRateHz: 2_048_000}, func() (sdr.Receiver, error) {
		return &aimStubRX{name: pathID, rate: 2_048_000}, nil
	})
	if _, err := hub.Get(pathID); err != nil {
		t.Fatal(err)
	}

	a := &app{
		receiverHub: hub,
		holdAim:     &holdAimByPath{byID: make(map[string]holdAimEntry)},
	}
	w := sweep.Window{CenterHz: lo, SpanHz: 1_600_000}
	const start int64 = 1_000_000

	a.maybeAimHoldChannel(pathID, w, start, start, nil)
	ch, _ := hub.Channel(pathID)
	if ch.OffsetHz != 0 {
		t.Fatalf("without sights offset=%d, want 0", ch.OffsetHz)
	}

	live := []freqplan.Sighting{{CenterHz: peak, SNRDB: 14, LastSeenMs: start}}
	a.maybeAimHoldChannel(pathID, w, start, start, live)
	ch, _ = hub.Channel(pathID)
	if ch.OffsetHz != 8_000 {
		t.Fatalf("after aim offset=%d, want 8000", ch.OffsetHz)
	}

	a.maybeAimHoldChannel(pathID, w, start, start+1_000, live)
	ch, _ = hub.Channel(pathID)
	if ch.OffsetHz != 8_000 {
		t.Fatalf("second tick offset=%d, want stay 8000", ch.OffsetHz)
	}

	live2 := []freqplan.Sighting{{CenterHz: 434_012_000, SNRDB: 15, LastSeenMs: start + freqplan.ProbeMs}}
	a.maybeAimHoldChannel(pathID, w, start, start+freqplan.ProbeMs, live2)
	ch, _ = hub.Channel(pathID)
	if ch.OffsetHz != 12_000 {
		t.Fatalf("live correction offset=%d, want 12000", ch.OffsetHz)
	}
}

func TestMaybeAimHoldChannel_AirClearsStaleDopplerNorad(t *testing.T) {
	const pathID = "air1"
	const lo uint64 = 437_760_000
	const start int64 = 1_000_000

	hub := sdr.NewReceiverHub()
	hub.Register(pathID, sdr.PathConfig{SampleRateHz: 2_048_000}, func() (sdr.Receiver, error) {
		return &aimStubRX{name: pathID, rate: 2_048_000}, nil
	})
	_, _ = hub.Get(pathID)
	cs := sdr.DefaultChannelSettings()
	cs.DopplerNorad = 67287
	cs.DopplerAuto = true
	cs.DemodOff = false
	if err := hub.SetChannel(pathID, cs); err != nil {
		t.Fatal(err)
	}

	a := &app{
		receiverHub: hub,
		holdAim:     &holdAimByPath{byID: make(map[string]holdAimEntry)},
	}
	a.maybeAimHoldChannel(pathID, sweep.Window{CenterHz: lo, SpanHz: 1_600_000, Air: true}, start, start, nil)
	got, _ := hub.Channel(pathID)
	if got.DopplerNorad != 0 {
		t.Fatalf("doppler_norad=%d, want 0 on air hop", got.DopplerNorad)
	}
}

func TestMaybeAimHoldChannel_OperatorLockBlocksAim(t *testing.T) {
	const pathID = "p1"
	const lo uint64 = 434_000_000
	const start int64 = 1_000_000

	hub := sdr.NewReceiverHub()
	hub.Register(pathID, sdr.PathConfig{SampleRateHz: 2_048_000}, func() (sdr.Receiver, error) {
		return &aimStubRX{name: pathID, rate: 2_048_000}, nil
	})
	_, _ = hub.Get(pathID)

	a := &app{
		receiverHub: hub,
		holdAim:     &holdAimByPath{byID: make(map[string]holdAimEntry)},
	}
	w := sweep.Window{CenterHz: lo, SpanHz: 1_600_000}
	// Сначала открываем стоянку (иначе lock до Reset сбросится).
	a.maybeAimHoldChannel(pathID, w, start, start, nil)
	a.lockHoldAim(pathID)
	live := []freqplan.Sighting{{CenterHz: 434_010_000, SNRDB: 16, LastSeenMs: start}}
	a.maybeAimHoldChannel(pathID, w, start, start, live)
	ch, _ := hub.Channel(pathID)
	if ch.OffsetHz != 0 {
		t.Fatalf("locked offset=%d, want 0", ch.OffsetHz)
	}
}
