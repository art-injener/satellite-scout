package payload

import (
	"encoding/binary"
	"testing"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/usp"
)

func TestDecode_USPBeacon(t *testing.T) {
	body := make([]byte, usp.BeaconGeneralLen)
	binary.LittleEndian.PutUint16(body[34:], uint16(int16(7500))) // Uab
	body[48] = 42                                                 // RSSI
	binary.LittleEndian.PutUint32(body[58:], 1000)                // uptime
	info := packUSP(usp.TypeBeacon, body)
	frame := ax25.Frame{Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "RS32S"}, Info: info}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	pl := Decode(Frame{Raw: raw, Profile: "gmsk4800_usp"})
	if pl.Kind != KindBeacon || pl.Decoder != "usp.beacon" {
		t.Fatalf("kind=%q decoder=%q summary=%q", pl.Kind, pl.Decoder, pl.Summary)
	}
	if pl.Src != "RS32S" {
		t.Fatalf("src=%q", pl.Src)
	}
	if len(pl.Params) < 5 {
		t.Fatalf("params=%d", len(pl.Params))
	}
	found := false
	for _, p := range pl.Params {
		if p.ID == "uab" && p.Value == "7,5" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing uab in %v", pl.Params)
	}
}

func TestDecode_USPTextFallsThrough(t *testing.T) {
	frame := ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "RS0SIM"},
		Info: []byte("not a beacon msg"),
	}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	pl := Decode(Frame{Raw: raw, Profile: "gmsk9600_usp"})
	// Маяк не подтвердился; строгий AX.25 с печатным текстом → сообщение.
	if pl.Kind != KindText && pl.Kind != KindUnknown {
		t.Fatalf("kind=%q decoder=%q", pl.Kind, pl.Decoder)
	}
}

func packUSP(msgType uint16, body []byte) []byte {
	out := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint16(out[0:], msgType)
	binary.LittleEndian.PutUint16(out[6:], uint16(len(body)))
	copy(out[8:], body)
	return out
}
