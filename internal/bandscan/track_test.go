package bandscan

import (
	"math"
	"testing"
)

const (
	stepMs    = 1000 / DecisionsPerSecond
	testBinHz = 625.0
	// synthBins — длина Excess у решения, собранного без IQ. Частота бина считается от неё.
	synthBins = 4096
	t0        = uint64(1_700_000_000_000)
)

// synthDecision builds a detector decision without IQ: excess 0 dB everywhere in the usable band,
// the given peaks raised to their SNR.
func synthDecision(ms, centerHz uint64, peaks ...Peak) Decision {
	half := testUsableHz / int(testBinHz) / 2
	d := Decision{
		TimeMs: ms, DurationMs: stepMs, CenterHz: centerHz, BinHz: testBinHz,
		Excess: make([]float32, synthBins), loBin: synthBins/2 - half, hiBin: synthBins/2 + half,
	}
	for _, p := range peaks {
		for i := d.loBin; i < d.hiBin; i++ {
			if f := d.binFreq(i); f >= p.LoHz && f <= p.HiHz {
				d.Excess[i] = p.SNRDB
			}
		}
		if p.SNRDB >= ThresholdDB {
			d.Peaks = append(d.Peaks, p)
		}
	}
	return d
}

func carrier(centerHz, widthHz float64, snr float32) Peak {
	return Peak{LoHz: centerHz - widthHz/2, HiHz: centerHz + widthHz/2, SNRDB: snr}
}

// feed runs the tracker over stream time [from, to) at the detector decision rate.
func feed(tr *Tracker, from, to uint64, centerHz uint64, peaksAt func(ms uint64) []Peak) {
	for ms := from; ms < to; ms += stepMs {
		tr.Update(synthDecision(ms, centerHz, peaksAt(ms)...))
	}
}

func only(t *testing.T, sigs []Signal) Signal {
	t.Helper()
	if len(sigs) != 1 {
		t.Fatalf("got %d signals %+v, want one", len(sigs), sigs)
	}
	return sigs[0]
}

func steadyAt(hz float64) func(uint64) []Peak {
	return func(uint64) []Peak { return []Peak{carrier(hz, 2_000, 15)} }
}

func TestTracker_SignalShownAfterSecondHit(t *testing.T) {
	tr := NewTracker()
	tr.Update(synthDecision(t0, testCenter, carrier(testCenter+100_000, 2_000, 15)))
	if sigs := tr.Signals(nil); len(sigs) != 0 {
		t.Fatalf("one hit shown: %+v, want hidden until confirmed", sigs)
	}
	tr.Update(synthDecision(t0+stepMs, testCenter, carrier(testCenter+100_000, 2_000, 15)))
	if sigs := tr.Signals(nil); len(sigs) != 0 {
		t.Fatalf("short burst shown: %+v", sigs)
	}
	if len(tr.segs) != 1 {
		t.Fatalf("cache has %d segments, want the burst kept", len(tr.segs))
	}
}

func TestTracker_RemembersSignalForAnHour(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+3_000, testCenter, steadyAt(testCenter+100_000))
	feed(tr, t0+3_000, t0+3_000+HoldMs+stepMs, testCenter, func(uint64) []Peak { return nil })
	if len(tr.Signals(nil)) != 1 {
		t.Fatal("hour list dropped a signal that only just went quiet")
	}
	tr.Update(synthDecision(t0+3_000+RememberMs+stepMs, testCenter))
	if sigs := tr.Signals(nil); len(sigs) != 0 {
		t.Errorf("signal still shown after an hour: %+v", sigs)
	}
}

func TestTracker_DropsSpeckAfterHold(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+2*stepMs, testCenter, steadyAt(testCenter+100_000))
	if len(tr.Signals(nil)) != 0 || len(tr.segs) != 1 {
		t.Fatalf("shown %d cached %d, want hidden and kept", len(tr.Signals(nil)), len(tr.segs))
	}
	feed(tr, t0+2*stepMs, t0+2*stepMs+HoldMs+stepMs, testCenter, func(uint64) []Peak { return nil })
	if sigs := tr.Signals(nil); len(sigs) != 0 || len(tr.segs) != 1 {
		t.Fatalf("after the hold shown %d cached %d", len(sigs), len(tr.segs))
	}
}

