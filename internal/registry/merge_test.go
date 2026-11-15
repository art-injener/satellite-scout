package registry

import (
	"fmt"
	"testing"
	"time"
)

var testAt = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func TestMergeTransmitters_PriorityOrderOnNearFrequency(t *testing.T) {
	by := map[string][]Transmitter{
		SourceSatNOGS: {
			{UUID: "s1", DownlinkHz: 437_801_000, Status: "active", Alive: new(true), Source: SourceSatNOGS},
		},
		SourceSonik:        {{UUID: "s1", DownlinkHz: 437_802_000, Status: "active", Source: SourceSonik}},
		SourceAMSAT:        {{DownlinkHz: 437_800_000, Source: SourceAMSAT}},
		SourceHeavensAbove: {{DownlinkHz: 437_803_000, Source: SourceHeavensAbove}},
		SourceCurated:      {{DownlinkHz: 437_800_000, Name: "ISS", Source: SourceCurated}},
	}
	got := mergeTransmitters(by)
	if len(got) != 1 || got[0].Source != SourceCurated {
		t.Fatalf("got %+v, want only curated", got)
	}
}

func TestMergeTransmitters_SameUUIDMatchesBeyondTolerance(t *testing.T) {
	by := map[string][]Transmitter{
		SourceSonik: {{UUID: "u1", DownlinkHz: 437_800_000, Status: "active", Source: SourceSonik}},
		SourceSatNOGS: {
			{UUID: "u1", DownlinkHz: 437_850_000, Status: "active", Alive: new(true), Source: SourceSatNOGS},
		},
	}
	got := mergeTransmitters(by)
	if len(got) != 1 || got[0].Source != SourceSonik {
		t.Fatalf("got %+v, want sonik record only (same uuid)", got)
	}
}

func TestMergeTransmitters_DifferentFrequenciesKept(t *testing.T) {
	by := map[string][]Transmitter{
		SourceCurated: {{DownlinkHz: 145_800_000, Source: SourceCurated}},
		SourceSatNOGS: {
			{UUID: "u2", DownlinkHz: 437_800_000, Status: "active", Alive: new(true), Source: SourceSatNOGS},
		},
	}
	got := mergeTransmitters(by)
	if len(got) != 2 || got[0].Source != SourceCurated || got[1].Source != SourceSatNOGS {
		t.Fatalf("got %+v, want curated then satnogs", got)
	}
}

func TestMergeTransmitters_InactiveAndNoDownlinkSkipped(t *testing.T) {
	by := map[string][]Transmitter{
		SourceSonik: {
			{UUID: "a", DownlinkHz: 437_000_000, Status: "inactive", Source: SourceSonik},
			{UUID: "b", DownlinkHz: 0, Status: "active", Source: SourceSonik},
		},
		SourceSatNOGS: {
			{UUID: "c", DownlinkHz: 145_000_000, Status: "active", Alive: new(false), Source: SourceSatNOGS},
			{UUID: "d", DownlinkHz: 437_000_000, Status: "active", Alive: new(true), Source: SourceSatNOGS},
		},
	}
	got := mergeTransmitters(by)
	if len(got) != 1 || got[0].UUID != "d" {
		t.Fatalf("got %+v, want only active satnogs d (inactive sonik does not hide it)", got)
	}
}

func TestMergeSatellite_NamePriority(t *testing.T) {
	orbit := &Orbit{Name: "ISS (ZARYA)", Source: SourceSpaceTrack}
	sonik := map[string]SatFact{SourceSonik: {Name: "ISS", Source: SourceSonik}}
	curated := map[string][]Transmitter{SourceCurated: {{Name: "МКС", DownlinkHz: 1, Source: SourceCurated}}}

	cases := []struct {
		name       string
		facts      map[string]SatFact
		txs        map[string][]Transmitter
		want, from string
	}{
		{"curated first", sonik, curated, "МКС", SourceCurated},
		{"sonik over space-track", sonik, nil, "ISS", SourceSonik},
		{"space-track last", nil, nil, "ISS (ZARYA)", SourceSpaceTrack},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeSatellite(25544, tc.facts, tc.txs, orbit)
			if got.Name != tc.want || got.NameSource != tc.from {
				t.Fatalf("name = %q from %q, want %q from %q", got.Name, got.NameSource, tc.want, tc.from)
			}
		})
	}
}

