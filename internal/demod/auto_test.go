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

func TestAutoPicker_GMSK9600USPWithoutHintsLocksOnOneFrame(t *testing.T) {
	air := beaconAirOf(t, testRate, 1, time.Second, "gmsk9600_usp")
	frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)

	picker := NewAutoPicker()
	clock := time.Unix(1_000, 0)
	picker.now = func() time.Time { return clock }
	picker.Configure("no-hints", BuildCandidates(CandidateHints{}))
	if len(picker.Candidates()) <= 5 {
		t.Fatalf("expected all frame candidates without limit 5, got %d", len(picker.Candidates()))
	}

	cs := sdr.ChannelSettings{
		OffsetHz: testOffset, BandwidthHz: sdr.MaxChannelBandwidthHz, SquelchDBFS: -90, Demod: DemodAuto,
	}
	var pkts []taggedPacket
	for _, f := range frames {
		clock = clock.Add(time.Millisecond)
		p, st := picker.Process(f, cs, 0)
		pkts = append(pkts, p...)
		if st.AutoPhase == "locked" {
			break
		}
	}
	if picker.Locked() != "gmsk9600_usp" {
		t.Fatalf(
			"locked = %q, want gmsk9600_usp on single valid USP frame without hints (pkts=%d)",
			picker.Locked(), len(pkts),
		)
	}
	if len(pkts) < 1 {
		t.Fatal("expected at least 1 decoded packet")
	}
}

func TestAutoPicker_ConfigureSameKeyKeepsHitsAndDeframers(t *testing.T) {
	air := beaconAirOf(t, testRate, 2, time.Second, "afsk1200")
	frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)

	picker := NewAutoPicker()
	clock := time.Unix(2_000, 0)
	picker.now = func() time.Time { return clock }
	picker.window = time.Minute
	picker.Configure("same-key", []string{"fsk9600_g3ruh", "afsk1200", "gmsk9600_usp"})

	cs := sdr.ChannelSettings{
		OffsetHz: testOffset, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodAuto,
	}
	reconfigured := false
	for _, f := range frames {
		clock = clock.Add(time.Millisecond)
		p, st := picker.Process(f, cs, 0)
		if len(p) > 0 && !reconfigured {
			// Simulate hints update arriving mid-search on the same TX key.
			picker.Configure("same-key", []string{"afsk1200", "fsk9600_g3ruh", "gmsk9600_usp", "bpsk1200"})
			reconfigured = true
		}
		if st.AutoPhase == "locked" {
			break
		}
	}
	if !reconfigured {
		t.Fatal("never decoded first afsk1200 frame")
	}
	if picker.Locked() != "afsk1200" {
		t.Fatalf("locked = %q, want afsk1200 (Configure on same key must preserve hits)", picker.Locked())
	}
}

func TestAutoPicker_StrongFECProfilesLockOnSingleFrame(t *testing.T) {
	cases := []struct {
		id     string
		hints  CandidateHints
		want   string
		period time.Duration
		lead   int
		bw     uint32
	}{
		{
			id: "gmsk2400_usp", want: "gmsk2400_usp",
			period: 3 * time.Second, lead: testRate / 5, bw: 15_000,
		},
		{
			id: "gmsk4800_ax100", hints: CandidateHints{Mode: "GMSK", Baud: 4800}, want: "gmsk4800_ax100",
			period: time.Second, bw: 15_000,
		},
		{
			id: "fsk9600_geoscan", want: "fsk9600_geoscan",
			period: time.Second, bw: sdr.MaxChannelBandwidthHz,
		},
		{
			id: "qpsk_ccsds", want: "qpsk_ccsds",
			period: time.Second, bw: sdr.MaxChannelBandwidthHz,
		},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			var air []complex64
			if tc.lead > 0 {
				air = beaconAirWithLead(t, testRate, 1, tc.period, tc.id, tc.lead)
			} else {
				air = beaconAirOf(t, testRate, 1, tc.period, tc.id)
			}
			frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)

			picker := NewAutoPicker()
			clock := time.Unix(3_000, 0)
			picker.now = func() time.Time { return clock }
			picker.Configure("k-"+tc.id, BuildCandidates(tc.hints))

			cs := sdr.ChannelSettings{
				OffsetHz: testOffset, BandwidthHz: tc.bw, SquelchDBFS: -90, Demod: DemodAuto,
			}
			var pkts []taggedPacket
			for _, f := range frames {
				clock = clock.Add(time.Millisecond)
				p, st := picker.Process(f, cs, 0)
				pkts = append(pkts, p...)
				if st.AutoPhase == "locked" {
					break
				}
			}
			if picker.Locked() != tc.want {
				t.Fatalf("locked = %q, want %q on single valid frame", picker.Locked(), tc.want)
			}
			if len(pkts) != 1 {
				t.Fatalf("got %d packets, want 1 without duplicates across equivalent pipelines", len(pkts))
			}
		})
	}
}
