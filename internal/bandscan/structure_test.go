package bandscan

import (
	"math"
	"strconv"
	"testing"

	"github.com/art-injener/satellite-scout/internal/signalclass"
	"github.com/art-injener/satellite-scout/internal/signals"
)

func segID(t *testing.T, tr *Tracker) int {
	t.Helper()
	id, err := strconv.Atoi(only(t, tr.Signals(nil)).ID[1:])
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTracker_JobForSteadySignalTakesLastSlice(t *testing.T) {
	tr := NewTracker()
	const sig = testCenter + 100_000
	feed(tr, t0, t0+uint64(sliceMs)+stepMs, testCenter, steadyAt(sig))
	job, ok := tr.nextJob(0)
	if !ok {
		t.Fatal("no job for a confirmed steady signal")
	}
	if job.toMs != tr.nowMs || job.fromMs != tr.nowMs-sliceMs {
		t.Errorf("window %d..%d, want the last %d ms up to %d", job.fromMs, job.toMs, sliceMs, tr.nowMs)
	}
	if math.Abs(job.centerHz-sig) > testBinHz || job.bandwidthHz < 2_000 {
		t.Errorf("job at %.0f Hz %.0f Hz wide, want the segment", job.centerHz, job.bandwidthHz)
	}
	if _, again := tr.nextJob(0); again {
		t.Error("the same segment queued again right away")
	}
	after := t0 + uint64(sliceMs) + stepMs
	feed(tr, after, after+reclassifyMs+stepMs, testCenter, steadyAt(sig))
	if _, again := tr.nextJob(0); !again {
		t.Errorf("no repeat job after %d ms", reclassifyMs)
	}
}

func TestTracker_JobWaitsForShortBurstToEnd(t *testing.T) {
	tr := NewTracker()
	const sig = testCenter + 100_000
	burst := func(ms uint64) []Peak {
		if ms < t0+2*stepMs {
			return []Peak{carrier(sig, 12_000, 20)}
		}
		return nil
	}
	feed(tr, t0, t0+2*stepMs, testCenter, burst)
	if _, ok := tr.nextJob(0); ok {
		t.Fatal("job while the short burst is still on air")
	}
	feed(tr, t0+2*stepMs, t0+5*stepMs, testCenter, burst)
	job, ok := tr.nextJob(0)
	if !ok {
		t.Fatal("no job after the burst ended")
	}
	if job.fromMs != t0-burstMarginMs || job.toMs != t0+stepMs+burstMarginMs {
		t.Errorf("window %d..%d ms from t0, want the burst with margins", int64(job.fromMs-t0), job.toMs-t0)
	}
}

func TestTracker_JobNotBeforeHistory(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+uint64(sliceMs)+stepMs, testCenter, steadyAt(testCenter+100_000))
	// Окно короче кольца: from сдвигаем к началу доступной истории, а не бросаем задание.
	job, ok := tr.nextJob(tr.nowMs - 100)
	if !ok {
		t.Fatal("expected a job clamped to available IQ history")
	}
	if job.fromMs != tr.nowMs-100 {
		t.Errorf("fromMs=%d, want history start %d", job.fromMs, tr.nowMs-100)
	}
}

func TestTracker_StructureVotesOverLastThree(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+3_000, testCenter, steadyAt(testCenter+100_000))
	id := segID(t, tr)
	fsk := signalclass.Result{Modulation: signalclass.Mod2FSK, Why: "fsk"}
	fm := signalclass.Result{Modulation: signalclass.ModFM, Why: "fm"}

	tr.SetStructure(id, fsk)
	tr.SetStructure(id, fm)
	if s := only(t, tr.Signals(nil)).Structure; s == nil || s.Modulation != signalclass.Mod2FSK {
		t.Fatalf("structure %+v after a tie, want the one shown before", s)
	}
	tr.SetStructure(id, fm)
	if s := only(t, tr.Signals(nil)).Structure; s.Modulation != signalclass.ModFM {
		t.Errorf("structure %q, want fm by two of three", s.Modulation)
	}
}

