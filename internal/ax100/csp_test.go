package ax100

import (
	"bytes"
	"testing"
)

func TestCSP_RoundTrip(t *testing.T) {
	h := CSPHeader{Priority: 2, Source: 1, Destination: 10, DstPort: 8, SrcPort: 14, CRC: true}
	body := []byte("hello")
	raw := PackCSP(h, body)
	got, data, ok := ParseCSP(raw)
	if !ok {
		t.Fatal("parse failed")
	}
	if got != h {
		t.Fatalf("header %+v, want %+v", got, h)
	}
	if !bytes.Equal(data, body) {
		t.Fatalf("body %q", data)
	}
	if FormatCSP(got) != "CSP 1:14 → 10:8" {
		t.Fatalf("format %q", FormatCSP(got))
	}
}
