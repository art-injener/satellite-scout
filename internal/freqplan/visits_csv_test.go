package freqplan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAppendReadVisits_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freq_visits.csv")
	first := []Visit{{
		RadioPath: "rp1", CenterHz: 436_500_000, SpanHz: 1_600_000,
		WindowStartMs: 10, WindowEndMs: 70,
		NoradID: 27607, Name: "SO-50", UUID: "u", FreqHz: 436_795_000,
		Energy: true, Origin: "satellite", Emission: "pulsed", Received: true, Source: SourceCurated,
	}}
	if err := AppendVisits(path, first); err != nil {
		t.Fatal(err)
	}
	second := []Visit{{
		RadioPath: "rp1", CenterHz: 145_900_000, SpanHz: 1_600_000,
		WindowStartMs: 70, WindowEndMs: 130,
		NoradID: 43017, Name: "AO-91", FreqHz: 145_960_000,
		Energy: false, Received: false, Source: SourceCurated,
	}}
	if err := AppendVisits(path, second); err != nil {
		t.Fatal(err)
	}
	got, err := ReadVisits(path)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]Visit{}, first...), second...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAppendVisits_EmptyIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freq_visits.csv")
	if err := AppendVisits(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty append must not create file, err=%v", err)
	}
}

func TestReadVisits_MissingFileIsEmpty(t *testing.T) {
	got, err := ReadVisits(filepath.Join(t.TempDir(), "missing.csv"))
	if err != nil || got != nil {
		t.Fatalf("got %+v err %v", got, err)
	}
}
