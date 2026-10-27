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
