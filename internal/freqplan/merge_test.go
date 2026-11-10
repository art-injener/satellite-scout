package freqplan

import (
	"reflect"
	"testing"
)

func TestMerge_CuratedWinsOverLaterSource(t *testing.T) {
	passes := []Pass{{NoradID: 25544, SatName: "ISS", AOS: 1000, LOS: 2000}}
	curated := sourceMap{25544: {{
		Name: "ISS voice", FreqHz: 437_800_000, Mode: "FM", Source: SourceCurated,
	}}}
	satnogs := sourceMap{25544: {{
		UUID: "satnogs-uuid", Name: "ISS", FreqHz: 437_800_000, Mode: "FM", Source: SourceSatNOGS,
	}}}

	got := Merge(passes, []Source{curated, satnogs})
	want := []Entry{{
		NoradID: 25544, Name: "ISS voice", FreqHz: 437_800_000, Mode: "FM",
		AOS: 1000, LOS: 2000, Source: SourceCurated,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMerge_KeepsDistinctFrequencies(t *testing.T) {
	passes := []Pass{{NoradID: 25544, SatName: "ISS", AOS: 1000, LOS: 2000}}
	curated := sourceMap{25544: {{
		Name: "ISS SSTV", FreqHz: 145_800_000, Mode: "SSTV", Source: SourceCurated,
	}}}
	satnogs := sourceMap{25544: {{
		UUID: "v", Name: "ISS voice", FreqHz: 437_800_000, Mode: "FM", Source: SourceSatNOGS,
	}}}

	got := Merge(passes, []Source{curated, satnogs})
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[0].FreqHz != 145_800_000 || got[0].Source != SourceCurated {
		t.Errorf("first = %+v, want curated SSTV", got[0])
	}
	if got[1].FreqHz != 437_800_000 || got[1].Source != SourceSatNOGS || got[1].UUID != "v" {
		t.Errorf("second = %+v, want satnogs voice", got[1])
	}
}

func TestMerge_NearFrequencyIsSameChannel(t *testing.T) {
	passes := []Pass{{NoradID: 1, SatName: "X", AOS: 1, LOS: 2}}
	curated := sourceMap{1: {{FreqHz: 436_795_000, Mode: "FM", Source: SourceCurated}}}
	other := sourceMap{1: {{FreqHz: 436_796_500, Mode: "FM", Source: SourceSatNOGS, UUID: "dup"}}}

	got := Merge(passes, []Source{curated, other})
	if len(got) != 1 || got[0].Source != SourceCurated || got[0].FreqHz != 436_795_000 {
		t.Fatalf("got %+v, want single curated entry", got)
	}
}

func TestMerge_UsesPassNameWhenTxNameEmpty(t *testing.T) {
	passes := []Pass{{NoradID: 27607, SatName: "SO-50", AOS: 10, LOS: 20}}
	src := sourceMap{27607: {{FreqHz: 436_795_000, Mode: "FM", Source: SourceSatNOGS}}}

	got := Merge(passes, []Source{src})
	if len(got) != 1 || got[0].Name != "SO-50" {
		t.Fatalf("got %+v, want name SO-50", got)
	}
}

func TestMerge_OneRowPerPass(t *testing.T) {
	passes := []Pass{
		{NoradID: 27607, SatName: "SO-50", AOS: 10, LOS: 20},
		{NoradID: 27607, SatName: "SO-50", AOS: 100, LOS: 200},
	}
	src := sourceMap{27607: {{FreqHz: 436_795_000, Mode: "FM", Source: SourceCurated}}}

	got := Merge(passes, []Source{src})
	if len(got) != 2 || got[0].AOS != 10 || got[1].AOS != 100 {
		t.Fatalf("got %+v, want two pass rows", got)
	}
}

func TestMerge_SkipsPassWithoutTransmitters(t *testing.T) {
	passes := []Pass{{NoradID: 1, SatName: "X", AOS: 1, LOS: 2}}
	got := Merge(passes, []Source{sourceMap{}})
	if got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

// sourceMap — мок Source для тестов слияния.
type sourceMap map[int][]Tx

func (s sourceMap) Transmitters(noradID int) []Tx { return s[noradID] }
