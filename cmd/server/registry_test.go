package main

import (
	"context"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/registry"
	"github.com/art-injener/satellite-scout/internal/satnogs"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

type manualSet map[int]bool

func (m manualSet) Contains(id int) bool { return m[id] }

func TestPassExcluder_ManualOrDecayed(t *testing.T) {
	reg := registry.New()
	err := reg.IngestCatalogPass(context.Background(), tracker.CatalogPass{
		At: time.Now(),
		Satcat: []tracker.SatcatRecord{
			{NoradID: 2012, Decay: time.Date(2024, 5, 12, 0, 0, 0, 0, time.UTC)},
			{NoradID: 25544},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ex := passExcluder{manual: manualSet{40069: true}, reg: reg}

	for id, want := range map[int]bool{2012: true, 40069: true, 25544: false, 99999: false} {
		if got := ex.Contains(id); got != want {
			t.Errorf("Contains(%d) = %v, want %v", id, got, want)
		}
	}
}

func TestPassExcluder_KeepsCatalogWithoutReceivable(t *testing.T) {
	reg := registry.New()
	ctx := context.Background()
	at := time.Now()
	_ = reg.ReplaceTransmitters(ctx, registry.SourceSonik, []registry.Transmitter{
		{NoradID: 2, DownlinkHz: 437_000_000, Status: "active"},
	}, at)
	_ = reg.ReplaceTransmitters(ctx, registry.SourceAMSAT, []registry.Transmitter{
		{NoradID: 1, DownlinkHz: 145_900_000},
	}, at)
	ex := passExcluder{manual: manualSet{}, reg: reg}
	if ex.Contains(1) || ex.Contains(2) {
		t.Fatal("pass excluder must not drop satellites by receivable/liveness labels")
	}
}

func TestGroupLabelFilter_Default(t *testing.T) {
	reg := registry.New()
	ctx := context.Background()
	at := time.Now()
	_ = reg.ReplaceTransmitters(ctx, registry.SourceAMSAT, []registry.Transmitter{
		{NoradID: 1, DownlinkHz: 145_900_000},
	}, at)
	_ = reg.ReplaceTransmitters(ctx, registry.SourceSonik, []registry.Transmitter{
		{NoradID: 2, DownlinkHz: 437_000_000, Status: "active"},
	}, at)
	f := newGroupLabelFilter(reg, registry.Bands{{MinHz: 24_100_000, MaxHz: 1_766_000_000}}, "")
	if !f.Include(1) {
		t.Fatal("alive in-band must be in the default group")
	}
	if f.Include(2) {
		t.Fatal("sonik-only must stay out of the default group once liveness is known")
	}
	f.SetLabel(registry.LabelStations)
	if f.Include(1) {
		t.Fatal("AMSAT voice bird is not a station")
	}
}

func TestBandSource_PrefersAntennaBand(t *testing.T) {
	reg := registry.New()
	_ = reg.ReplaceTransmitters(context.Background(), registry.SourceHeavensAbove, []registry.Transmitter{
		{NoradID: 1, DownlinkHz: 145_800_000},
		{NoradID: 1, DownlinkHz: 437_800_000},
	}, time.Now())
	ant := registry.Bands{{MinHz: 430_000_000, MaxHz: 450_000_000}}
	got := bandSource{
		src: reg, bands: registry.Bands{{MinHz: 24_100_000, MaxHz: 1_766_000_000}}, antenna: ant,
	}.Transmitters(1)
	if len(got) != 2 || got[0].FreqHz != 437_800_000 {
		t.Fatalf("got %+v, want UHF first", got)
	}
}

func TestStationAntennaBands(t *testing.T) {
	min, max := 430.0, 450.0
	paths := []config.RadioPath{
		{ID: 1, Antenna: config.AntennaConfig{FreqMinMHz: &min, FreqMaxMHz: &max}},
		{ID: 2, Antenna: config.AntennaConfig{}},
	}
	got := stationAntennaBands(paths)
	if len(got) != 1 || got[0].MinHz != 430_000_000 || got[0].MaxHz != 450_000_000 {
		t.Fatalf("bands = %+v", got)
	}
}

func TestBandSource_DropsOutOfBand(t *testing.T) {
	reg := registry.New()
	_ = reg.ReplaceTransmitters(context.Background(), registry.SourceHeavensAbove, []registry.Transmitter{
		{NoradID: 1, DownlinkHz: 437_000_000},
		{NoradID: 1, DownlinkHz: 2_275_000_000},
	}, time.Now())

	got := bandSource{src: reg, bands: registry.Bands{{MinHz: 24_100_000, MaxHz: 1_766_000_000}}}.Transmitters(1)
	if len(got) != 1 || got[0].FreqHz != 437_000_000 {
		t.Fatalf("got %+v, want only 437 MHz", got)
	}
	if all := (bandSource{src: reg}).Transmitters(1); len(all) != 2 {
		t.Fatalf("without bands got %d transmitters, want 2", len(all))
	}
}

func TestStationBands(t *testing.T) {
	paths := []config.RadioPath{
		{ID: 1, Receiver: config.ReceiverConfig{Driver: "rtlsdr"}},
		{ID: 2, Receiver: config.ReceiverConfig{Driver: "airspy"}},
	}
	got := stationBands(paths)
	if len(got) != 1 || got[0].MinHz != 24_100_000 || got[0].MaxHz != 1_766_000_000 {
		t.Fatalf("bands = %+v, want RTL-SDR only", got)
	}
	if none := stationBands(nil); len(none) != 0 {
		t.Fatalf("no radio paths: bands = %+v, want none", none)
	}
}

func TestSatnogsSnapshot_SkipsUnknownNorad(t *testing.T) {
	low := int64(437_800_000)
	cache := map[int][]satnogs.Transmitter{25544: {{UUID: "u1", DownlinkLow: &low, Status: "active", Alive: true}}}

	got := satnogsSnapshot([]int{25544, 40069}, func(id int) []satnogs.Transmitter { return cache[id] }, time.Now())

	if len(got) != 1 || len(got[25544]) != 1 || got[25544][0].UUID != "u1" {
		t.Fatalf("got %+v, want only ISS", got)
	}
}
