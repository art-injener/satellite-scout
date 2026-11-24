package bandscan

import "testing"

func TestTracker_RecordRx_AttachesFrameToNearestSegment(t *testing.T) {
	tr := NewTracker()
	hz := float64(testCenter + 100_000)
	feed(tr, t0, t0+3_000, testCenter, steadyAt(hz))
	if !tr.RecordRx(hz, HearNote{
		Kind: "beacon", Summary: "Маяк ТМИ", TimestampMs: t0 + 3_000, CountFrame: true,
		NoradID: 53385, SatName: "GEOSCAN",
	}) {
		t.Fatal("RecordRx missed the segment")
	}
	s := only(t, tr.Signals(nil))
	if s.FramesGood != 1 || s.LastKind != "beacon" || s.LastSummary != "Маяк ТМИ" {
		t.Fatalf("hear fields = %+v", s)
	}
	if s.LastRxMs != t0+3_000 || s.NoradID != 53385 || s.SatName != "GEOSCAN" {
		t.Fatalf("identity/time = norad=%d name=%q rx=%d", s.NoradID, s.SatName, s.LastRxMs)
	}
}

func TestTracker_RecordRx_KeepsLastPayloadForModal(t *testing.T) {
	tr := NewTracker()
	hz := float64(testCenter + 100_000)
	feed(tr, t0, t0+3_000, testCenter, steadyAt(hz))
	if !tr.RecordRx(hz, HearNote{
		Kind: "line", Summary: "Линия", Decoder: "line",
		InfoHex: "aa55cc33deadbeef", TimestampMs: t0 + 3_000, CountFrame: true,
	}) {
		t.Fatal("RecordRx missed the segment")
	}
	s := only(t, tr.Signals(nil))
	if s.LastKind != "line" || s.LastInfoHex != "aa55cc33deadbeef" || s.LastDecoder != "line" {
		t.Fatalf("line payload = %+v", s)
	}

	tr.RecordRx(hz, HearNote{
		Kind: "beacon", Summary: "Маяк ТМИ · RS23S", Decoder: "geoscan.beacon",
		Src: "RS23S", Dest: "BEACON", Info: "U=3.9", InfoHex: "01003e",
		Params:      []HearParam{{ID: "u", Name: "U", Value: "3.9", Unit: "V", Key: true}},
		TimestampMs: t0 + 4_000, CountFrame: true, NoradID: 53385, SatName: "GEOSCAN",
	})
	s = only(t, tr.Signals(nil))
	if s.FramesGood != 2 || s.LastKind != "beacon" || s.LastInfoHex != "01003e" {
		t.Fatalf("beacon overwrite = %+v", s)
	}
	if s.LastSrc != "RS23S" || s.LastDest != "BEACON" || s.LastInfo != "U=3.9" {
		t.Fatalf("addrs/info = src=%q dest=%q info=%q", s.LastSrc, s.LastDest, s.LastInfo)
	}
	if len(s.LastParams) != 1 || s.LastParams[0].Name != "U" || s.LastParams[0].Value != "3.9" {
		t.Fatalf("params = %+v", s.LastParams)
	}
	if len(s.RxHistory) != 2 || s.RxHistory[0].Kind != "line" || s.RxHistory[1].Kind != "beacon" {
		t.Fatalf("rx_history = %+v, want line then beacon", s.RxHistory)
	}
}

func TestTracker_RecordRx_HistoryKeepsLastN(t *testing.T) {
	tr := NewTracker()
	hz := float64(testCenter + 100_000)
	feed(tr, t0, t0+3_000, testCenter, steadyAt(hz))
	for i := range hearHistoryMax + 5 {
		tr.RecordRx(hz, HearNote{
			Kind: "line", InfoHex: "aa", TimestampMs: t0 + uint64(3_000+i), CountFrame: true,
		})
	}
	s := only(t, tr.Signals(nil))
	if len(s.RxHistory) != hearHistoryMax {
		t.Fatalf("rx_history len=%d, want %d", len(s.RxHistory), hearHistoryMax)
	}
	wantFirst := t0 + uint64(3_000+5)
	if s.RxHistory[0].TimestampMs != wantFirst {
		t.Fatalf("oldest kept ts=%d, want %d", s.RxHistory[0].TimestampMs, wantFirst)
	}
}