func TestMergeSatellite_Status(t *testing.T) {
	decay := time.Date(2024, 5, 12, 0, 0, 0, 0, time.UTC)
	aliveTx := Transmitter{DownlinkHz: 1, Status: "active", Alive: new(true), Source: SourceSatNOGS}
	deadTx := Transmitter{DownlinkHz: 1, Status: "active", Alive: new(false), Source: SourceSatNOGS}

	cases := []struct {
		name       string
		facts      map[string]SatFact
		txs        map[string][]Transmitter
		want, from string
	}{
		{
			"satcat decay over everything",
			map[string]SatFact{SourceSatcat: {Decay: decay}, SourceSonik: {Status: StatusInOrbit}},
			map[string][]Transmitter{SourceSatNOGS: {aliveTx}},
			StatusReEntered, SourceSatcat,
		},
		{
			"sonik re-entered without satcat",
			map[string]SatFact{SourceSonik: {Status: StatusReEntered}},
			map[string][]Transmitter{SourceSatNOGS: {aliveTx}},
			StatusReEntered, SourceSonik,
		},
		{
			"satcat on orbit blocks sonik re-entered",
			map[string]SatFact{SourceSatcat: {}, SourceSonik: {Status: StatusReEntered}},
			nil,
			StatusInOrbit, SourceSatcat,
		},
		{
			"satnogs alive flag does not confirm liveness",
			map[string]SatFact{SourceSonik: {Status: StatusInOrbit}},
			map[string][]Transmitter{SourceSatNOGS: {deadTx, aliveTx}},
			StatusInOrbit, SourceSonik,
		},
		{
			"satnogs alive flag with amsat",
			nil,
			map[string][]Transmitter{SourceSatNOGS: {aliveTx}, SourceAMSAT: {{DownlinkHz: 1}}},
			StatusAlive, SourceAMSAT,
		},
		{
			"satnogs dead over heavens-above",
			nil,
			map[string][]Transmitter{SourceSatNOGS: {deadTx}, SourceHeavensAbove: {{DownlinkHz: 1}}},
			StatusDead, SourceSatNOGS,
		},
		{
			"heavens-above alive over sonik",
			map[string]SatFact{SourceSonik: {Status: StatusFuture}},
			map[string][]Transmitter{SourceHeavensAbove: {{DownlinkHz: 1}}},
			StatusAlive, SourceHeavensAbove,
		},
		{
			"amsat alive",
			nil,
			map[string][]Transmitter{SourceAMSAT: {{DownlinkHz: 1}}},
			StatusAlive, SourceAMSAT,
		},
		{
			"sonik last",
			map[string]SatFact{SourceSonik: {Status: StatusFuture}},
			nil,
			StatusFuture, SourceSonik,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeSatellite(1, tc.facts, tc.txs, nil)
			if got.Status != tc.want || got.StatusSource != tc.from {
				t.Fatalf("status = %q from %q, want %q from %q", got.Status, got.StatusSource, tc.want, tc.from)
			}
		})
	}
}

func TestMergeSatellite_CardFieldsFromSonik(t *testing.T) {
	facts := map[string]SatFact{SourceSonik: {CallSign: "RS0ISS", Operator: "NASA", Launched: "1998-11-20"}}
	got := mergeSatellite(25544, facts, nil, nil)
	if fmt.Sprint(got.CallSign, got.Operator, got.Launched) != fmt.Sprint("RS0ISS", "NASA", "1998-11-20") {
		t.Fatalf("card = %+v", got)
	}
}