func TestTracker_ShortBurstsWithinJoinRadiusBecomeOne(t *testing.T) {
	tr := NewTracker()
	const c = testCenter + 100_000
	ms := t0
	for i, hz := range []float64{c, c + 4_000, c + 2_000} {
		feed(tr, ms, ms+1_000, testCenter, func(uint64) []Peak {
			return []Peak{carrier(hz, 1_000, 15)}
		})
		feed(tr, ms+1_000, ms+2_000, testCenter, func(uint64) []Peak { return nil })
		if i == 0 && len(tr.Signals(nil)) != 0 {
			t.Fatal("first burst shown")
		}
		ms += 5_000
	}
	sigs := tr.Signals(nil)
	if len(sigs) != 1 {
		t.Fatalf("got %d signals, want one source: %+v", len(sigs), sigs)
	}
	if sigs[0].Stats.OnS < 2 || sigs[0].Stats.Bursts < 3 {
		t.Fatalf("joined signal %+v, want on>=2s and 3 bursts", sigs[0].Stats)
	}
}

func TestTracker_DropsWideUnclassifiedAfterHold(t *testing.T) {
	tr := NewTracker()
	wide := func(uint64) []Peak { return []Peak{carrier(testCenter+100_000, 200_000, 15)} }
	feed(tr, t0, t0+5_000, testCenter, wide)
	feed(tr, t0+5_000, t0+5_000+HoldMs+stepMs, testCenter, func(uint64) []Peak { return nil })
	if sigs := tr.Signals(nil); len(sigs) != 0 {
		t.Fatalf("wide unclassified smear kept: %+v", sigs)
	}
}

// Statistics live by absolute frequency: leaving the band pauses observation, coming back resumes it.
func TestTracker_RetuneAwayPausesObservationAndKeepsStats(t *testing.T) {
	tr := NewTracker()
	const sig = testCenter + 100_000
	feed(tr, t0, t0+30_000, testCenter, steadyAt(sig))
	id := only(t, tr.Signals(nil)).ID

	// Two minutes elsewhere: the signal is out of the window, not silent.
	feed(tr, t0+30_000, t0+150_000, testCenter+5_000_000, func(uint64) []Peak { return nil })
	feed(tr, t0+150_000, t0+180_000, testCenter, steadyAt(sig))
	s := only(t, tr.Signals(nil))
	if s.ID != id {
		t.Errorf("id = %s after return, want the same segment %s", s.ID, id)
	}
	if s.Stats.ObservedS < 59 || s.Stats.ObservedS > 61 {
		t.Errorf("observed = %.1f s, want ~60 s (time away not counted)", s.Stats.ObservedS)
	}
	if s.Stats.OnS < 59 {
		t.Errorf("on air = %.1f s, want ~60 s", s.Stats.OnS)
	}
}

func TestTracker_HysteresisKeepsBurstOpen(t *testing.T) {
	tr := NewTracker()
	const sig = testCenter + 100_000
	levels := []float32{15, 15, 5, 5, 15, 3, 15}
	for i, l := range levels {
		tr.Update(synthDecision(t0+uint64(i)*stepMs, testCenter, carrier(sig, 2_000, l)))
	}
	// 15 15 5 5 15 stays one burst (5 dB is within 2 dB of the threshold), 3 dB closes it.
	// Короче 2 с в таблицу не попадает, счёт вспышек лежит в кеше.
	if len(tr.segs) != 1 {
		t.Fatalf("segments %d, want one cached burst", len(tr.segs))
	}
	if b := tr.segs[0].bursts; b != 2 {
		t.Errorf("bursts = %d, want 2", b)
	}
}

func TestTracker_EmissionPulsedWithPeriod(t *testing.T) {
	tr := NewTracker()
	const sig = testCenter + 100_000
	// Two decisions every 10 s, as a beacon frame.
	feed(tr, t0, t0+75_000, testCenter, func(ms uint64) []Peak {
		if (ms-t0)%10_000 < 2*stepMs {
			return []Peak{carrier(sig, 18_000, 12)}
		}
		return nil
	})
	s := only(t, tr.Signals(nil))
	if s.Emission != EmissionPulsed {
		t.Fatalf("emission = %q, want pulsed; stats %+v", s.Emission, s.Stats)
	}
	if math.Abs(s.Stats.PeriodS-10) > 0.3 {
		t.Errorf("period = %.2f s, want 10", s.Stats.PeriodS)
	}
}

func TestTracker_EmissionPulsedWithoutPeriod(t *testing.T) {
	tr := NewTracker()
	const sig = testCenter + 100_000
	starts := []uint64{0, 3_000, 20_000, 24_000, 50_000, 70_000}
	feed(tr, t0, t0+75_000, testCenter, func(ms uint64) []Peak {
		for _, s := range starts {
			if ms-t0 >= s && ms-t0 < s+1_000 {
				return []Peak{carrier(sig, 12_000, 20)}
			}
		}
		return nil
	})
	s := only(t, tr.Signals(nil))
	if s.Emission != EmissionPulsed || s.Stats.PeriodS != 0 {
		t.Errorf("emission = %q period %.1f, want pulsed without period", s.Emission, s.Stats.PeriodS)
	}
}

