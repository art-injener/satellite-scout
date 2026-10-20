package ax25

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"testing"
)

func mustBytes(t *testing.T, f Frame) []byte {
	t.Helper()
	b, err := f.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return b
}

// lineLevels encodes frames as they leave the modulator: HDLC with flags, then NRZI.
func lineLevels(t *testing.T, frames ...Frame) []byte {
	t.Helper()
	var bits []byte
	for _, f := range frames {
		bits = append(bits, HDLCBits(mustBytes(t, f), 4, 1)...)
	}
	return NRZI(bits)
}

// withoutFCS — what the deframer returns for a valid frame.
func withoutFCS(t *testing.T, f Frame) []byte {
	b := mustBytes(t, f)
	return b[:len(b)-2]
}

// --- Deframer ---

func TestDeframer_RoundTrip(t *testing.T) {
	var d Deframer
	got := d.Push(nil, lineLevels(t, testFrame()))
	if len(got) != 1 {
		t.Fatalf("frames=%d, want 1", len(got))
	}
	if want := withoutFCS(t, testFrame()); !bytes.Equal(got[0], want) {
		t.Errorf("frame=% X, want % X", got[0], want)
	}
	if d.Good() != 1 || d.Bad() != 0 {
		t.Errorf("good/bad=%d/%d, want 1/0", d.Good(), d.Bad())
	}
}

func TestDeframer_BackToBackFrames(t *testing.T) {
	a := testFrame()
	b := Frame{Dest: Address{Call: "BEACON"}, Src: Address{Call: "RS0SIM", SSID: 15}, Info: []byte("second")}
	// One flag between frames: closing flag of the first opens the second.
	bits := HDLCBits(mustBytes(t, a), 2, 0)
	bits = append(bits, HDLCBits(mustBytes(t, b), 1, 1)...)

	var d Deframer
	got := d.Push(nil, NRZI(bits))
	if len(got) != 2 {
		t.Fatalf("frames=%d, want 2", len(got))
	}
	if !bytes.Equal(got[0], withoutFCS(t, a)) || !bytes.Equal(got[1], withoutFCS(t, b)) {
		t.Errorf("frames=% X / % X", got[0], got[1])
	}
}

func TestDeframer_StuffingHeavyInfo(t *testing.T) {
	f := testFrame()
	f.Info = []byte{0xFF, 0xFF, 0x7E, 0x7E, 0xFE, 0x3F, 0x00, 0xFF}
	var d Deframer
	got := d.Push(nil, lineLevels(t, f))
	if len(got) != 1 || !bytes.Equal(got[0], withoutFCS(t, f)) {
		t.Fatalf("frames=% X, want one frame % X", got, withoutFCS(t, f))
	}
}

func TestDeframer_NoiseBeforeFrameGivesNoFalseFrames(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	noise := make([]byte, 20000)
	for i := range noise {
		noise[i] = byte(rng.UintN(2))
	}
	var d Deframer
	got := d.Push(nil, append(noise, lineLevels(t, testFrame())...))
	if len(got) != 1 || !bytes.Equal(got[0], withoutFCS(t, testFrame())) {
		t.Fatalf("frames=%d, want only the real frame", len(got))
	}
}

func TestDeframer_FlippedBitCountsAsBad(t *testing.T) {
	levels := lineLevels(t, testFrame())
	levels[4*8+40] ^= 1 // inside the address field, after the preamble
	var d Deframer
	if got := d.Push(nil, levels); len(got) != 0 {
		t.Fatalf("frames=%d, want 0 for a corrupted frame", len(got))
	}
	if d.Good() != 0 || d.Bad() != 1 {
		t.Errorf("good/bad=%d/%d, want 0/1", d.Good(), d.Bad())
	}
}

func TestDeframer_InvertedPolarity(t *testing.T) {
	levels := lineLevels(t, testFrame())
	for i := range levels {
		levels[i] ^= 1
	}
	var d Deframer
	if got := d.Push(nil, levels); len(got) != 1 {
		t.Fatalf("frames=%d, want 1: NRZI does not depend on polarity", len(got))
	}
}

func TestDeframer_StateContinuesAcrossCalls(t *testing.T) {
	levels := lineLevels(t, testFrame(), testFrame())
	var d Deframer
	var got [][]byte
	for i := 0; i < len(levels); i += 7 {
		got = d.Push(got, levels[i:min(i+7, len(levels))])
	}
	if len(got) != 2 {
		t.Fatalf("frames=%d, want 2", len(got))
	}
}

func TestDeframer_AbortDropsFrame(t *testing.T) {
	bits := HDLCBits(mustBytes(t, testFrame()), 2, 1)
	// Eight ones in the middle of the frame: transmitter abort. Eight keep the bit count
	// a multiple of a byte, so without abort handling the remainder would reach FCS check.
	mid := 2*8 + 60
	aborted := append(append(append([]byte{}, bits[:mid]...), 1, 1, 1, 1, 1, 1, 1, 1), bits[mid:]...)
	var d Deframer
	if got := d.Push(nil, NRZI(aborted)); len(got) != 0 {
		t.Fatalf("frames=%d, want 0 after abort", len(got))
	}
	if d.Bad() != 0 {
		t.Errorf("bad=%d, want 0: an aborted frame is dropped, not checked", d.Bad())
	}
}

func TestDeframer_ReturnedFrameIsNotReused(t *testing.T) {
	var d Deframer
	first := d.Push(nil, lineLevels(t, testFrame()))
	want := append([]byte{}, first[0]...)
	d.Push(nil, lineLevels(t, Frame{Dest: Address{Call: "X"}, Src: Address{Call: "Y"}, Info: []byte("zz")}))
	if !bytes.Equal(first[0], want) {
		t.Error("first frame changed after the next Push")
	}
}

// --- Parse ---

func TestParse_RoundTrip(t *testing.T) {
	f, err := Parse(withoutFCS(t, testFrame()))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := testFrame()
	if f.Dest != want.Dest || f.Src != want.Src || !bytes.Equal(f.Info, want.Info) {
		t.Errorf("frame=%+v, want %+v", f, want)
	}
}

func TestParse_SkipsRepeaters(t *testing.T) {
	b := withoutFCS(t, testFrame())
	// Insert one repeater after the source: source loses the end-of-address bit.
	rep, err := encodeAddress(Address{Call: "ARISS"}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	withPath := append(append([]byte{}, b[:14]...), rep...)
	withPath[13] &^= 0x01
	withPath = append(withPath, b[14:]...)

	f, err := Parse(withPath)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Src != testFrame().Src || !bytes.Equal(f.Info, testFrame().Info) {
		t.Errorf("frame=%+v", f)
	}
}

func TestParse_Errors(t *testing.T) {
	good := withoutFCS(t, testFrame())
	notUI := append([]byte{}, good...)
	notUI[14] = 0x00 // I-frame

	noEnd := append([]byte{}, good...)
	for i := 6; i < len(noEnd); i += 7 {
		noEnd[i] &^= 0x01
	}

	cases := []struct {
		name string
		b    []byte
		want error
	}{
		{"too short", good[:15], ErrInvalidFrame},
		{"not UI", notUI, ErrInvalidFrame},
		{"no end of address", noEnd, ErrInvalidFrame},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.b); !errors.Is(err, tc.want) {
				t.Errorf("err=%v, want %v", err, tc.want)
			}
		})
	}
}
