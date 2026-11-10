package freqplan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAndSave_CuratedOnly(t *testing.T) {
	dir := t.TempDir()
	curatedPath := filepath.Join(dir, "curated.json")
	planPath := filepath.Join(dir, "freq_plan.csv")
	body := `{"transmitters":[{"norad_id":27607,"name":"SO-50","freq_hz":436795000,"mode":"FM"}]}`
	if err := os.WriteFile(curatedPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	curated, err := LoadCurated(curatedPath)
	if err != nil {
		t.Fatal(err)
	}
	passes := []Pass{{NoradID: 27607, SatName: "SO-50", AOS: 1, LOS: 2}}
	entries := Merge(passes, []Source{curated})
	if err := WritePlan(planPath, entries); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FreqHz != 436_795_000 || got[0].Source != SourceCurated {
		t.Fatalf("got %+v", got)
	}
	if got[0].UpdatedMs == 0 {
		t.Fatal("WritePlan must stamp updated_ms")
	}
}
