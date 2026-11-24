package demod

import (
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

func TestAutoPicker_LocksAfterTwoGoodFrames(t *testing.T) {
	air := beaconAirOf(t, testRate, 3, time.Second, "afsk1200")
	frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)

	picker := NewAutoPicker()
	var clock time.Time
	picker.now = func() time.Time { return clock }
	picker.window = time.Minute
	picker.Configure("k", []string{"fsk9600_g3ruh", "afsk1200"})

	cs := sdr.ChannelSettings{
		OffsetHz: testOffset, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodAuto,
	}
	for _, f := range frames {
		clock = clock.Add(time.Millisecond)
		_, st := picker.Process(f, cs, 0)
		if st.AutoPhase == "locked" {
			if st.AutoLocked != "afsk1200" {
				t.Fatalf("locked %q, want afsk1200", st.AutoLocked)
			}
			break
		}
	}
	if picker.Locked() != "afsk1200" {
		t.Fatalf("pinned=%q", picker.Locked())
	}
}

func TestAutoPicker_RemembersPinAcrossKeys(t *testing.T) {
	p := NewAutoPicker()
	p.Configure("tx-a", []string{"afsk1200", "fsk9600_g3ruh"})
	p.pinned = "afsk1200"
	p.Configure("tx-b", []string{"fsk9600_geoscan"})
	if p.Locked() != "" {
		t.Fatalf("other key must start search, got %q", p.Locked())
	}
	p.Configure("tx-a", []string{"afsk1200", "fsk9600_g3ruh"})
	if p.Locked() != "afsk1200" {
		t.Fatalf("return to tx-a: pinned=%q, want remembered afsk1200", p.Locked())
	}
}

func TestAutoPicker_ReviewClearsMemory(t *testing.T) {
	p := NewAutoPicker()
	clock := time.Unix(100, 0)
	p.now = func() time.Time { return clock }
	p.review = 100 * time.Millisecond
	p.Configure("k", []string{"afsk1200"})
	p.pinned = "afsk1200"
	p.remember("k", "afsk1200")
	p.lastGood = clock
	clock = clock.Add(200 * time.Millisecond)
	cs := sdr.ChannelSettings{BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodAuto}
	f := sdr.IQFrame{Samples: make([]complex64, 64), SampleRate: uint32(testRate)}
	_, st := p.Process(f, cs, 0)
	if p.Locked() != "" || st.AutoPhase != "search" {
		t.Fatalf("review: pinned=%q phase=%s", p.Locked(), st.AutoPhase)
	}
	p.Configure("other", []string{"afsk1200"})
	p.Configure("k", []string{"afsk1200"})
	if p.Locked() != "" {
		t.Fatalf("failed demod must forget pin, got %q", p.Locked())
	}
}

func TestAutoConfigKey_StableWithoutInstantFreq(t *testing.T) {
	h := CandidateHints{Mode: "AFSK", Baud: 1200, CardProfile: "afsk1200"}
	a := autoConfigKey(25544, 435_000_000, 18_000, h)
	b := autoConfigKey(25544, 435_001_500, 18_000, h) // доплер сдвинул центр
	if a != b {
		t.Fatalf("key must ignore live freq: %q vs %q", a, b)
	}
	c := autoConfigKey(25544, 435_000_000, 18_000, CandidateHints{Mode: "FSK", Baud: 9600})
	if a == c {
		t.Fatal("different mode/baud must change key")
	}
}