func TestTracker_EmissionContinuousNeedsFiveMinutes(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+120_000, testCenter, steadyAt(testCenter+100_000))
	if e := only(t, tr.Signals(nil)).Emission; e != EmissionUndetermined {
		t.Errorf("after 2 min emission = %q, want undetermined until 5 min", e)
	}
	feed(tr, t0+120_000, t0+301_000, testCenter, steadyAt(testCenter+100_000))
	if e := only(t, tr.Signals(nil)).Emission; e != EmissionContinuous {
		t.Errorf("after 5 min emission = %q, want continuous", e)
	}
}

func TestTracker_EmissionUndeterminedUnderOneMinute(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+30_000, testCenter, steadyAt(testCenter+100_000))
	s := only(t, tr.Signals(nil))
	if s.Emission != EmissionUndetermined || s.Origin != OriginUnknown {
		t.Errorf("emission %q origin %q, want undetermined and unknown under 60 s", s.Emission, s.Origin)
	}
}

func TestTracker_OriginTerrestrialWhenStill(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+61_000, testCenter, steadyAt(testCenter+100_000))
	if o := only(t, tr.Signals(nil)).Origin; o != OriginTerrestrial {
		t.Errorf("origin = %q, want terrestrial after 60 s on one frequency", o)
	}
}

// The segment follows the expected transmitter as Doppler moves it: probable at first, then satellite.
func TestTracker_OriginSatelliteFollowsExpected(t *testing.T) {
	tr := NewTracker()
	freq := func(ms uint64) float64 { return testCenter + 100_000 - 150*float64(ms-t0)/1000 } // −150 Гц/с
	expected := func(ms uint64) []Expected {
		return []Expected{{NoradID: 53385, UUID: "tx1", FreqHz: freq(ms)}}
	}
	peaks := func(ms uint64) []Peak { return []Peak{carrier(freq(ms), 18_000, 14)} }

	feed(tr, t0, t0+3_000, testCenter, peaks)
	s := only(t, tr.Signals(expected(t0+2_000)))
	if s.Origin != OriginSatelliteProbable || s.NoradID != 53385 {
		t.Fatalf("at start origin %q norad %d, want probable 53385", s.Origin, s.NoradID)
	}
	for ms := t0 + 2_000; ms < t0+20_000; ms += 1_000 {
		feed(tr, ms, ms+1_000, testCenter, peaks)
		s = only(t, tr.Signals(expected(ms+1_000)))
	}
	if s.Origin != OriginSatellite || s.NoradID != 53385 {
		t.Errorf("after the transmitter moved origin %q norad %d, want satellite 53385", s.Origin, s.NoradID)
	}
	if s.Stats.DriftHzS > -100 || s.Stats.DriftHzS < -200 {
		t.Errorf("drift = %.0f Hz/s, want ~−150", s.Stats.DriftHzS)
	}
}

// A ground station on a satellite frequency: the expected marker passes by, the segment stays put.
func TestTracker_OriginTerrestrialWhenExpectedLeaves(t *testing.T) {
	tr := NewTracker()
	const sig = testCenter + 100_000
	expAt := func(ms uint64) []Expected {
		return []Expected{{NoradID: 1, UUID: "tx", FreqHz: sig - 200*float64(ms-t0)/1000}}
	}
	for ms := t0; ms < t0+62_000; ms += 1_000 {
		feed(tr, ms, ms+1_000, testCenter, steadyAt(sig))
		tr.Signals(expAt(ms + 1_000))
	}
	if s := only(t, tr.Signals(expAt(t0+62_000))); s.Origin != OriginTerrestrial || s.NoradID != 0 {
		t.Errorf("origin %q norad %d, want terrestrial without a satellite", s.Origin, s.NoradID)
	}
}

// One continuous drifting signal whose skirt flickers off as a separate peak stays one segment.
func TestTracker_DriftingSignalWithFlickeringSkirtIsOneSegment(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+70_000, testCenter, func(ms uint64) []Peak {
		c := testCenter + 100_000 - 100*float64(ms-t0)/1000 // −100 Гц/с
		k := (ms - t0) / stepMs
		switch k % 3 {
		case 0: // весь сигнал одним куском
			return []Peak{{LoHz: c - 5_000, HiHz: c + 7_000, SNRDB: 15}}
		case 1: // правый склон оторвался
			return []Peak{{LoHz: c - 5_000, HiHz: c + 3_000, SNRDB: 15}, {LoHz: c + 5_500, HiHz: c + 7_000, SNRDB: 7}}
		default: // склон ушёл под порог
			return []Peak{{LoHz: c - 5_000, HiHz: c + 3_000, SNRDB: 15}}
		}
	})
	sigs := tr.Signals(nil)
	if len(sigs) != 1 {
		t.Fatalf("got %d signals, want one drifting signal: %+v", len(sigs), sigs)
	}
	if e := sigs[0].Emission; e == EmissionPulsed {
		t.Errorf("continuous signal emission = %q", e)
	}
}

