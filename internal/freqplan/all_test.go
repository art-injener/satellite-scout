package freqplan

import (
	"testing"
)

func TestAll_ReturnsCopyOfEveryNorad(t *testing.T) {
	src := map[int][]Tx{
		25544: {{FreqHz: 437_800_000, Source: SourceCurated}},
		43017: {{FreqHz: 145_960_000, Source: SourceCurated}},
	}
	cases := map[string]interface{ All() map[int][]Tx }{
		"curated":       &Curated{byNorad: src},
		"heavens-above": &HeavensAbove{byNorad: src},
		"amsat":         &AMSAT{byNorad: src},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			got := s.All()
			if len(got) != 2 || len(got[25544]) != 1 || got[43017][0].FreqHz != 145_960_000 {
				t.Fatalf("All() = %v, want both NORAD", got)
			}
			got[25544][0].FreqHz = 1
			if src[25544][0].FreqHz != 437_800_000 {
				t.Fatal("All() must return a copy, source was modified")
			}
		})
	}
}

func TestAll_NilSourceIsEmpty(t *testing.T) {
	var c *Curated
	var h *HeavensAbove
	var a *AMSAT
	if len(c.All()) != 0 || len(h.All()) != 0 || len(a.All()) != 0 {
		t.Fatal("nil sources must return empty maps")
	}
}
