package sigmf

import (
	"path/filepath"
	"testing"
)

func TestLoadMeta_CF32Fixture(t *testing.T) {
	m, err := LoadMeta(filepath.Join("testdata", "tone_cf32.sigmf-meta"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Global.Datatype != DatatypeCF32LE {
		t.Errorf("datatype=%q", m.Global.Datatype)
	}
	if m.SampleRate() != 32000 {
		t.Errorf("rate=%d", m.SampleRate())
	}
	if m.CenterHz() != 436_666_000 {
		t.Errorf("center=%d", m.CenterHz())
	}
}

func TestLoadMeta_CI16Fixture(t *testing.T) {
	m, err := LoadMeta(filepath.Join("testdata", "tone_ci16.sigmf-meta"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Global.Datatype != DatatypeCI16LE {
		t.Errorf("datatype=%q", m.Global.Datatype)
	}
	if m.SampleRate() != 40000 {
		t.Errorf("rate=%d", m.SampleRate())
	}
}

func TestLoadMeta_UnsupportedDatatype(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.sigmf-meta")
	if err := writeFile(path, `{"global":{"core:datatype":"ci8","core:sample_rate":48000},"captures":[{"core:frequency":1e8}]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMeta(path); err == nil {
		t.Fatal("want error for ci8")
	}
}

func TestDataPath(t *testing.T) {
	got := dataPath("/tmp/foo.sigmf-meta")
	if got != "/tmp/foo.sigmf-data" {
		t.Errorf("got %q", got)
	}
}

func TestLoadMeta_ScoutExtensions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sat.sigmf-meta")
	body := `{
  "global": {
    "core:datatype": "cf32_le",
    "core:sample_rate": 48000,
    "core:version": "1.0.0",
    "scout:norad_id": 53385,
    "scout:sat_name": "GEOSCAN-EDELVEIS",
    "scout:radio_path": "radio_path_1",
    "scout:tle_line1": "1 53385U 22096E   22328.00000000  .00000000  00000-0  00000-0 0  9990",
    "scout:tle_line2": "2 53385  97.5000 100.0000 0010000  90.0000 270.0000 15.20000000000000"
  },
  "captures": [{"core:sample_start": 0, "core:frequency": 436200000}]
}`
	if err := writeFile(path, body); err != nil {
		t.Fatal(err)
	}
	m, err := LoadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if !m.HasScoutSat() || m.Global.NoradID != 53385 {
		t.Fatalf("norad=%d", m.Global.NoradID)
	}
	if m.Global.SatName != "GEOSCAN-EDELVEIS" || m.Global.RadioPath != "radio_path_1" {
		t.Errorf("name/path = %q / %q", m.Global.SatName, m.Global.RadioPath)
	}
	if m.Global.TLELine1 == "" || m.Global.TLELine2 == "" {
		t.Error("expected TLE lines")
	}
}

func TestSaveMeta_RoundTripScout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sigmf-meta")
	want := Meta{
		Global: Global{
			Datatype: DatatypeCF32LE, SampleRate: 32000, Version: "1.0.0",
			NoradID: 25544, SatName: "ISS", RadioPath: "radio_path_0",
			TLELine1: "1 25544U", TLELine2: "2 25544  51.6",
		},
		Captures: []Capture{{Frequency: 145_800_000}},
	}
	if err := SaveMeta(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Global.NoradID != 25544 || got.Global.SatName != "ISS" {
		t.Errorf("got %+v", got.Global)
	}
}