// Neighbouring transmitters 20 kHz apart are two signals, not one.
func TestTracker_SeparateSignalsStaySeparate(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+10_000, testCenter, func(uint64) []Peak {
		return []Peak{carrier(testCenter+100_000, 10_000, 15), carrier(testCenter+120_000, 10_000, 15)}
	})
	if n := len(tr.Signals(nil)); n != 2 {
		t.Errorf("got %d signals, want two neighbouring transmitters", n)
	}
}

// Two pieces seen apart, then bridged by one peak, become the older segment.
func TestTracker_BridgingPeakMergesIntoOlder(t *testing.T) {
	tr := NewTracker()
	const c = testCenter + 100_000
	feed(tr, t0, t0+1_000, testCenter, func(uint64) []Peak { return []Peak{carrier(c-3_000, 2_000, 15)} })
	feed(tr, t0+1_000, t0+2_000, testCenter, func(uint64) []Peak {
		return []Peak{carrier(c-3_000, 2_000, 15), carrier(c+3_000, 2_000, 15)}
	})
	older := tr.Signals(nil)[0].ID
	feed(tr, t0+2_000, t0+3_000, testCenter, func(uint64) []Peak { return []Peak{carrier(c, 8_000, 15)} })
	if s := only(t, tr.Signals(nil)); s.ID != older || s.Stats.FirstSeenMs != t0 {
		t.Errorf("merged into %s first seen %d, want the older %s from t0", s.ID, s.Stats.FirstSeenMs, older)
	}
}

func TestTracker_ShownRowStaysWhenItGrowsWide(t *testing.T) {
	tr := NewTracker()
	const c = testCenter + 100_000
	feed(tr, t0, t0+3_000, testCenter, func(uint64) []Peak {
		return []Peak{carrier(c, 2_000, 15)}
	})
	got := tr.Signals(nil)
	if len(got) != 1 {
		t.Fatalf("narrow signal not shown: %+v", got)
	}
	id := got[0].ID
	seg := tr.segs[0]
	seg.hiHz = seg.loHz + 80_000
	seg.structure = nil
	got = tr.Signals(nil)
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("row dropped after it grew wide: %+v", got)
	}
}

func TestTracker_TwoShownRowsAreNotCollapsed(t *testing.T) {
	tr := NewTracker()
	const c = testCenter + 100_000
	feed(tr, t0, t0+3_000, testCenter, func(uint64) []Peak {
		return []Peak{carrier(c, 2_000, 15), carrier(c+8_000, 2_000, 15)}
	})
	if n := len(tr.Signals(nil)); n != 2 {
		t.Fatalf("got %d signals before the bridge, want 2", n)
	}
	feed(tr, t0+3_000, t0+4_000, testCenter, func(uint64) []Peak {
		return []Peak{carrier(c+4_000, 12_000, 15)}
	})
	if n := len(tr.Signals(nil)); n != 2 {
		t.Fatalf("bridge collapsed shown rows to %d", n)
	}
}

func TestTracker_EvictsLongestSilentOverLimit(t *testing.T) {
	tr := NewTracker()
	tr.limit = 3
	for i := range 4 {
		hz := testCenter + 100_000 + float64(i)*50_000
		feed(tr, t0+uint64(i)*1_000, t0+uint64(i)*1_000+2*stepMs, testCenter, steadyAt(hz))
	}
	if n := len(tr.segs); n != 3 {
		t.Fatalf("segments = %d, want the limit 3", n)
	}
	for _, s := range tr.segs {
		if math.Abs(s.center()-(testCenter+100_000)) < testBinHz {
			t.Error("the longest silent segment survived eviction")
		}
	}
}

func TestBandHint(t *testing.T) {
	cases := []struct {
		hz   float64
		want string
	}{
		{100_100_000, "Вещ. ЧМ"},
		{137_100_000, "Метео"},
		{145_800_000, "Люб. 2 м"},
		{405_020_000, "Радиозонд"},
		{433_920_000, "ЛПД 433"},
		{437_800_000, "Люб. 70 см"},
		{446_100_000, "PMR 446"},
		{162_000_000, ""},
	}
	for _, c := range cases {
		if got := bandHint(c.hz); got != c.want {
			t.Errorf("bandHint(%.0f) = %q, want %q", c.hz, got, c.want)
		}
	}
}
