package payload

import (
	"testing"

	"github.com/art-injener/satellite-scout/internal/ax100"
)

func TestDecode_CSPOnAX100Profile(t *testing.T) {
	raw := ax100.PackCSP(ax100.CSPHeader{
		Priority: 2, Source: 1, Destination: 10, SrcPort: 14, DstPort: 8,
	}, []byte("hello TM"))
	pl := Decode(Frame{Raw: raw, Profile: "gmsk4800_ax100"})
	if pl.Decoder != "csp" || pl.Kind != KindText {
		t.Fatalf("got kind=%q decoder=%q summary=%q", pl.Kind, pl.Decoder, pl.Summary)
	}
	if pl.Src != "1:14" || pl.Dest != "10:8" {
		t.Fatalf("src/dest %q/%q", pl.Src, pl.Dest)
	}
	if pl.Info != "hello TM" {
		t.Fatalf("info %q", pl.Info)
	}
}
