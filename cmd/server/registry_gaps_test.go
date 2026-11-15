package main

import (
	"context"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/registry"
)

type fetchSpy struct {
	ids []int
}

func (s *fetchSpy) RequestFetch(ids []int) {
	s.ids = append(s.ids, ids...)
}

func TestRequestSatNOGSGaps_OnlyUncovered(t *testing.T) {
	reg := registry.New()
	_ = reg.ReplaceTransmitters(context.Background(), registry.SourceSonik, []registry.Transmitter{
		{NoradID: 25544, DownlinkHz: 145_800_000},
		{NoradID: 57166, DownlinkHz: 137_900_000},
	}, time.Now())

	spy := &fetchSpy{}
	requestSatNOGSGaps(reg, spy, []int{25544, 57166, 40069})
	if len(spy.ids) != 1 || spy.ids[0] != 40069 {
		t.Fatalf("RequestFetch = %v, want [40069]", spy.ids)
	}
}

func TestRequestSatNOGSGaps_NilFetcherOrNoGaps(t *testing.T) {
	reg := registry.New()
	_ = reg.ReplaceTransmitters(context.Background(), registry.SourceAMSAT, []registry.Transmitter{
		{NoradID: 1, DownlinkHz: 145_900_000},
	}, time.Now())

	requestSatNOGSGaps(reg, nil, []int{1})
	spy := &fetchSpy{}
	requestSatNOGSGaps(reg, spy, []int{1})
	if len(spy.ids) != 0 {
		t.Fatalf("RequestFetch = %v, want none", spy.ids)
	}
}
