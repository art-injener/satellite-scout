package demod

import (
	"math"
	"math/cmplx"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sstv"
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

func TestAutoPicker_SearchReportsPerCandidateStats(t *testing.T) {
	// Single AFSK frame is below autoNeedGoodHDLC (2), so AutoPicker stays in search
	// and must report per-candidate stats for the candidate that decoded a frame.
	air := beaconAirOf(t, testRate, 1, time.Second, "afsk1200")
	frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)

	picker := NewAutoPicker()
	clock := time.Unix(4_000, 0)
	picker.now = func() time.Time { return clock }
	picker.Configure("search-stats", []string{"fsk9600_geoscan", "afsk1200"})

	cs := sdr.ChannelSettings{
		OffsetHz: testOffset, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodAuto,
	}
	var lastSt Status
	for _, f := range frames {
		clock = clock.Add(time.Millisecond)
		_, st := picker.Process(f, cs, 0)
		lastSt = st
	}
	if lastSt.AutoPhase != "search" {
		t.Fatalf("phase=%q, want search after 1 HDLC frame", lastSt.AutoPhase)
	}
	idx := slices.IndexFunc(lastSt.AutoStats, func(s AutoCandidateStat) bool {
		return s.ID == "afsk1200" && s.Good >= 1
	})
	if idx < 0 {
		t.Fatalf("auto_stats=%+v, want afsk1200 with good>=1", lastSt.AutoStats)
	}
}

func TestAutoPicker_LocksOnSSTVByVIS(t *testing.T) {
	src := sstv.TestPattern(320, 240)
	frames := toFrames(sstvAir(t, src, sstv.Robot36, 20), sstvRate, 8192, 0)

	picker := NewAutoPicker()
	clock := time.Unix(5_000, 0)
	picker.now = func() time.Time { return clock }
	picker.Configure("sstv-tx", []string{"fsk9600_geoscan", "gmsk9600_usp", DemodSSTV})

	cs := sdr.ChannelSettings{
		OffsetHz: 20_000, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodAuto,
	}
	events := make([]sstv.Event, 0, 8)
	var lockedAt int
	for i, f := range frames {
		clock = clock.Add(time.Duration(len(f.Samples)) * time.Second / sstvRate)
		_, st := picker.Process(f, cs, 0)
		events = append(events, picker.Pictures()...)
		if st.AutoPhase == "locked" && lockedAt == 0 {
			lockedAt = i + 1
		}
	}
	if picker.Locked() != DemodSSTV {
		t.Fatalf("locked = %q, want %q", picker.Locked(), DemodSSTV)
	}
	var hasStart, hasDone bool
	for _, e := range events {
		if e.Kind == sstv.EventStart && e.Mode.ID == "robot36" {
			hasStart = true
		}
		if e.Kind == sstv.EventDone {
			hasDone = true
		}
	}
	if !hasStart || !hasDone {
		t.Fatalf("sstv events start=%v done=%v, want full Robot36 picture via AutoPicker", hasStart, hasDone)
	}
}

func cwAir(rate float64, offsetHz, snrDB float64, text string, sec float64) []complex64 {
	n := int(rate * sec)
	m := modem.NewOOK(modem.OOKConfig{SampleRate: rate, Baud: modem.CWBaud(18)})
	var clean []complex64
	for len(clean) < n {
		clean = m.Modulate(clean, modem.MorseLevels(text))
	}
	clean = clean[:n]
	ch := modem.NewChannel(modem.ChannelConfig{
		SampleRate:       rate,
		OffsetHz:         func(time.Duration) float64 { return offsetHz },
		SNRdB:            snrDB,
		NoiseBandwidthHz: 15_000,
		Seed:             31,
	})
	return ch.Process(nil, clean)
}

