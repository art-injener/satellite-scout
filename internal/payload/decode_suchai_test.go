package payload

import (
	"testing"

	"github.com/art-injener/satellite-scout/internal/ax100"
)

func TestDecode_SuchaiBeacon(t *testing.T) {
	raw := ax100.PackSuchaiBeacon(ax100.SuchaiPort3, map[string]uint32{
		"eps_vbatt":     7200,
		"obc_hrs_alive": 50,
	})
	pl := Decode(Frame{Raw: raw, Profile: "gmsk4800_ax100"})
	if pl.Kind != KindBeacon || pl.Decoder != "suchai.beacon" {
		t.Fatalf("kind=%q decoder=%q summary=%q", pl.Kind, pl.Decoder, pl.Summary)
	}
	if pl.Summary != "Маяк ТМИ · SUCHAI-3" {
		t.Fatalf("summary=%q", pl.Summary)
	}
	found := false
	for _, p := range pl.Params {
		if p.ID == "eps_vbatt" && p.Value == "7,20" {
			found = true
		}
	}
	if !found {
		t.Fatalf("params=%v", pl.Params)
	}
}

func TestDecode_CSPStillWorksWhenNotSuchai(t *testing.T) {
	raw := ax100.PackCSP(ax100.CSPHeader{
		Priority: 2, Source: 1, Destination: 10, SrcPort: 14, DstPort: 8,
	}, []byte("hello TM"))
	pl := Decode(Frame{Raw: raw, Profile: "gmsk4800_ax100"})
	if pl.Decoder != "csp" {
		t.Fatalf("decoder=%q, want csp", pl.Decoder)
	}
}