func TestTracker_StructureForGoneSegmentIgnored(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+3_000, testCenter, steadyAt(testCenter+100_000))
	tr.SetStructure(999, signalclass.Result{Modulation: signalclass.ModFM})
	if s := only(t, tr.Signals(nil)).Structure; s != nil {
		t.Errorf("structure %+v from an unknown segment", s)
	}
}

func TestTracker_WideFMOnBroadcastBandGetsService(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+3_000, 104_500_000, steadyAt(104_100_000))
	id := segID(t, tr)
	tr.SetStructure(id, signalclass.Result{Modulation: signalclass.ModWFM, Why: "wide"})
	s := only(t, tr.Signals(nil)).Structure
	if s == nil || s.Service != signalclass.ServiceBroadcast || s.ServiceLabel != signalclass.LabelBroadcast {
		t.Fatalf("structure %+v, want broadcast on the FM band", s)
	}
}

// Эфирное радио без имени PS тоже переразбирается чаще обычных 10 с.
func TestTracker_BroadcastWithoutStationReclassifiesSooner(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+uint64(sliceMs)+stepMs, 104_500_000, steadyAt(104_100_000))
	id := segID(t, tr)
	tr.SetStructure(id, signalclass.Result{
		Modulation: signalclass.ModWFM, Service: signalclass.ServiceBroadcast, ServiceLabel: signalclass.LabelBroadcast,
	})
	if _, ok := tr.nextJob(0); !ok {
		t.Fatal("first job")
	}
	after := tr.nowMs + rdsRetryMs + stepMs
	feed(tr, tr.nowMs, after, 104_500_000, steadyAt(104_100_000))
	if _, ok := tr.nextJob(0); !ok {
		t.Fatalf("no retry after %d ms for broadcast without PS", rdsRetryMs)
	}
}

// Участок с частично собранным RDS PS переразбирается раньше обычных 10 с.
func TestTracker_PartialRDSPSReclassifiesSooner(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+uint64(sliceMs)+stepMs, 104_500_000, steadyAt(104_100_000))
	id := segID(t, tr)
	tr.SetStructure(id, signalclass.Result{
		Modulation: signalclass.ModWFM, Service: signalclass.ServiceBroadcast, ServiceLabel: signalclass.LabelBroadcast,
		RDSParts: [4][2]byte{{'D', 'F'}}, RDSHave: [4]bool{true, false, false, false},
	})
	if _, ok := tr.nextJob(0); !ok {
		t.Fatal("first job")
	}
	after := tr.nowMs + rdsRetryMs + stepMs
	feed(tr, tr.nowMs, after, 104_500_000, steadyAt(104_100_000))
	if _, ok := tr.nextJob(0); !ok {
		t.Fatalf("no retry after %d ms with partial PS", rdsRetryMs)
	}
}

// Сегменты RDS PS с разных разборов склеиваются в имя станции.
func TestTracker_RDSStationMergesAcrossClassifies(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+3_000, 104_500_000, steadyAt(104_100_000))
	id := segID(t, tr)
	a := signalclass.Result{
		Modulation: signalclass.ModWFM, Service: signalclass.ServiceBroadcast,
		RDSParts: [4][2]byte{{'E', 'U'}, {'R', 'O'}}, RDSHave: [4]bool{true, true, false, false},
	}
	b := signalclass.Result{
		Modulation: signalclass.ModWFM, Service: signalclass.ServiceBroadcast,
		RDSParts: [4][2]byte{{}, {}, {'P', 'A'}, {'+', ' '}}, RDSHave: [4]bool{false, false, true, true},
	}
	tr.SetStructure(id, a)
	if s := only(t, tr.Signals(nil)).Structure; s != nil && s.Station != "" {
		t.Fatalf("station %q after two segments, want empty", s.Station)
	}
	tr.SetStructure(id, b)
	s := only(t, tr.Signals(nil)).Structure
	if s == nil || s.Station != "EUROPA+" {
		t.Fatalf("structure %+v, want station EUROPA+", s)
	}
}

