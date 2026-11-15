package registry

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/satnogs"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// testTLE собирает корректный TLE МКС с заданными именем и эпохой (YYDDD.DDDDDDDD).
func testTLE(t *testing.T, name, epoch string) *tracker.TLE {
	t.Helper()
	l1 := withChecksum(fmt.Sprintf("1 25544U 98067A   %s  .00016717  00000-0  10270-3 0  999", epoch))
	l2 := withChecksum("2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.4981557142340")
	tle, err := tracker.ParseTLE([]string{name, l1, l2})
	if err != nil {
		t.Fatalf("ParseTLE: %v", err)
	}
	return tle
}

// withChecksum дописывает контрольную цифру строки TLE.
func withChecksum(line string) string {
	sum := 0
	for _, c := range line {
		switch {
		case c >= '0' && c <= '9':
			sum += int(c - '0')
		case c == '-':
			sum++
		}
	}
	return line + strconv.Itoa(sum%10)
}

func TestRegistry_ReplaceTransmittersDropsOldSnapshot(t *testing.T) {
	r := New()
	ctx := context.Background()
	if err := r.ReplaceTransmitters(ctx, SourceHeavensAbove, []Transmitter{
		{NoradID: 1, DownlinkHz: 145_000_000}, {NoradID: 2, DownlinkHz: 437_000_000},
	}, testAt); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceTransmitters(
		ctx,
		SourceHeavensAbove,
		[]Transmitter{{NoradID: 2, DownlinkHz: 436_000_000}},
		testAt,
	); err != nil {
		t.Fatal(err)
	}
	if got := r.Transmitters(1); len(got) != 0 {
		t.Fatalf("NORAD 1 = %v, want empty after new snapshot", got)
	}
	if got := r.Transmitters(2); len(got) != 1 || got[0].FreqHz != 436_000_000 || got[0].Source != SourceHeavensAbove {
		t.Fatalf("NORAD 2 = %v, want new frequency", got)
	}
}

func TestRegistry_ReplaceTransmittersByNoradTouchesOnlyListed(t *testing.T) {
	r := New()
	ctx := context.Background()
	active := Transmitter{Status: "active", Alive: new(true)}
	tx := func(norad int, hz int64) Transmitter { t := active; t.NoradID, t.DownlinkHz = norad, hz; return t }
	if err := r.ReplaceTransmittersByNorad(ctx, SourceSatNOGS, map[int][]Transmitter{
		1: {tx(1, 145_000_000)}, 2: {tx(2, 437_000_000)},
	}, testAt); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceTransmittersByNorad(ctx, SourceSatNOGS, map[int][]Transmitter{2: nil}, testAt); err != nil {
		t.Fatal(err)
	}
	if len(r.Transmitters(1)) != 1 {
		t.Fatal("NORAD 1 must stay")
	}
	if len(r.Transmitters(2)) != 0 {
		t.Fatal("NORAD 2 must be cleared")
	}
}

