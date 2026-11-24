package main

import (
	"path/filepath"
	"testing"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/demod"
	"github.com/art-injener/satellite-scout/internal/sigmf"
)

func TestPathSatHints_PrefersSigMFOverConfig(t *testing.T) {
	dir := t.TempDir()
	meta := filepath.Join(dir, "sat.sigmf-meta")
	if err := sigmf.SaveMeta(meta, sigmf.Meta{
		Global: sigmf.Global{
			Datatype: sigmf.DatatypeCF32LE, SampleRate: 48000, Version: "1.0.0",
			NoradID: 53385, SatName: "GEOSCAN-EDELVEIS",
		},
		Captures: []sigmf.Capture{{Frequency: 436_200_000}},
	}); err != nil {
		t.Fatal(err)
	}
	// .sigmf-data не нужен: LoadMeta читает только meta.
	paths := []config.RadioPath{{
		ID: 1,
		Receiver: config.ReceiverConfig{
			Imitator: &config.ImitatorConfig{
				Replay:  meta,
				NoradID: 25544,
				SatName: "ISS",
			},
		},
	}}
	got := pathSatHints(paths)
	h, ok := got.Hint("radio_path_1")
	if !ok || h.NoradID != 53385 || h.Source != demod.SatSourceSigMF {
		t.Fatalf("got %+v ok=%v", h, ok)
	}
}

func TestPathSatHints_FallsBackToConfigNorad(t *testing.T) {
	paths := []config.RadioPath{{
		ID: 2,
		Receiver: config.ReceiverConfig{
			Imitator: &config.ImitatorConfig{NoradID: 25544, SatName: "ISS"},
		},
	}}
	h, ok := pathSatHints(paths).Hint("radio_path_2")
	if !ok || h.NoradID != 25544 || h.Source != demod.SatSourceReplay || h.SatName != "ISS" {
		t.Fatalf("got %+v ok=%v", h, ok)
	}
}