func TestTracker_RecordRx_PrefersInBandOverNear(t *testing.T) {
	tr := NewTracker()
	a := float64(testCenter + 50_000)
	b := float64(testCenter + 120_000)
	feed(tr, t0, t0+3_000, testCenter, func(uint64) []Peak {
		return []Peak{carrier(a, 20_000, 15), carrier(b, 20_000, 15)}
	})
	tr.RecordRx(a+1_000, HearNote{Kind: "line", TimestampMs: t0 + 3_000, CountFrame: true})
	var gotA, gotB Signal
	for _, s := range tr.Signals(nil) {
		switch {
		case abs(float64(s.CenterHz)-a) < 5_000:
			gotA = s
		case abs(float64(s.CenterHz)-b) < 5_000:
			gotB = s
		}
	}
	if gotA.FramesGood != 1 || gotB.FramesGood != 0 {
		t.Fatalf("in-band A=%d B=%d, want frame on A only", gotA.FramesGood, gotB.FramesGood)
	}
}

func TestTracker_PinnedSatNotEvictedWhenFull(t *testing.T) {
	tr := NewTracker()
	tr.limit = 2
	hz := float64(testCenter + 100_000)
	feed(tr, t0, t0+3_000, testCenter, steadyAt(hz))
	tr.RecordRx(hz, HearNote{
		Kind: "voice", TimestampMs: t0 + 3_000, NoradID: 25544, SatName: "ISS",
	})
	id := only(t, tr.Signals(nil)).ID
	// Новые участки на других частотах заполняют лимит — pin КА не вытесняется.
	for i := 0; i < 5; i++ {
		f := float64(testCenter) + 200_000 + float64(i)*50_000
		feed(tr, t0+uint64(4+i)*1_000, t0+uint64(7+i)*1_000, testCenter, steadyAt(f))
	}
	found := false
	for _, s := range tr.Signals(nil) {
		if s.ID == id && s.LastKind == "voice" && s.NoradID == 25544 {
			found = true
		}
	}
	if !found {
		t.Fatalf("pinned sat evicted; segs=%d signals=%+v", len(tr.segs), tr.Signals(nil))
	}
}

func TestTracker_RecordRx_CreatesSegmentWhenNoMatch(t *testing.T) {
	tr := NewTracker()
	tr.nowMs = t0
	if !tr.RecordRx(436_500_000, HearNote{
		Kind: "beacon", TimestampMs: t0 + 100, CountFrame: true,
		NoradID: 53385, SatName: "GEOSCAN",
	}) {
		t.Fatal("RecordRx should create a hear segment")
	}
	s := only(t, tr.Signals(nil))
	if s.FramesGood != 1 || s.LastRxMs != t0+100 || s.NoradID != 53385 {
		t.Fatalf("created hear row %+v", s)
	}
}

func TestTracker_PinnedSatKeepsIdentityWhenExpectedDoesNotFollow(t *testing.T) {
	tr := NewTracker()
	hz := float64(testCenter + 100_000)
	feed(tr, t0, t0+3_000, testCenter, steadyAt(hz))
	tr.RecordRx(hz, HearNote{
		Kind: "voice", TimestampMs: t0 + 3_000, NoradID: 25544, SatName: "ISS",
	})
	// Риска на частоте есть, но участок «не следует» (частота риска уезжает) — pin сохраняет КА.
	exp := []Expected{{NoradID: 25544, UUID: "iss", FreqHz: hz - 50_000, SatName: "ISS"}}
	s := only(t, tr.Signals(exp))
	if s.NoradID != 25544 || s.SatName != "ISS" || s.LastKind != "voice" {
		t.Fatalf("pinned sat lost identity: %+v", s)
	}
}

func TestTracker_NonSatRxDoesNotPinPastRememberRules(t *testing.T) {
	tr := NewTracker()
	hz := float64(testCenter + 100_000)
	// Короткий всплеск без qualifies: кадр без NORAD не должен оставить строку навечно.
	tr.Update(synthDecision(t0, testCenter, carrier(hz, 2_000, 15)))
	tr.Update(synthDecision(t0+stepMs, testCenter, carrier(hz, 2_000, 15)))
	if !tr.RecordRx(hz, HearNote{Kind: "line", TimestampMs: t0 + stepMs, CountFrame: true}) {
		t.Fatal("expected a segment for the burst")
	}
	// shown=true от RecordRx держит строку; без NORAD pin() ложен.
	for _, seg := range tr.segs {
		if seg.pinned() {
			t.Fatal("line without NORAD must not pin")
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