func TestAutoPicker_LocksOnCWAndIgnoresCarrierAndNoise(t *testing.T) {
	const rate = 256_000.0
	air := cwAir(rate, 20_000, 18, "CQ CQ DE RS0SIM K ", 3.0)
	frames := toFrames(air, rate, 8192, 0)

	picker := NewAutoPicker()
	clock := time.Unix(6_000, 0)
	picker.now = func() time.Time { return clock }
	picker.Configure("cw-tx", []string{"fsk9600_g3ruh", "cw"})

	cs := sdr.ChannelSettings{
		OffsetHz: 20_000, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodAuto,
	}
	var audioSamples int
	for _, f := range frames {
		clock = clock.Add(time.Duration(len(f.Samples)) * time.Second / rate)
		_, _ = picker.Process(f, cs, 0)
		audioSamples += len(picker.Audio())
	}
	if picker.Locked() != "cw" {
		t.Fatalf("locked = %q, want cw", picker.Locked())
	}
	if audioSamples == 0 {
		t.Fatal("expected CW audio output after locking on cw")
	}

	// Plain carrier must not trigger CW lock.
	carrierClean := make([]complex64, int(rate*2.0))
	for i := range carrierClean {
		carrierClean[i] = complex64(cmplx.Rect(1, 2*math.Pi*20_000*float64(i)/rate))
	}
	ch := modem.NewChannel(modem.ChannelConfig{
		SampleRate: rate, SNRdB: 20, NoiseBandwidthHz: 15_000, Seed: 33,
	})
	carrierFrames := toFrames(ch.Process(nil, carrierClean), rate, 8192, 0)
	picker2 := NewAutoPicker()
	picker2.now = func() time.Time { return clock }
	picker2.Configure("cw-carrier", []string{"fsk9600_g3ruh", "cw"})
	for _, f := range carrierFrames {
		clock = clock.Add(time.Duration(len(f.Samples)) * time.Second / rate)
		picker2.Process(f, cs, 0)
	}
	if picker2.Locked() == "cw" {
		t.Fatal("plain carrier must not lock on cw")
	}
}

func TestAutoPicker_FrameProfileBeatsCWHint(t *testing.T) {
	air := beaconAirOf(t, testRate, 1, time.Second, "fsk9600_geoscan")
	frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)

	picker := NewAutoPicker()
	clock := time.Unix(7_000, 0)
	picker.now = func() time.Time { return clock }
	picker.Configure("mixed-tx", []string{"fsk9600_geoscan", "cw"})

	cs := sdr.ChannelSettings{
		OffsetHz: testOffset, BandwidthHz: sdr.MaxChannelBandwidthHz, SquelchDBFS: -90, Demod: DemodAuto,
	}
	for _, f := range frames {
		clock = clock.Add(time.Millisecond)
		_, st := picker.Process(f, cs, 0)
		if st.AutoPhase == "locked" {
			break
		}
	}
	if picker.Locked() != "fsk9600_geoscan" {
		t.Fatalf("locked = %q, want fsk9600_geoscan (frame profile must win over cw)", picker.Locked())
	}
}

// runVoiceAuto feeds the air to a picker and returns the last status and any audio emitted.
func runVoiceAuto(picker *AutoPicker, air []complex64, cs sdr.ChannelSettings) (Status, []float32) {
	var st Status
	var audio []float32
	for _, f := range toFrames(air, testRate, sdr.DefaultBlockSize, 0) {
		_, st = picker.Process(f, cs, 0)
		audio = append(audio, picker.Audio()...)
	}
	return st, audio
}

func TestAutoPicker_DoesNotLockVoiceProfiles(t *testing.T) {
	cases := []struct {
		name  string
		cands []string
		air   []complex64
		bw    uint32
	}{
		{"fm", []string{"fm", "afsk1200", "fsk9600_g3ruh"}, fmToneAir(4), 15_000},
		{"am", []string{"am", "fsk9600_g3ruh"}, amToneAir(4), 15_000},
		{"usb", []string{"usb", "lsb", "fsk9600_g3ruh"}, ssbAir(1200, 1700, 4), 2800},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			picker := NewAutoPicker()
			picker.Configure("voice-"+tc.name, tc.cands)
			st, audio := runVoiceAuto(picker, tc.air, voiceSettings(DemodAuto, tc.bw, -90))
			if st.AutoPhase != "search" || st.AutoLocked != "" || picker.Locked() != "" {
				t.Fatalf("phase=%s locked=%q picker=%q, want search (voice is manual-only)",
					st.AutoPhase, st.AutoLocked, picker.Locked())
			}
			if len(audio) != 0 {
				t.Fatalf("emitted %d audio samples, want 0 (voice is manual-only)", len(audio))
			}
		})
	}
}

