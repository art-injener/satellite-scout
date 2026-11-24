package main

import (
	"context"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/registry"
)

func TestRegistryTXSource_ListsSortedActive(t *testing.T) {
	reg := registry.New()
	ctx := context.Background()
	at := time.Unix(1, 0).UTC()
	_ = reg.ReplaceTransmitters(ctx, registry.SourceAMSAT, []registry.Transmitter{
		{NoradID: 25544, UUID: "uhf", Name: "ISS FM", DownlinkHz: 437_800_000, Mode: "FM", Status: "active"},
		{NoradID: 25544, UUID: "vhf", Name: "APRS", DownlinkHz: 145_825_000, Mode: "AFSK", Baud: 1200, Status: "active"},
		{NoradID: 25544, UUID: "dead", DownlinkHz: 400_000_000, Mode: "FM", Status: "inactive"},
	}, at)

	src := newRegistryTXSource(reg)
	svc := src.asServices()
	got := svc.GetTransmitters(25544)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 active: %+v", len(got), got)
	}
	if got[0].FreqHz != 145_825_000 || got[0].Modulation != "AFSK 1200" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].FreqHz != 437_800_000 {
		t.Errorf("second = %+v", got[1])
	}
	primary := svc.GetPrimaryTransmitter(25544)
	if primary == nil || primary.UUID != "vhf" || primary.FreqMHz != "145.825" {
		t.Fatalf("primary = %+v", primary)
	}
	svc.RequestFetch([]int{25544}) // no-op, must not panic

	rest := src.asREST()
	all := rest.GetAllTransmitters(25544)
	if len(all) != 2 || !all[0].Alive || all[0].Status != "active" {
		t.Fatalf("REST all = %+v", all)
	}
	sum := rest.GetPrimaryTransmitter(25544)
	if sum == nil || sum.FreqMHz != "145.825" {
		t.Fatalf("REST primary = %+v", sum)
	}
}

func TestRegistryTXSource_Empty(t *testing.T) {
	src := newRegistryTXSource(registry.New())
	if got := src.asServices().GetTransmitters(1); got != nil {
		t.Fatalf("got %+v", got)
	}
	if src.asServices().GetPrimaryTransmitter(1) != nil {
		t.Fatal("primary want nil")
	}
	if newRegistryTXSource(nil) != nil {
		t.Fatal("nil registry")
	}
}
