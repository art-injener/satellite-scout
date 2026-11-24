package usp

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/art-injener/satellite-scout/internal/ax25"
)

func TestBeaconParams_General(t *testing.T) {
	body := make([]byte, BeaconGeneralLen)
	binary.LittleEndian.PutUint16(body[0:], 8200)                  // Usb1 мВ
	binary.LittleEndian.PutUint16(body[2:], 8100)                  // Usb2
	binary.LittleEndian.PutUint16(body[4:], 8000)                  // Usb3
	binary.LittleEndian.PutUint16(body[6:], 120)                   // Isb1
	binary.LittleEndian.PutUint16(body[14:], 50)                   // Ich1
	binary.LittleEndian.PutUint16(body[22:], uint16(int16(25)))    // t1_pw
	binary.LittleEndian.PutUint16(body[34:], uint16(int16(7400)))  // Uab мВ
	body[44] = 3                                                   // Nres
	body[46] = 12                                                  // t_amp
	body[48] = 40                                                  // RSSIrx → −40
	binary.LittleEndian.PutUint32(body[58:], 3600)                 // UpTime
	binary.LittleEndian.PutUint16(body[62:], 180)                  // Current УКВ
	binary.LittleEndian.PutUint16(body[64:], uint16(int16(3300)))  // Uuhf

	info := packUSPMessage(TypeBeacon, 1, 2, body)
	frame := ax25.Frame{Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "RS32S"}, Info: info}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	params, kind, ok := BeaconParams(raw)
	if !ok {
		t.Fatal("expected beacon")
	}
	if kind != "beacon" {
		t.Fatalf("kind=%q", kind)
	}
	got := map[string]string{}
	for _, p := range params {
		got[p.ID] = p.Value
	}
	if got["uab"] != "7,4" {
		t.Fatalf("uab=%q, want 7,4 В", got["uab"])
	}
	if got["rssi_rx"] != "-40" {
		t.Fatalf("rssi_rx=%q", got["rssi_rx"])
	}
	if got["uptime_uhf"] != "3600" {
		t.Fatalf("uptime=%q", got["uptime_uhf"])
	}
	if got["t_amp"] != "12" {
		t.Fatalf("t_amp=%q", got["t_amp"])
	}
	keyIDs := 0
	for _, p := range params {
		if p.Key {
			keyIDs++
		}
	}
	if keyIDs < 3 {
		t.Fatalf("key params=%d, want ≥3", keyIDs)
	}
}

func TestBeaconParams_UHF(t *testing.T) {
	body := make([]byte, BeaconUHFLen)
	body[0] = 0xF6 // t_amp = -10
	body[1] = 20   // t_uhf
	body[2] = 55   // RSSIrx → -55
	body[3] = 90   // RSSIidle → -90
	body[4] = 10   // Pf
	body[5] = 0xFC // Pb = -4
	binary.LittleEndian.PutUint32(body[8:], 100)   // time (не нужен)
	binary.LittleEndian.PutUint32(body[12:], 7200) // uptime
	binary.LittleEndian.PutUint32(body[16:], 4800) // bitrate
	binary.LittleEndian.PutUint16(body[20:], 200)
	binary.LittleEndian.PutUint16(body[22:], uint16(int16(3200)))

	info := packUSPMessage(TypeUHFBeacon, 0, 0, body)
	frame := ax25.Frame{Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "RS15S"}, Info: info}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	params, kind, ok := BeaconParams(raw)
	if !ok || kind != "uhf_beacon" {
		t.Fatalf("ok=%v kind=%q", ok, kind)
	}
	byID := map[string]string{}
	for _, p := range params {
		byID[p.ID] = p.Value
	}
	if byID["t_amp"] != "-10" || byID["rssi_rx"] != "-55" || byID["uptime_uhf"] != "7200" {
		t.Fatalf("params=%v", byID)
	}
}

func TestBeaconParams_Extended(t *testing.T) {
	body := make([]byte, BeaconExtendedLen)
	binary.LittleEndian.PutUint16(body[0:], uint16(int16(27))) // t_mb
	binary.LittleEndian.PutUint32(body[2:], math.Float32bits(12.5))   // mx
	binary.LittleEndian.PutUint32(body[10:], math.Float32bits(-3.25)) // mz @ 2+2*4
	body[26] = 5                                                     // nres @ 2+24
	binary.LittleEndian.PutUint32(body[29:], uint32(1_700_000_000))
	info := PackMessage(TypeExtendedBeacon, 0, 0, body)
	frame := ax25.Frame{Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "RS15S"}, Info: info}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	params, kind, ok := BeaconParams(raw)
	if !ok || kind != "extended_beacon" {
		t.Fatalf("ok=%v kind=%q", ok, kind)
	}
	byID := map[string]string{}
	for _, p := range params {
		byID[p.ID] = p.Value
	}
	if byID["t_mb"] != "27" || byID["nres"] != "5" {
		t.Fatalf("params=%v", byID)
	}
	if byID["mz"] != "-3,25" {
		t.Fatalf("mz=%q", byID["mz"])
	}
}

func TestBeaconParams_RejectsText(t *testing.T) {
	frame := ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "RS0SIM"},
		Info: []byte("hello USP"),
	}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := BeaconParams(raw); ok {
		t.Fatal("text must not parse as beacon")
	}
}

func TestBeaconParams_WithoutFCS(t *testing.T) {
	body := make([]byte, BeaconUHFLen)
	body[1] = 18
	body[2] = 50
	binary.LittleEndian.PutUint32(body[12:], 100)
	info := packUSPMessage(TypeUHFBeacon, 0, 0, body)
	frame := ax25.Frame{Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "RS15S"}, Info: info}
	raw, err := frame.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := BeaconParams(raw[:len(raw)-2]); !ok {
		t.Fatal("expected beacon without FCS")
	}
}

func packUSPMessage(msgType, sender, receiver uint16, body []byte) []byte {
	out := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint16(out[0:], msgType)
	binary.LittleEndian.PutUint16(out[2:], sender)
	binary.LittleEndian.PutUint16(out[4:], receiver)
	binary.LittleEndian.PutUint16(out[6:], uint16(len(body)))
	copy(out[8:], body)
	return out
}
