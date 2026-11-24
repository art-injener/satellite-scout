package payload

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/geoscan"
)

func TestDecode_EmptyIsUnknown(t *testing.T) {
	p := Decode(Frame{})
	if p.Kind != KindUnknown || p.Decoder != "unknown" {
		t.Fatalf("got kind=%q decoder=%q", p.Kind, p.Decoder)
	}
}

func TestDecode_StrictAX25Text(t *testing.T) {
	raw, err := ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "R1ABC"},
		Info: []byte("hello scout"),
	}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	raw = raw[:len(raw)-2] // без FCS: так приходит из дефрамера
	p := Decode(Frame{Raw: raw, Profile: "afsk1200"})
	if p.Kind != KindText || p.Decoder != "ax25.ui" {
		t.Fatalf("got kind=%q decoder=%q", p.Kind, p.Decoder)
	}
	if p.Src != "R1ABC" || p.Dest != "CQ" || p.Info != "hello scout" {
		t.Fatalf("src/dest/info = %q/%q/%q", p.Src, p.Dest, p.Info)
	}
	if p.InfoHex != hex.EncodeToString([]byte("hello scout")) {
		t.Fatalf("info_hex=%s", p.InfoHex)
	}
}

func TestDecode_GarbageCallsignIsUnknown(t *testing.T) {
	// Байты похожи на кадр, но позывной с недопустимым символом → не text/beacon.
	raw := make([]byte, 16)
	copy(raw[0:6], []byte{('T' << 1), (' ' << 1), (' ' << 1), (' ' << 1), (' ' << 1), (' ' << 1)})
	raw[6] = 0x60 // SSID, не конец
	// Src: символ 0x01<<1 недопустим после Decode call
	copy(raw[7:13], []byte{0x02, ('A' << 1), (' ' << 1), (' ' << 1), (' ' << 1), (' ' << 1)})
	raw[13] = 0x61 // конец адреса
	raw[14] = 0x03 // UI
	raw[15] = 0xF0 // PID
	raw = append(raw, []byte("x")...)
	p := Decode(Frame{Raw: raw, Profile: "afsk1200"})
	if p.Kind != KindUnknown {
		t.Fatalf("got kind=%q, want unknown (no false TMI)", p.Kind)
	}
}

func TestDecode_GEOSCANBeacon(t *testing.T) {
	info, err := geoscan.BuildBeacon(map[string]float64{"eps_cell_voltage_full": 7.4})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ax25.Frame{
		Dest: ax25.Address{Call: geoscan.BeaconDest},
		Src:  ax25.Address{Call: "RS23S"},
		Info: info,
	}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	raw = raw[:len(raw)-2]
	p := Decode(Frame{Raw: raw, Profile: profileGEOSCAN})
	if p.Kind != KindBeacon || p.Decoder != "geoscan.beacon" {
		t.Fatalf("got kind=%q decoder=%q", p.Kind, p.Decoder)
	}
	if p.Src != "RS23S" || p.Dest != geoscan.BeaconDest {
		t.Fatalf("src/dest = %q/%q", p.Src, p.Dest)
	}
	if len(p.Params) == 0 {
		t.Fatal("expected beacon params")
	}
}

