package geoscan

import (
	"bytes"
	"errors"
	"testing"
)

func TestWhiten_MatchesCC11xxPN9(t *testing.T) {
	// PN9 sequence of TI CC11xx data whitening: it is what whitening adds to zero bytes.
	want := []byte{0xFF, 0xE1, 0x1D, 0x9A, 0xED, 0x85, 0x33, 0x24, 0xEA, 0x7A, 0xD2, 0x39, 0x70, 0x97, 0x57, 0x0A}
	got := make([]byte, len(want))
	Whiten(got)
	if !bytes.Equal(got, want) {
		t.Errorf("PN9=% X, want % X", got, want)
	}
}

func TestWhiten_TwiceRestoresData(t *testing.T) {
	data := []byte("GEOSCAN whitening round trip, longer than the 511-bit PN9 period: 0123456789abcdef0123456789")
	b := append([]byte{}, data...)
	Whiten(b)
	if bytes.Equal(b, data) {
		t.Fatal("whitening left data unchanged")
	}
	Whiten(b)
	if !bytes.Equal(b, data) {
		t.Errorf("after two passes % X, want % X", b, data)
	}
}

func TestCRC16_CheckValue(t *testing.T) {
	// CRC-16 of CC11xx (poly 0x8005, init 0xFFFF, no reflection) is CRC-16/CMS: check value 0xAEE7.
	if got := CRC16([]byte("123456789")); got != 0xAEE7 {
		t.Errorf("CRC16=0x%04X, want 0xAEE7", got)
	}
}

func TestEncode_Layout(t *testing.T) {
	data := testData(LongDataLen)
	levels, err := Encode(data, 4, 2)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if want := (4 + 4 + LongDataLen + 2 + 2) * 8; len(levels) != want {
		t.Fatalf("levels=%d, want %d", len(levels), want)
	}
	b := packBits(levels)
	if !bytes.Equal(b[:4], []byte{0xAA, 0xAA, 0xAA, 0xAA}) {
		t.Errorf("preamble=% X, want AA x4", b[:4])
	}
	if !bytes.Equal(b[4:8], []byte{0x93, 0x0B, 0x51, 0xDE}) {
		t.Errorf("sync word=% X, want 93 0B 51 DE", b[4:8])
	}
	frame := append([]byte{}, b[8:8+LongDataLen+2]...)
	Whiten(frame)
	if !bytes.Equal(frame[:LongDataLen], data) {
		t.Errorf("data after dewhitening=% X, want % X", frame[:LongDataLen], data)
	}
	if crc := CRC16(data); frame[LongDataLen] != byte(crc>>8) || frame[LongDataLen+1] != byte(crc) {
		t.Errorf("CRC bytes=% X, want 0x%04X most significant first", frame[LongDataLen:], crc)
	}
	if !bytes.Equal(b[len(b)-2:], []byte{0xAA, 0xAA}) {
		t.Errorf("postamble=% X, want AA AA", b[len(b)-2:])
	}
}

func TestEncode_RejectsOtherLengths(t *testing.T) {
	for _, n := range []int{0, 48, ShortDataLen + 1, LongDataLen + 1} {
		if _, err := Encode(make([]byte, n), 4, 2); !errors.Is(err, ErrInvalidLength) {
			t.Errorf("%d bytes: err=%v, want ErrInvalidLength", n, err)
		}
	}
}

// testData — frame data of the given length with a recognizable pattern.
func testData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + 3)
	}
	return b
}

// packBits packs line levels into bytes, most significant bit first.
func packBits(levels []byte) []byte {
	b := make([]byte, len(levels)/8)
	for i, lv := range levels[:len(b)*8] {
		b[i/8] |= (lv & 1) << (7 - i%8)
	}
	return b
}