func feedBursts(tr *Tracker, center uint64, sig float64, dur uint64) {
	feed(tr, t0, t0+dur, center, func(ms uint64) []Peak {
		if (ms-t0)%10_000 < 2*stepMs {
			return []Peak{carrier(sig, 12_000, 16)}
		}
		return nil
	})
}

func TestTracker_AmateurVoiceBeforeEmissionIsKnown(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+5_000, testCenter, steadyAt(testCenter+100_000))
	id := segID(t, tr)
	tr.SetStructure(id, signalclass.Result{
		Modulation: signalclass.ModFMVoice,
		Params:     signalclass.Params{OccupiedHz: 17_000},
	})
	s := only(t, tr.Signals(nil))
	if s.Emission != EmissionUndetermined {
		t.Fatalf("emission %q", s.Emission)
	}
	if s.Structure == nil || s.Structure.Service != signals.IDAmateur ||
		s.Structure.ServiceLabel != signalclass.LabelTransmitter {
		t.Fatalf("structure %+v", s.Structure)
	}
}

func TestTracker_AmateurDataFromSignature(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+5_000, testCenter, steadyAt(testCenter+100_000))
	id := segID(t, tr)
	tr.SetStructure(id, signalclass.Result{
		Modulation: signalclass.Mod2FSK,
		Params:     signalclass.Params{OccupiedHz: 12_000, Baud: 9600},
	})
	st := only(t, tr.Signals(nil)).Structure
	if st == nil || st.Service != signals.IDAmateurData || st.ServiceLabel != signalclass.LabelTransmitter ||
		st.ServiceBasis != signalclass.BasisSignature {
		t.Fatalf("structure %+v", st)
	}
}

func TestTracker_AmateurVoiceFromSignature(t *testing.T) {
	tr := NewTracker()
	feedBursts(tr, testCenter, testCenter+100_000, 75_000)
	id := segID(t, tr)
	tr.SetStructure(id, signalclass.Result{
		Modulation: signalclass.ModFMVoice,
		Params:     signalclass.Params{OccupiedHz: 12_000},
	})
	s := only(t, tr.Signals(nil))
	if s.Emission != EmissionPulsed || s.Origin != OriginTerrestrial {
		t.Fatalf("emission %q origin %q", s.Emission, s.Origin)
	}
	if s.Structure == nil || s.Structure.Service != signals.IDAmateur ||
		s.Structure.ServiceBasis != signalclass.BasisSignature || s.Structure.ServiceLabel != signalclass.LabelTransmitter {
		t.Fatalf("structure %+v", s.Structure)
	}
}

func TestTracker_ISMBurstFromSignature(t *testing.T) {
	tr := NewTracker()
	const center = 434_000_000
	feedBursts(tr, center, 433_920_000, 75_000)
	id := segID(t, tr)
	tr.SetStructure(id, signalclass.Result{
		Modulation: signalclass.ModOOK,
		Params:     signalclass.Params{OccupiedHz: 8_000, Baud: 2_000},
	})
	st := only(t, tr.Signals(nil)).Structure
	if st == nil || st.Service != signals.IDISM || st.ServiceLabel != "Датчик или брелок" ||
		st.ServiceBasis != signalclass.BasisSignature {
		t.Fatalf("structure %+v", st)
	}
}

func TestTracker_ServiceSurvivesOneMiss(t *testing.T) {
	tr := NewTracker()
	feedBursts(tr, testCenter, testCenter+100_000, 75_000)
	id := segID(t, tr)
	wide := signalclass.Result{Modulation: signalclass.ModFMVoice, Params: signalclass.Params{OccupiedHz: 12_000}}
	narrow := signalclass.Result{Modulation: signalclass.ModFMVoice, Params: signalclass.Params{OccupiedHz: 3_000}}
	tr.SetStructure(id, wide)
	tr.SetStructure(id, wide)
	tr.SetStructure(id, narrow)
	if s := only(t, tr.Signals(nil)).Structure; s == nil || s.Service != signals.IDAmateur {
		t.Fatalf("one miss dropped %+v", s)
	}
	tr.SetStructure(id, narrow)
	if s := only(t, tr.Signals(nil)).Structure; s != nil && s.Service != "" {
		t.Fatalf("two misses kept %q", s.Service)
	}
}

