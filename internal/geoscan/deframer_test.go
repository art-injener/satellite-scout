package geoscan

import (
	"bytes"
	"math/rand/v2"
	"slices"
	"testing"
)

func mustEncode(t *testing.T, data []byte, preamble, postamble int) []byte {
	t.Helper()
	levels, err := Encode(data, preamble, postamble)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return levels
}

func TestDeframer_RoundTripBothSizes(t *testing.T) {
	for _, n := range []int{ShortDataLen, LongDataLen} {
		var d Deframer
		got := d.Push(nil, mustEncode(t, testData(n), 8, 2))
		if len(got) != 1 || !bytes.Equal(got[0], testData(n)) {
			t.Fatalf("%d bytes: frames=% X, want one frame % X", n, got, testData(n))
		}
		if d.Good() != 1 || d.Bad() != 0 {
			t.Errorf("%d bytes: good/bad=%d/%d, want 1/0", n, d.Good(), d.Bad())
		}
	}
}

func TestDeframer_ShortFrameDoesNotWaitForLongSize(t *testing.T) {
	// The transmitter stops right after a 66-byte frame: it must come out without more bits.
	var d Deframer
	if got := d.Push(nil, mustEncode(t, testData(ShortDataLen), 8, 0)); len(got) != 1 {
		t.Fatalf("frames=%d, want 1 right after the CRC", len(got))
	}
}

func TestDeframer_BackToBackFrames(t *testing.T) {
	a, b := testData(LongDataLen), testData(ShortDataLen)
	b[0] = 0x55
	levels := append(mustEncode(t, a, 8, 0), mustEncode(t, b, 2, 1)...)
	var d Deframer
	got := d.Push(nil, levels)
	if len(got) != 2 || !bytes.Equal(got[0], a) || !bytes.Equal(got[1], b) {
		t.Fatalf("frames=% X, want both frames", got)
	}
}

func TestDeframer_SyncWordErrors(t *testing.T) {
	for _, tc := range []struct {
		flips int
		want  int
	}{{4, 1}, {5, 0}} {
		levels := mustEncode(t, testData(LongDataLen), 8, 2)
		for i := range tc.flips {
			levels[8*8+i*6] ^= 1 // spread over the sync word after the preamble
		}
		var d Deframer
		if got := d.Push(nil, levels); len(got) != tc.want {
			t.Errorf("%d sync word errors: frames=%d, want %d", tc.flips, len(got), tc.want)
		}
	}
}

func TestDeframer_FlippedDataBitCountsAsBad(t *testing.T) {
	levels := mustEncode(t, testData(LongDataLen), 8, 2)
	levels[(8+4+10)*8+3] ^= 1
	var d Deframer
	if got := d.Push(nil, levels); len(got) != 0 {
		t.Fatalf("frames=%d, want 0 for a corrupted frame", len(got))
	}
	if d.Good() != 0 || d.Bad() != 1 {
		t.Errorf("good/bad=%d/%d, want 0/1: one bad per sync word, not per tried size", d.Good(), d.Bad())
	}
}

func TestDeframer_NoiseBeforeFrameGivesNoFalseFrames(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	noise := make([]byte, 20000)
	for i := range noise {
		noise[i] = byte(rng.UintN(2))
	}
	var d Deframer
	got := d.Push(nil, slices.Concat(noise, mustEncode(t, testData(LongDataLen), 8, 2)))
	if len(got) != 1 || !bytes.Equal(got[0], testData(LongDataLen)) {
		t.Fatalf("frames=%d, want only the real frame", len(got))
	}
}

func TestDeframer_StateContinuesAcrossCalls(t *testing.T) {
	levels := append(mustEncode(t, testData(LongDataLen), 8, 2), mustEncode(t, testData(ShortDataLen), 8, 2)...)
	var d Deframer
	var got [][]byte
	for i := 0; i < len(levels); i += 7 {
		got = d.Push(got, levels[i:min(i+7, len(levels))])
	}
	if len(got) != 2 {
		t.Fatalf("frames=%d, want 2", len(got))
	}
}

func TestDeframer_ReturnedFrameIsNotReused(t *testing.T) {
	var d Deframer
	first := d.Push(nil, mustEncode(t, testData(LongDataLen), 8, 2))
	want := append([]byte{}, first[0]...)
	d.Push(nil, mustEncode(t, make([]byte, LongDataLen), 8, 2))
	if !bytes.Equal(first[0], want) {
		t.Error("first frame changed after the next Push")
	}
}