func TestDecode_GEOSCANBeaconAfterTransportHeader(t *testing.T) {
	info, err := geoscan.BuildBeacon(nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := ax25.Frame{
		Dest: ax25.Address{Call: geoscan.BeaconDest},
		Src:  ax25.Address{Call: "RS20S"},
		Info: info,
	}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	body = body[:len(body)-2]
	raw := append([]byte{0x01, 0x00, 0x26, 0x04, 0x20}, body...)
	p := Decode(Frame{Raw: raw, Profile: profileGEOSCAN})
	if p.Kind != KindBeacon || p.Decoder != "geoscan.beacon" {
		t.Fatalf("got kind=%q decoder=%q", p.Kind, p.Decoder)
	}
}

// Кадр как в записи pe0sat: короткий CC1125, транспортный заголовок, бит конца адреса сброшен.
func TestDecode_GEOSCANBeaconFromAirShortFrame(t *testing.T) {
	raw, err := hex.DecodeString(
		"0100260420848a82869e9c60a4a66460a6406003f00c00e305bf051b000900589ec709e7ec00f101" +
			"000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 64 {
		t.Fatalf("len=%d, want 64", len(raw))
	}
	p := Decode(Frame{Raw: raw, Profile: profileGEOSCAN})
	if p.Kind != KindBeacon || p.Decoder != "geoscan.beacon" {
		t.Fatalf("got kind=%q decoder=%q, want beacon", p.Kind, p.Decoder)
	}
	if p.Src != "RS20S" || p.Dest != geoscan.BeaconDest {
		t.Fatalf("src/dest = %q/%q", p.Src, p.Dest)
	}
	if len(p.Params) == 0 {
		t.Fatal("expected beacon params")
	}
}

func TestDecode_GEOSCANImage(t *testing.T) {
	raw := make([]byte, imgHeaderLen+imgChunkStep)
	raw[0], raw[1], raw[2] = imgMagic0, 0x00, imgMagic2
	raw[3] = 0x05 // продолжение
	raw[4] = imgMagic4
	binary.LittleEndian.PutUint16(raw[5:7], imgOffsetBase+2*imgChunkStep)
	raw[7] = imgMagic7
	copy(raw[imgHeaderLen:], []byte{0xFF, 0xD8, 0xFF, 0xDB})

	p := Decode(Frame{Raw: raw, Profile: profileGEOSCAN})
	if p.Kind != KindImage || p.Decoder != "geoscan.image" {
		t.Fatalf("got kind=%q decoder=%q", p.Kind, p.Decoder)
	}
	if p.Offset != imgOffsetBase+2*imgChunkStep {
		t.Fatalf("offset=%d", p.Offset)
	}
	if p.Summary == "" || p.InfoHex == "" {
		t.Fatalf("summary/hex empty: %q / %q", p.Summary, p.InfoHex)
	}
}

func TestDecode_GEOSCANImageNotOnAFSK(t *testing.T) {
	raw := make([]byte, imgHeaderLen+8)
	raw[0], raw[2], raw[4], raw[7] = imgMagic0, imgMagic2, imgMagic4, imgMagic7
	p := Decode(Frame{Raw: raw, Profile: "afsk1200"})
	if p.Kind == KindImage {
		t.Fatal("image decoder must not run outside geoscan profile")
	}
}

func TestLabel(t *testing.T) {
	if Label(KindBeacon) != "Маяк ТМИ" {
		t.Fatal(Label(KindBeacon))
	}
	if Label(KindUnknown) != "Неизвестно" {
		t.Fatal(Label(KindUnknown))
	}
	if Label(KindLine) != "Линия" {
		t.Fatal(Label(KindLine))
	}
	if Label(KindSonde) != "Зонд" {
		t.Fatal(Label(KindSonde))
	}
}

func TestDecode_RS41Sonde(t *testing.T) {
	raw, err := hex.DecodeString("8635f44093df1a602c87e0fa0521e8943d9cef4c7a67393f6d39fb546461f2111b6447ab79a746c80350cda5344157f8c0c12234f46902220f792816174b313933303239331a00000300000a00002f0007322ce53e31991abf12dada3eb68468c16755d51c7a2a15310216060245f302000d08a31607821e08bb210219060243f302000000000000000000000000000000220d7c1e0807d03cdc071fd81ddb19d70a8d0eb602b60cb518d40692ff00ff00ff001c277d59b8d83301ff0f881f0f38f4fe18b283038735ff000000003eb8ff4947201e6e3aff55415f13fc6e005440440cf100009e9f7406f85800832b631719d70010bebc172a8b00000000000000000000000000000000000000000000a48b7b15366181193ef05d07e1245b1be0f721f801f60804107b0b76110000000000000000000000000000000000ecc7")
	if err != nil {
		t.Fatal(err)
	}
	p := Decode(Frame{Raw: raw, Profile: "rs41"})
	if p.Kind != KindSonde || p.Decoder != "rs41" {
		t.Fatalf("kind=%q decoder=%q", p.Kind, p.Decoder)
	}
	if p.Src != "K1930293" || !strings.Contains(p.Summary, "K1930293") {
		t.Fatalf("summary=%q src=%q", p.Summary, p.Src)
	}
	if len(p.Params) < 4 {
		t.Fatalf("params=%v", p.Params)
	}
}

func TestDecode_M10Sonde(t *testing.T) {
	raw, err := hex.DecodeString("649f20000000000000000000000020bf258c0b78607c004c4b400000000000000000000000000000000000000000000000000000000000000000000000000106a20000000000000000000000000000000000000000000000000000000002083a3125019f37")
	if err != nil {
		t.Fatal(err)
	}
	p := Decode(Frame{Raw: raw, Profile: "m10"})
	if p.Kind != KindSonde || p.Decoder != "m10" {
		t.Fatalf("kind=%q decoder=%q", p.Kind, p.Decoder)
	}
	if !strings.Contains(p.Summary, "310") {
		t.Fatalf("summary=%q", p.Summary)
	}
	var hasTemp bool
	for _, prm := range p.Params {
		if prm.ID == "temp" {
			hasTemp = true
		}
	}
	if !hasTemp {
		t.Fatalf("params=%v", p.Params)
	}
}

func TestDecode_OOKLineHex(t *testing.T) {
	raw := []byte{0xAA, 0x55, 0xCC, 0x33}
	p := Decode(Frame{Raw: raw, Profile: "ook"})
	if p.Kind != KindLine {
		t.Fatalf("kind=%q, want line", p.Kind)
	}
	if p.InfoHex != hex.EncodeToString(raw) {
		t.Fatalf("hex=%s", p.InfoHex)
	}
	if p.Summary != "Линия" {
		t.Fatalf("summary=%q", p.Summary)
	}
}

func TestDecode_BPSK9600StrictAX25Text(t *testing.T) {
	raw, err := ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "RS0SIM"},
		Info: []byte("bpsk9600 beacon"),
	}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	raw = raw[:len(raw)-2]
	p := Decode(Frame{Raw: raw, Profile: "bpsk9600"})
	if p.Kind != KindText || p.Decoder != "ax25.ui" {
		t.Fatalf("got kind=%q decoder=%q", p.Kind, p.Decoder)
	}
}

func TestDecode_QPSKCCSDSIsUnknown(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	p := Decode(Frame{Raw: raw, Profile: "qpsk_ccsds"})
	if p.Kind != KindUnknown {
		t.Fatalf("kind=%q, want unknown (no TM yet)", p.Kind)
	}
}
