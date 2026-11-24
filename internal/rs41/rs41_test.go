package rs41

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Golden std frame (clear, after whitening) from rs1729/RS rs41.txt — CRC-OK TLV blocks.
const goldenClearHex = "8635f44093df1a602c87e0fa0521e8943d9cef4c7a67393f6d39fb546461f2111b6447ab79a746c80350cda5344157f8c0c12234f46902220f792816174b313933303239331a00000300000a00002f0007322ce53e31991abf12dada3eb68468c16755d51c7a2a15310216060245f302000d08a31607821e08bb210219060243f302000000000000000000000000000000220d7c1e0807d03cdc071fd81ddb19d70a8d0eb602b60cb518d40692ff00ff00ff001c277d59b8d83301ff0f881f0f38f4fe18b283038735ff000000003eb8ff4947201e6e3aff55415f13fc6e005440440cf100009e9f7406f85800832b631719d70010bebc172a8b00000000000000000000000000000000000000000000a48b7b15366181193ef05d07e1245b1be0f721f801f60804107b0b76110000000000000000000000000000000000ecc7"

func goldenClear(t *testing.T) []byte {
	t.Helper()
	b, err := hex.DecodeString(goldenClearHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != StdFrameLen {
		t.Fatalf("len=%d", len(b))
	}
	return b
}

func TestCRC16_StatusBlock(t *testing.T) {
	f := goldenClear(t)
	if !checkTLV(f, posStatus) {
		t.Fatal("status CRC")
	}
	if !checkTLV(f, posGPS3) {
		t.Fatal("gps3 CRC")
	}
}

func TestWhiten_HeaderRoundTrip(t *testing.T) {
	f := goldenClear(t)
	air := ScrambleFrame(f)
	if !bytes.Equal(air[:8], ScrambledHeader) {
		t.Fatalf("scrambled hdr=%x", air[:8])
	}
	back := Descramble(air)
	if !bytes.Equal(back, f) {
		t.Fatal("descramble mismatch")
	}
}

func TestEncodeMsg_MatchesGoldenParity(t *testing.T) {
	f := goldenClear(t)
	data := f[DataOffset:]
	const useful = (StdFrameLen - DataOffset) / 2
	msg1 := make([]byte, useful)
	msg2 := make([]byte, useful)
	for i := 0; i < useful; i++ {
		msg1[i] = data[2*(useful-1-i)]
		msg2[i] = data[2*(useful-1-i)+1]
	}
	c1, err := encodeMsg(msg1)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := encodeMsg(msg2)
	if err != nil {
		t.Fatal(err)
	}
	p1 := reverseCopy(c1[useful:])
	p2 := reverseCopy(c2[useful:])
	if !bytes.Equal(p1, f[HeaderLen:HeaderLen+24]) {
		t.Fatalf("parity1 got %x want %x", p1, f[HeaderLen:HeaderLen+24])
	}
	if !bytes.Equal(p2, f[HeaderLen+24:HeaderLen+48]) {
		t.Fatalf("parity2 got %x want %x", p2, f[HeaderLen+24:HeaderLen+48])
	}
}

func TestCorrectFrame_CleanGolden(t *testing.T) {
	f := append([]byte(nil), goldenClear(t)...)
	n, ok := CorrectFrame(f)
	if !ok {
		t.Fatal("correct failed")
	}
	if n != 0 {
		t.Fatalf("corrected=%d, want 0", n)
	}
	if !FrameOK(f) {
		t.Fatal("frame not ok after correct")
	}
}

func TestCorrectFrame_FixesByteErrors(t *testing.T) {
	f := append([]byte(nil), goldenClear(t)...)
	f[80] ^= 0xFF
	f[200] ^= 0x0F
	n, ok := CorrectFrame(f)
	if !ok {
		t.Fatal("correct failed")
	}
	if n < 1 {
		t.Fatalf("corrected=%d, want >=1", n)
	}
	if !FrameOK(f) {
		t.Fatal("status CRC after repair")
	}
	tel, ok := Parse(f)
	if !ok || tel.ID != "K1930293" {
		t.Fatalf("parse=%+v ok=%v", tel, ok)
	}
}

func TestParse_GoldenIDAndPosition(t *testing.T) {
	tel, ok := Parse(goldenClear(t))
	if !ok {
		t.Fatal("parse")
	}
	if tel.ID != "K1930293" {
		t.Fatalf("id=%q", tel.ID)
	}
	if tel.FrameNb != 0x1716 {
		t.Fatalf("frame=%d want 0x1716", tel.FrameNb)
	}
	if !tel.HasPos {
		t.Fatal("no position")
	}
	if tel.LatDeg < 46 || tel.LatDeg > 47 || tel.LonDeg < 16 || tel.LonDeg > 17 {
		t.Fatalf("llh=%.5f %.5f %.1f", tel.LatDeg, tel.LonDeg, tel.AltM)
	}
	if tel.AltM < 20000 || tel.AltM > 40000 {
		t.Fatalf("alt=%.1f", tel.AltM)
	}
}

func TestDeframer_GoldenOnAir(t *testing.T) {
	air := ScrambleFrame(goldenClear(t))
	levels := BytesToLevels(air)
	// Leading idle zeros, then frame.
	in := append(bytes.Repeat([]byte{0}, 16), levels...)
	d := &Deframer{}
	out := d.Push(nil, in)
	if len(out) != 1 {
		t.Fatalf("frames=%d good=%d bad=%d sync=%d", len(out), d.Good(), d.Bad(), d.SyncWords())
	}
	tel, ok := Parse(out[0])
	if !ok || tel.ID != "K1930293" {
		t.Fatalf("tel=%+v", tel)
	}
}

func TestDeframer_InvertedPolarity(t *testing.T) {
	air := ScrambleFrame(goldenClear(t))
	levels := BytesToLevels(air)
	for i := range levels {
		levels[i] ^= 1
	}
	d := &Deframer{}
	out := d.Push(nil, levels)
	if len(out) != 1 {
		t.Fatalf("frames=%d bad=%d", len(out), d.Bad())
	}
	tel, ok := Parse(out[0])
	if !ok || tel.ID != "K1930293" {
		t.Fatalf("tel=%+v", tel)
	}
}

func TestGeneratorPoly_MatchesPublished(t *testing.T) {
	want := []byte{
		0x01, 0x7a, 0x76, 0xa9, 0x46, 0xb2, 0xed, 0xd8, 0x66, 0x73, 0x96, 0xe5, 0x49,
		0x82, 0x48, 0x3d, 0x2b, 0xce, 0x01, 0xed, 0xf7, 0x7f, 0xd9, 0x90, 0x75,
	}
	got := rs41codec.genpoly[:]
	if !bytes.Equal(got, want) {
		t.Fatalf("gen=%x want=%x", got, want)
	}
}
