package freqplan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWriteReadPlan_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freq_plan.csv")
	want := []Entry{
		{
			NoradID: 25544, Name: "ISS", UUID: "u1", FreqHz: 437_800_000,
			Mode: "FM", Baud: 0, AOS: 1000, LOS: 2000, Source: SourceCurated, UpdatedMs: 50,
		},
		{
			NoradID: 27607, Name: "SO-50", FreqHz: 436_795_000,
			Mode: "FM", AOS: 3000, LOS: 4000, Source: SourceSatNOGS, UpdatedMs: 50,
		},
	}
	if err := WritePlan(path, want); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	got, err := ReadPlan(path)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestReadPlan_MissingFileIsEmpty(t *testing.T) {
	got, err := ReadPlan(filepath.Join(t.TempDir(), "missing.csv"))
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

func TestWritePlan_CreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "freq_plan.csv")
	if err := WritePlan(path, []Entry{{NoradID: 1, FreqHz: 100, Source: SourceCurated}}); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