func TestTracker_SatelliteOriginClosesTheLabel(t *testing.T) {
	tr := NewTracker()
	freq := func(ms uint64) float64 { return testCenter + 100_000 - 150*float64(ms-t0)/1000 }
	expected := func(ms uint64) []Expected {
		return []Expected{{NoradID: 53385, UUID: "tx1", FreqHz: freq(ms)}}
	}
	peaks := func(ms uint64) []Peak { return []Peak{carrier(freq(ms), 12_000, 14)} }
	feed(tr, t0, t0+3_000, testCenter, peaks)
	id := segID(t, tr)
	tr.SetStructure(id, signalclass.Result{Modulation: signalclass.ModFMVoice, Params: signalclass.Params{OccupiedHz: 12_000}})
	if s := only(t, tr.Signals(expected(t0+2_000))); s.Origin != OriginSatelliteProbable ||
		s.Structure == nil || s.Structure.ServiceLabel != signalclass.LabelTransmitter+"?" {
		t.Fatalf("probable origin %q structure %+v", s.Origin, s.Structure)
	}
	feed(tr, t0+2_000, t0+20_000, testCenter, peaks)
	tr.SetStructure(id, signalclass.Result{Modulation: signalclass.Mod2FSK, Params: signalclass.Params{OccupiedHz: 12_000, Baud: 9600}})
	tr.SetStructure(id, signalclass.Result{Modulation: signalclass.Mod2FSK, Params: signalclass.Params{OccupiedHz: 12_000, Baud: 9600}})
	s := only(t, tr.Signals(expected(t0+20_000)))
	if s.Origin != OriginSatellite || s.Structure == nil || s.Structure.Service != signalclass.ServiceSatTx ||
		s.Structure.ServiceLabel != signalclass.LabelTransmitter {
		t.Fatalf("origin %q structure %+v", s.Origin, s.Structure)
	}
}

func TestTracker_UnknownOriginDropsSatelliteLabel(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+10_000, testCenter, steadyAt(testCenter+100_000))
	id := segID(t, tr)
	tr.SetStructure(id, signalclass.Result{
		Modulation: signalclass.ModFMVoice, Service: signalclass.ServiceSatVoice, ServiceLabel: signalclass.LabelTransmitter,
	})
	s := only(t, tr.Signals(nil))
	if s.Origin != OriginUnknown {
		t.Fatalf("origin %q", s.Origin)
	}
	if s.Structure != nil && s.Structure.Service != "" {
		t.Fatalf("unknown origin kept %q", s.Structure.Service)
	}
}

func TestTracker_SSTVServiceSticksAfterVIS(t *testing.T) {
	tr := NewTracker()
	feed(tr, t0, t0+uint64(sliceMs)+stepMs, testCenter, steadyAt(testCenter+100_000))
	id := segID(t, tr)
	vis := signalclass.Result{
		Modulation: signalclass.ModFM, Service: "sstv", ServiceLabel: "SSTV, Robot 36", Why: "VIS",
	}
	lines := signalclass.Result{Modulation: signalclass.ModFM, Service: "sstv_probable", ServiceLabel: "SSTV?", Why: "tones"}
	tr.SetStructure(id, vis)
	tr.SetStructure(id, lines)
	tr.SetStructure(id, lines)
	s := only(t, tr.Signals(nil)).Structure
	if s == nil || s.Service != "sstv" || s.ServiceLabel != "SSTV, Robot 36" {
		t.Fatalf("structure %+v, want sticky SSTV Robot 36", s)
	}
}
