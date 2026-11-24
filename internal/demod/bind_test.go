package demod

import (
	"testing"
	"time"
)

type callsignsMap map[string]struct {
	norad int
	name  string
}

func (m callsignsMap) LookupCallsign(call string) (int, string, bool) {
	key := NormalizeCallsign(call)
	e, ok := m[key]
	if !ok {
		return 0, "", false
	}
	return e.norad, e.name, true
}

type namesMap map[int]string

func (m namesMap) SatName(norad int) string { return m[norad] }

type horizonSet map[int]bool

func (h horizonSet) IsVisible(norad int) bool { return h[norad] }

func TestResolveSat_CallsignWins(t *testing.T) {
	in := ResolveInput{
		SrcCall:     "RS23S",
		FrequencyHz: 436_200_000,
		BandwidthHz: 20_000,
		Callsigns: callsignsMap{
			"RS23S": {norad: 53385, name: "GEOSCAN-EDELVEIS"},
		},
		Path: PathSatHint{NoradID: 25544, SatName: "ISS", Source: SatSourceSigMF},
		Transmitters: []ExpectedTransmitter{
			{NoradID: 25544, FreqHz: 436_200_000},
		},
		Doppler: dopplerByNorad{25544: -1},
		Horizon: horizonSet{25544: true},
		Now:     time.Now(),
	}
	b := ResolveSat(in)
	if b.NoradID != 53385 || b.Source != SatSourceCallsign || b.SatName != "GEOSCAN-EDELVEIS" {
		t.Fatalf("got %+v", b)
	}
}

func TestResolveSat_FreqPlanUniqueVisible(t *testing.T) {
	now := time.Now()
	in := ResolveInput{
		FrequencyHz: 436_200_000,
		BandwidthHz: 20_000,
		Now:         now,
		Transmitters: []ExpectedTransmitter{
			{NoradID: 53385, FreqHz: 436_200_000},
			{NoradID: 99999, FreqHz: 145_800_000},
		},
		Doppler: dopplerByNorad{53385: 0, 99999: 0},
		Horizon: horizonSet{53385: true},
		Names:   namesMap{53385: "GEOSCAN-EDELVEIS"},
	}
	b := ResolveSat(in)
	if b.NoradID != 53385 || b.Source != SatSourceFreqPlan || b.SatName != "GEOSCAN-EDELVEIS" {
		t.Fatalf("got %+v", b)
	}
}

func TestResolveSat_FreqPlanSkipsBelowHorizon(t *testing.T) {
	in := ResolveInput{
		FrequencyHz:  436_200_000,
		BandwidthHz:  20_000,
		Now:          time.Now(),
		Transmitters: []ExpectedTransmitter{{NoradID: 53385, FreqHz: 436_200_000}},
		Doppler:      dopplerByNorad{53385: 0},
		Horizon:      horizonSet{53385: false},
		Path:         PathSatHint{NoradID: 53385, SatName: "G", Source: SatSourceSigMF},
	}
	b := ResolveSat(in)
	if b.Source != SatSourceSigMF || b.NoradID != 53385 {
		t.Fatalf("want sigmf fallback, got %+v", b)
	}
}

func TestResolveSat_FreqPlanAmbiguous(t *testing.T) {
	in := ResolveInput{
		FrequencyHz: 436_200_000,
		BandwidthHz: 40_000,
		Now:         time.Now(),
		Transmitters: []ExpectedTransmitter{
			{NoradID: 1, FreqHz: 436_195_000},
			{NoradID: 2, FreqHz: 436_205_000},
		},
		Doppler: dopplerByNorad{1: 0, 2: 0},
		Horizon: horizonSet{1: true, 2: true},
		Path:    PathSatHint{NoradID: 9, Source: SatSourceReplay},
	}
	b := ResolveSat(in)
	if b.NoradID != 9 || b.Source != SatSourceReplay {
		t.Fatalf("ambiguous freq must fall through to path, got %+v", b)
	}
}

func TestResolveSat_SigMFBeforeReplayConfig(t *testing.T) {
	// Path hint already chooses source at wiring; resolver just trusts Path.Source.
	b := ResolveSat(ResolveInput{
		Path: PathSatHint{NoradID: 53385, SatName: "GEOSCAN-EDELVEIS", Source: SatSourceSigMF},
	})
	if b.Source != SatSourceSigMF || b.NoradID != 53385 {
		t.Fatalf("got %+v", b)
	}
}

func TestResolveSat_Empty(t *testing.T) {
	if b := ResolveSat(ResolveInput{}); b.NoradID != 0 || b.Source != "" {
		t.Fatalf("got %+v", b)
	}
}