func TestAutoPicker_AlwaysUsesAdaptiveSquelch(t *testing.T) {
	// Even when caller passes SquelchAuto=false and SquelchDBFS=-90, Auto mode
	// must compute an adaptive threshold and stay closed on -55 dBFS broadband noise.
	rng := rand.New(rand.NewPCG(33, 44))
	sigma := math.Sqrt(math.Pow(10, -55.0/10.0) / 2)
	noise := make([]complex64, int(testRate/2))
	for i := range noise {
		noise[i] = complex(float32(rng.NormFloat64()*sigma), float32(rng.NormFloat64()*sigma))
	}
	picker := NewAutoPicker()
	picker.Configure("auto-noise", []string{"afsk1200", "fsk9600_g3ruh"})
	cs := sdr.ChannelSettings{
		OffsetHz:    testOffset,
		BandwidthHz: 15_000,
		SquelchDBFS: -90,
		SquelchAuto: false,
		Demod:       DemodAuto,
	}
	var st Status
	for _, f := range toFrames(noise, testRate, sdr.DefaultBlockSize, 0) {
		_, st = picker.Process(f, cs, 0)
	}
	if st.SquelchOpen {
		t.Fatalf("Auto squelch opened on pure noise: level=%.1f th=%.1f", st.LevelDBFS, st.SquelchDBFS)
	}
	if !st.SquelchAuto {
		t.Fatal("Auto status SquelchAuto = false, want true")
	}
	if st.SquelchDBFS <= -85 {
		t.Fatalf("Auto threshold %.1f dBFS, want adaptive threshold above channel noise", st.SquelchDBFS)
	}
}

func TestAutoPicker_KnownSatExpandsToSameFamilyAfterActiveSignal(t *testing.T) {
	picker := NewAutoPicker()
	picker.expandAfter = time.Second // accelerated in unit test (default is 15 s)
	plan := CandidatePlan{
		Initial:  []string{"gmsk4800_usp"},
		Expanded: []string{"gmsk4800_usp", "fsk9600_geoscan"},
	}
	picker.ConfigurePlan("sat-53385", plan)
	cs := sdr.ChannelSettings{
		OffsetHz:    testOffset,
		BandwidthHz: sdr.MaxChannelBandwidthHz,
		SquelchDBFS: -90,
		Demod:       DemodAuto,
	}

	// 2 seconds of silence (squelch closed) must NOT trigger expansion.
	silence := make([]complex64, int(2*testRate))
	for _, f := range toFrames(silence, testRate, sdr.DefaultBlockSize, 0) {
		picker.Process(f, cs, 0)
	}
	if !slices.Equal(picker.Candidates(), plan.Initial) {
		t.Fatalf("candidates expanded during silence: %v", picker.Candidates())
	}

	// Active GEOSCAN signal does not match gmsk4800_usp: after 1 s of active signal
	// picker expands to the same FSK family and locks fsk9600_geoscan.
	air := beaconAirOf(t, testRate, 4, 800*time.Millisecond, "fsk9600_geoscan")
	var expandedAt bool
	for _, f := range toFrames(air, testRate, sdr.DefaultBlockSize, 3000) {
		picker.Process(f, cs, 0)
		if slices.Equal(picker.Candidates(), plan.Expanded) {
			expandedAt = true
		}
	}
	if !expandedAt {
		t.Fatalf(
			"expected expansion to %v after active signal without CRC, stayed %v",
			plan.Expanded, picker.Candidates(),
		)
	}
	if picker.Locked() != "fsk9600_geoscan" {
		t.Fatalf("Locked() = %q, want fsk9600_geoscan after family expansion", picker.Locked())
	}
}

func TestAutoPicker_BandwidthFollowsTargetCandidates(t *testing.T) {
	picker := NewAutoPicker()
	picker.ConfigurePlan("sat-afsk", CandidatePlan{
		Initial:  []string{"afsk1200"},
		Expanded: []string{"afsk1200", "fsk9600_g3ruh"},
	})
	cs := sdr.ChannelSettings{
		OffsetHz:    testOffset,
		BandwidthHz: sdr.MaxChannelBandwidthHz, // 18 kHz from UI
		SquelchDBFS: -90,
		Demod:       DemodAuto,
	}
	frames := toFrames(make([]complex64, sdr.DefaultBlockSize*2), testRate, sdr.DefaultBlockSize, 0)
	_, st := picker.Process(frames[0], cs, 0)
	if st.PassbandHz > 15_000 {
		t.Fatalf("narrow candidate passband = %.0f Hz, want <= 15000 Hz", st.PassbandHz)
	}
}
