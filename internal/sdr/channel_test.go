package sdr

import (
	"errors"
	"math"
	"slices"
	"testing"
)

func TestDefaultChannelSettings_ValidAndAFSK(t *testing.T) {
	c := DefaultChannelSettings()
	if err := c.Validate(2_048_000); err != nil {
		t.Fatalf("default settings invalid: %v", err)
	}
	if c.Demod != "afsk1200" {
		t.Errorf("demod=%q, want afsk1200", c.Demod)
	}
	if !c.DemodOff {
		t.Error("demodulator must start off")
	}
	if !slices.Contains(Demods, c.Demod) {
		t.Errorf("default demod %q not in Demods", c.Demod)
	}
}

func TestChannelSettings_Validate(t *testing.T) {
	const rate = 2_048_000
	ok := ChannelSettings{OffsetHz: 0, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: "afsk1200"}

	tests := []struct {
		name    string
		mutate  func(c *ChannelSettings)
		wantErr bool
	}{
		{"valid", func(*ChannelSettings) {}, false},
		{"zero bandwidth", func(c *ChannelSettings) { c.BandwidthHz = 0 }, true},
		{"bandwidth wider than sample rate", func(c *ChannelSettings) { c.BandwidthHz = rate + 1 }, true},
		{"widest channel", func(c *ChannelSettings) { c.BandwidthHz = MaxChannelBandwidthHz }, false},
		{"wider than the demodulator passes", func(c *ChannelSettings) { c.BandwidthHz = MaxChannelBandwidthHz + 1 }, true},
		{"channel at upper band edge", func(c *ChannelSettings) { c.OffsetHz = rate/2 - 7_500 }, false},
		{"channel past upper band edge", func(c *ChannelSettings) { c.OffsetHz = rate/2 - 7_499 }, true},
		{"channel past lower band edge", func(c *ChannelSettings) { c.OffsetHz = -(rate/2 - 7_499) }, true},
		{"squelch zero", func(c *ChannelSettings) { c.SquelchDBFS = 0 }, false},
		{"squelch above full scale", func(c *ChannelSettings) { c.SquelchDBFS = 0.5 }, true},
		{"squelch NaN", func(c *ChannelSettings) { c.SquelchDBFS = math.NaN() }, true},
		{"squelch -Inf", func(c *ChannelSettings) { c.SquelchDBFS = math.Inf(-1) }, true},
		{"empty demod", func(c *ChannelSettings) { c.Demod = "" }, true},
		{"unknown demod", func(c *ChannelSettings) { c.Demod = "lora" }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := ok
			tc.mutate(&c)
			err := c.Validate(rate)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate()=%v, wantErr=%v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidChannel) {
				t.Errorf("error %v does not wrap ErrInvalidChannel", err)
			}
		})
	}
}

func hubWithChannelPaths(ids ...string) *ReceiverHub {
	hub := NewReceiverHub()
	for _, id := range ids {
		hub.Register(id, PathConfig{}, func() (Receiver, error) {
			return &stubReceiver{name: id, caps: Capabilities{RecommendedSampleRate: 2_048_000}}, nil
		})
	}
	return hub
}

func TestReceiverHub_ChannelDefaults(t *testing.T) {
	hub := hubWithChannelPaths("p1")
	got, err := hub.Channel("p1")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	if got != DefaultChannelSettings() {
		t.Errorf("Channel()=%+v, want defaults %+v", got, DefaultChannelSettings())
	}
}

func TestReceiverHub_SetChannelKeptPerPath(t *testing.T) {
	hub := hubWithChannelPaths("p1", "p2")
	want := ChannelSettings{OffsetHz: -12_000, BandwidthHz: 18_000, SquelchDBFS: -70, Demod: "fsk9600_g3ruh"}
	if err := hub.SetChannel("p1", want); err != nil {
		t.Fatalf("SetChannel: %v", err)
	}
	if got, _ := hub.Channel("p1"); got != want {
		t.Errorf("p1 Channel()=%+v, want %+v", got, want)
	}
	if got, _ := hub.Channel("p2"); got != DefaultChannelSettings() {
		t.Errorf("p2 Channel()=%+v, want defaults", got)
	}
}

func TestReceiverHub_SetChannelRejectsInvalidAndKeepsPrevious(t *testing.T) {
	hub := hubWithChannelPaths("p1")
	bad := DefaultChannelSettings()
	bad.BandwidthHz = 0
	if err := hub.SetChannel("p1", bad); !errors.Is(err, ErrInvalidChannel) {
		t.Fatalf("SetChannel err=%v, want ErrInvalidChannel", err)
	}
	if got, _ := hub.Channel("p1"); got != DefaultChannelSettings() {
		t.Errorf("Channel()=%+v after rejected set, want defaults", got)
	}
}

// The demodulator status carries the gain, so other clients follow it.
func TestReceiverHub_GainDB(t *testing.T) {
	hub := hubWithChannelPaths("p1")
	r, err := hub.Get("p1")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetGain(32.8); err != nil {
		t.Fatal(err)
	}
	if got, err := hub.GainDB("p1"); err != nil || got != 32.8 {
		t.Errorf("GainDB()=%v, %v; want 32.8", got, err)
	}
	if _, err := hub.GainDB("nope"); !errors.Is(err, ErrUnknownRadioPath) {
		t.Errorf("GainDB err=%v, want ErrUnknownRadioPath", err)
	}
}

func TestReceiverHub_ChannelUnknownPath(t *testing.T) {
	hub := hubWithChannelPaths("p1")
	if _, err := hub.Channel("nope"); !errors.Is(err, ErrUnknownRadioPath) {
		t.Errorf("Channel err=%v, want ErrUnknownRadioPath", err)
	}
	if err := hub.SetChannel("nope", DefaultChannelSettings()); !errors.Is(err, ErrUnknownRadioPath) {
		t.Errorf("SetChannel err=%v, want ErrUnknownRadioPath", err)
	}
}