func TestRegistry_TransmittersAsFreqplanSource(t *testing.T) {
	r := New()
	ctx := context.Background()
	_ = r.ReplaceTransmitters(ctx, SourceCurated, []Transmitter{
		{NoradID: 25544, Name: "ISS", DownlinkHz: 437_800_000, Mode: "FM"},
	}, testAt)
	_ = r.ReplaceTransmitters(ctx, SourceSonik, []Transmitter{
		{NoradID: 25544, UUID: "u1", Name: "APRS", DownlinkHz: 145_825_000, Mode: "AFSK", Baud: 1200, Status: "active"},
		{NoradID: 25544, UUID: "u2", DownlinkHz: 437_801_000, Mode: "FM", Status: "active"},
	}, testAt)

	var src freqplan.Source = r
	got := src.Transmitters(25544)
	want := []freqplan.Tx{
		{Name: "ISS", FreqHz: 437_800_000, Mode: "FM", Source: SourceCurated},
		{UUID: "u1", Name: "APRS", FreqHz: 145_825_000, Mode: "AFSK", Baud: 1200, Source: SourceSonik},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestRegistry_IngestCatalogPass_PartialSatcatIsKept(t *testing.T) {
	r := New()
	decay := time.Date(2024, 5, 12, 0, 0, 0, 0, time.UTC)
	err := r.IngestCatalogPass(context.Background(), tracker.CatalogPass{
		At:        testAt,
		Satcat:    []tracker.SatcatRecord{{NoradID: 2012, Decay: decay}, {NoradID: 25544}},
		SatcatErr: errors.New("space-track: satcat timeout"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Decayed(2012) || r.Decayed(25544) {
		t.Fatalf("decay: 2012=%v 25544=%v", r.Decayed(2012), r.Decayed(25544))
	}
	l := r.Loads()[SourceSatcat]
	if l.Err == "" || !l.LastOK.IsZero() {
		t.Fatalf("load = %+v, want failure without a successful stamp", l)
	}
}

func TestRegistry_PutOrbits_SameEpochPrefersSpaceTrack(t *testing.T) {
	r := New()
	ctx := context.Background()
	tle := testTLE(t, "ISS (ZARYA)", "26316.50000000")
	if err := r.PutOrbits(ctx, tracker.TLESourceCache, []*tracker.TLE{tle}, testAt); err != nil {
		t.Fatal(err)
	}
	if err := r.PutOrbits(ctx, SourceSpaceTrack, []*tracker.TLE{tle}, testAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	o, ok := r.Orbit(25544)
	if !ok || o.Source != SourceSpaceTrack {
		t.Fatalf("orbit = %+v, want space-track over cache at the same epoch", o)
	}
}

func TestRegistry_PutOrbitsKeepsFresherEpoch(t *testing.T) {
	r := New()
	ctx := context.Background()
	fresh := testTLE(t, "ISS (ZARYA)", "26316.50000000")
	old := testTLE(t, "ISS OLD", "24001.50000000")

	if err := r.PutOrbits(ctx, SourceSpaceTrack, []*tracker.TLE{fresh}, testAt); err != nil {
		t.Fatal(err)
	}
	if err := r.PutOrbits(ctx, tracker.TLESourceCelestrak, []*tracker.TLE{old}, testAt); err != nil {
		t.Fatal(err)
	}
	o, ok := r.Orbit(25544)
	if !ok || o.Source != SourceSpaceTrack || o.Line1 != fresh.Line1 || o.Name != "ISS (ZARYA)" {
		t.Fatalf("orbit = %+v, want fresh Space-Track element", o)
	}
}

func TestRegistry_IngestCatalogPass(t *testing.T) {
	r := New()
	decay := time.Date(2024, 5, 12, 0, 0, 0, 0, time.UTC)
	pass := tracker.CatalogPass{
		At:   testAt,
		TLEs: []*tracker.TLE{testTLE(t, "ISS (ZARYA)", "26316.50000000")},
		Sonik: []tracker.SonikSat{
			{NoradID: 25544, Name: "ISS", Status: StatusInOrbit, CallSign: "RS0ISS"},
			{NoradID: 2012, Name: "OPS 1184", Status: StatusReEntered},
			{NoradID: 40069, Name: "METEOR-M2", Status: StatusReEntered},
		},
		Satcat: []tracker.SatcatRecord{{NoradID: 25544}, {NoradID: 40069}, {NoradID: 33591, Decay: decay}},
	}
	if err := r.IngestCatalogPass(context.Background(), pass); err != nil {
		t.Fatalf("IngestCatalogPass: %v", err)
	}

	iss, ok := r.Satellite(25544)
	if !ok || iss.Name != "ISS" || iss.CallSign != "RS0ISS" || iss.Status != StatusInOrbit {
		t.Fatalf("ISS = %+v", iss)
	}
	if _, hasOrbit := r.Orbit(25544); !hasOrbit {
		t.Fatal("ISS orbit missing")
	}
	if !r.Decayed(2012) {
		t.Fatal("2012 re-entered by sonik must be decayed")
	}
	if r.Decayed(40069) {
		t.Fatal("40069: SATCAT on orbit wins over sonik re-entered")
	}
	if s, _ := r.Satellite(33591); !r.Decayed(33591) || !s.Decay.Equal(decay) {
		t.Fatalf("33591 = %+v, want decayed by SATCAT on %v", s, decay)
	}
	if r.Decayed(25544) {
		t.Fatal("ISS must not be decayed")
	}
	loads := r.Loads()
	for _, src := range []string{SourceSpaceTrack, SourceSonik, SourceSatcat} {
		if l := loads[src]; !l.LastOK.Equal(testAt) || l.Err != "" {
			t.Fatalf("load %s = %+v, want ok at %v", src, l, testAt)
		}
	}
}

func TestRegistry_IngestCatalogPass_GPErrorRecordsFailureOnly(t *testing.T) {
	r := New()
	ctx := context.Background()
	_ = r.IngestCatalogPass(ctx, tracker.CatalogPass{
		At:     testAt,
		TLEs:   []*tracker.TLE{testTLE(t, "ISS (ZARYA)", "26316.50000000")},
		Satcat: []tracker.SatcatRecord{{NoradID: 2012, Decay: testAt}},
	})
	later := testAt.Add(time.Hour)
	err := r.IngestCatalogPass(ctx, tracker.CatalogPass{At: later, Err: errors.New("space-track: down")})
	if err != nil {
		t.Fatalf("IngestCatalogPass: %v", err)
	}
	l := r.Loads()[SourceSpaceTrack]
	if !l.LastFail.Equal(later) || l.Err != "space-track: down" || !l.LastOK.Equal(testAt) {
		t.Fatalf("load = %+v, want failure at %v and previous ok kept", l, later)
	}
	if !r.Decayed(2012) {
		t.Fatal("SATCAT facts from the previous pass must stay")
	}
}

func TestRegistry_FromSatnogs(t *testing.T) {
	low, up := int64(437_800_000), int64(145_990_000)
	baud := 9600.0
	got := FromSatnogs(25544, []satnogs.Transmitter{{
		UUID: "u1", Description: "BPSK", DownlinkLow: &low, UplinkLow: &up, Mode: "BPSK", Baud: &baud,
		Status: "active", Alive: true, NoradCatID: 25544,
	}}, testAt)
	want := Transmitter{
		UUID: "u1", NoradID: 25544, Name: "BPSK", DownlinkHz: low, UplinkHz: up, Mode: "BPSK", Baud: 9600,
		Status: "active", Alive: new(true), Source: SourceSatNOGS, FetchedAt: testAt,
	}
	if len(got) != 1 || !got[0].sameRecord(want) || !got[0].FetchedAt.Equal(testAt) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Реестр как единственный источник даёт тот же план, что прежний список источников.
func TestRegistry_PlanMatchesPreviousSourceOrder(t *testing.T) {
	curated := map[int][]freqplan.Tx{25544: {{Name: "ISS", FreqHz: 437_800_000, Mode: "FM", Source: SourceCurated}}}
	ha := map[int][]freqplan.Tx{
		25544: {{FreqHz: 437_802_000, Source: SourceHeavensAbove}, {FreqHz: 145_800_000, Source: SourceHeavensAbove}},
		43017: {{FreqHz: 145_960_000, Source: SourceHeavensAbove}},
	}
	amsat := map[int][]freqplan.Tx{
		43017: {{FreqHz: 145_961_000, Source: SourceAMSAT}, {FreqHz: 435_350_000, Source: SourceAMSAT}},
	}
	low1, low2 := int64(145_825_000), int64(435_350_500)
	satnogsTx := map[int][]satnogs.Transmitter{
		25544: {{UUID: "a", DownlinkLow: &low1, Mode: "AFSK", Status: "active", Alive: true}},
		43017: {{UUID: "b", DownlinkLow: &low2, Mode: "FM", Status: "active", Alive: true}},
	}

	byMap := func(m map[int][]freqplan.Tx) freqplan.Source {
		return freqplan.ProviderFunc(func(id int) []freqplan.Tx { return m[id] })
	}
	oldSources := []freqplan.Source{
		byMap(curated),
		byMap(ha),
		byMap(amsat),
		freqplan.ProviderFunc(func(id int) []freqplan.Tx {
			active := satnogs.ActiveDownlinks(satnogsTx[id])
			out := make([]freqplan.Tx, 0, len(active))
			for _, s := range active {
				out = append(
					out,
					freqplan.Tx{
						UUID:   s.UUID,
						Name:   s.Description,
						FreqHz: s.FreqHz,
						Mode:   s.Mode,
						Baud:   s.Baud,
						Source: SourceSatNOGS,
					},
				)
			}
			return out
		}),
	}

	r := New()
	ctx := context.Background()
	_ = r.ReplaceTransmitters(ctx, SourceCurated, FromFreqplan(curated, testAt), testAt)
	_ = r.ReplaceTransmitters(ctx, SourceHeavensAbove, FromFreqplan(ha, testAt), testAt)
	_ = r.ReplaceTransmitters(ctx, SourceAMSAT, FromFreqplan(amsat, testAt), testAt)
	byNorad := map[int][]Transmitter{}
	for id, list := range satnogsTx {
		byNorad[id] = FromSatnogs(id, list, testAt)
	}
	_ = r.ReplaceTransmittersByNorad(ctx, SourceSatNOGS, byNorad, testAt)

	passes := []freqplan.Pass{
		{NoradID: 25544, SatName: "ISS", AOS: 1000, LOS: 2000},
		{NoradID: 43017, SatName: "AO-91", AOS: 1500, LOS: 2500},
	}
	want := freqplan.Merge(passes, oldSources)
	got := freqplan.Merge(passes, []freqplan.Source{r})
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("plan differs\n got %+v\nwant %+v", got, want)
	}
	if len(want) != 5 {
		t.Fatalf("fixture plan has %d rows, want 5", len(want))
	}
}
