package freqplan

import (
	"strings"
	"testing"
)

const amsatSample = `name,norad_id,uplink,downlink,beacon,mode,callsign,status,satnogs_id
SO-50,27607,145.850,436.795,,FM,,active,IRES-1
AO-91,43017,435.250,145.960,145.960,FM DUV,,active,PMAW-1
RS-44,44909,145.935-145.995,435.670-435.610,435.605,SSB CW*,RS44,operational,UFYD-1
DEAD,1,,437.000,,FM,,re-entered,X
MULTI,61781,,435.400/436.210/10460.000,,FM,,active,REOO-1
`

func TestParseAMSAT_ActiveDownlinksAndBeacon(t *testing.T) {
	src, err := ParseAMSAT(strings.NewReader(amsatSample))
	if err != nil {
		t.Fatal(err)
	}
	so50 := src.Transmitters(27607)
	if len(so50) != 1 || so50[0].FreqHz != 436_795_000 || so50[0].Source != SourceAMSAT {
		t.Fatalf("SO-50 = %+v", so50)
	}
	ao91 := src.Transmitters(43017)
	// downlink и beacon на одной частоте — один канал
	if len(ao91) != 1 || ao91[0].FreqHz != 145_960_000 {
		t.Fatalf("AO-91 = %+v", ao91)
	}
	rs44 := src.Transmitters(44909)
	// середина транспондера + маяк
	if len(rs44) != 2 {
		t.Fatalf("RS-44 = %+v, want mid + beacon", rs44)
	}
	if rs44[0].FreqHz != 435_640_000 && rs44[1].FreqHz != 435_640_000 {
		t.Errorf("RS-44 missing midpoint 435.640: %+v", rs44)
	}
	if rs44[0].FreqHz != 435_605_000 && rs44[1].FreqHz != 435_605_000 {
		t.Errorf("RS-44 missing beacon 435.605: %+v", rs44)
	}
	if src.Transmitters(1) != nil {
		t.Fatal("re-entered must be skipped")
	}
}

func TestParseAMSAT_SplitsSlashList(t *testing.T) {
	src, err := ParseAMSAT(strings.NewReader(amsatSample))
	if err != nil {
		t.Fatal(err)
	}
	got := src.Transmitters(61781)
	want := map[int64]bool{435_400_000: true, 436_210_000: true, 10_460_000_000: true}
	if len(got) != 3 {
		t.Fatalf("got %+v, want 3 freqs", got)
	}
	for _, tx := range got {
		if !want[tx.FreqHz] {
			t.Errorf("unexpected %d", tx.FreqHz)
		}
	}
}

func TestParseAMSATFreqField(t *testing.T) {
	cases := []struct {
		in   string
		want []int64
	}{
		{"436.795", []int64{436_795_000}},
		{"435.670-435.610", []int64{435_640_000}},
		{"145.925-145.975", []int64{145_950_000}},
		{"435.400/436.210", []int64{435_400_000, 436_210_000}},
		{"", nil},
		{"  ", nil},
	}
	for _, tc := range cases {
		got := parseAMSATFreqField(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("%q → %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q[%d] = %d, want %d", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestMerge_AMSATFillsGapsAfterSatNOGS(t *testing.T) {
	passes := []Pass{{NoradID: 27607, SatName: "SO-50", AOS: 1, LOS: 2}}
	satnogs := sourceMap{27607: {{FreqHz: 436_795_000, Mode: "FM", Source: SourceSatNOGS}}}
	amsat := sourceMap{27607: {
		{FreqHz: 436_795_000, Mode: "FM", Source: SourceAMSAT},
		{FreqHz: 145_850_000, Mode: "FM", Source: SourceAMSAT}, // uplink как ошибочный downlink — всё же в CSV иногда
	}}
	// В тесте второй — «дыра», которой нет в SatNOGS.
	got := Merge(passes, []Source{satnogs, amsat})
	if len(got) != 2 {
		t.Fatalf("got %+v, want satnogs + extra amsat", got)
	}
	// Сортировка по частоте: дыра AMSAT ниже, совпадающий канал — SatNOGS.
	if got[0].Source != SourceAMSAT || got[0].FreqHz != 145_850_000 {
		t.Errorf("lower = %+v", got[0])
	}
	if got[1].Source != SourceSatNOGS || got[1].FreqHz != 436_795_000 {
		t.Errorf("shared = %+v", got[1])
	}
}
