package ax25

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"testing"
)

// --- FCS ---

func TestFCS_CheckValue(t *testing.T) {
	// Стандартное контрольное значение CRC-16/X.25 для строки "123456789".
	if got := FCS([]byte("123456789")); got != 0x906E {
		t.Errorf("FCS=0x%04X, want 0x906E", got)
	}
}

func TestFCS_Empty(t *testing.T) {
	if got := FCS(nil); got != 0x0000 {
		t.Errorf("FCS(nil)=0x%04X, want 0x0000", got)
	}
}

// --- Frame.Bytes ---

func testFrame() Frame {
	return Frame{
		Dest: Address{Call: "CQ"},
		Src:  Address{Call: "R4UAB", SSID: 1},
		Info: []byte("hi"),
	}
}

func TestFrameBytes_DestAddress(t *testing.T) {
	b, err := testFrame().Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	// Символы сдвинуты на 1 бит влево, дополнены пробелами; SSID-байт с битом команды.
	want := []byte{0x86, 0xA2, 0x40, 0x40, 0x40, 0x40, 0xE0}
	if !bytes.Equal(b[0:7], want) {
		t.Errorf("dest=% X, want % X", b[0:7], want)
	}
}

func TestFrameBytes_SrcAddressLastBit(t *testing.T) {
	b, err := testFrame().Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	// SSID=1, бит конца адресного поля выставлен.
	want := []byte{0xA4, 0x68, 0xAA, 0x82, 0x84, 0x40, 0x63}
	if !bytes.Equal(b[7:14], want) {
		t.Errorf("src=% X, want % X", b[7:14], want)
	}
}

func TestFrameBytes_ControlPIDInfo(t *testing.T) {
	b, err := testFrame().Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	want := []byte{0x03, 0xF0, 'h', 'i'}
	if !bytes.Equal(b[14:18], want) {
		t.Errorf("control/pid/info=% X, want % X", b[14:18], want)
	}
	if len(b) != 20 {
		t.Errorf("len=%d, want 20 (14 address + 2 control/pid + 2 info + 2 fcs)", len(b))
	}
}

func TestFrameBytes_FCSLittleEndian(t *testing.T) {
	b, err := testFrame().Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	n := len(b)
	fcs := FCS(b[:n-2])
	if b[n-2] != byte(fcs) || b[n-1] != byte(fcs>>8) {
		t.Errorf("fcs bytes=% X, want low-first of 0x%04X", b[n-2:], fcs)
	}
}

func TestFrameBytes_LowercaseCallsign(t *testing.T) {
	f := testFrame()
	f.Src.Call = "r4uab"
	b, err := f.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if b[7] != 0xA4 {
		t.Errorf("src[0]=0x%02X, want 0xA4 ('R'<<1)", b[7])
	}
}

func TestFrameBytes_InvalidAddress(t *testing.T) {
	cases := []struct {
		name string
		addr Address
		want error
	}{
		{"empty", Address{Call: ""}, ErrInvalidCallsign},
		{"too long", Address{Call: "ABCDEFG"}, ErrInvalidCallsign},
		{"bad char", Address{Call: "R4-AB"}, ErrInvalidCallsign},
		{"ssid over 15", Address{Call: "R4UAB", SSID: 16}, ErrInvalidSSID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := testFrame()
			f.Src = tc.addr
			_, err := f.Bytes()
			if !errors.Is(err, tc.want) {
				t.Errorf("err=%v, want %v", err, tc.want)
			}
		})
	}
}

// --- HDLCBits ---

var flagBits = []byte{0, 1, 1, 1, 1, 1, 1, 0}

func TestHDLCBits_FlagsAtEdges(t *testing.T) {
	bits := HDLCBits([]byte{0x00}, 2, 1)
	// 2 флага + 8 бит данных + 1 флаг.
	if len(bits) != 8*2+8+8 {
		t.Fatalf("len=%d, want %d", len(bits), 8*2+8+8)
	}
	for i := range 2 {
		if !bytes.Equal(bits[i*8:i*8+8], flagBits) {
			t.Errorf("leading flag %d=%v, want %v", i, bits[i*8:i*8+8], flagBits)
		}
	}
	if !bytes.Equal(bits[len(bits)-8:], flagBits) {
		t.Errorf("trailing flag=%v, want %v", bits[len(bits)-8:], flagBits)
	}
}

func TestHDLCBits_LSBFirst(t *testing.T) {
	bits := HDLCBits([]byte{0x01}, 0, 0)
	want := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(bits, want) {
		t.Errorf("bits=%v, want %v", bits, want)
	}
}

func TestHDLCBits_StuffsZeroAfterFiveOnes(t *testing.T) {
	bits := HDLCBits([]byte{0xFF}, 0, 0)
	want := []byte{1, 1, 1, 1, 1, 0, 1, 1, 1}
	if !bytes.Equal(bits, want) {
		t.Errorf("bits=%v, want %v", bits, want)
	}
}

func TestHDLCBits_StuffingAcrossByteBoundary(t *testing.T) {
	// 0xF0 даёт в конце четыре единицы, 0x01 — пятую в начале следующего байта.
	bits := HDLCBits([]byte{0xF0, 0x01}, 0, 0)
	want := []byte{0, 0, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(bits, want) {
		t.Errorf("bits=%v, want %v", bits, want)
	}
}

func TestHDLCBits_NoSixOnesInsidePayload(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	payload := make([]byte, 512)
	for i := range payload {
		payload[i] = byte(rng.UintN(256))
	}
	bits := HDLCBits(payload, 1, 1)
	inner := bits[8 : len(bits)-8]
	run := 0
	for i, b := range inner {
		if b == 1 {
			run++
		} else {
			run = 0
		}
		if run >= 6 {
			t.Fatalf("six ones in a row at bit %d inside payload", i)
		}
	}
}

// --- NRZI ---

func TestNRZI_ZeroTogglesOneKeeps(t *testing.T) {
	// Начальный уровень 0: ноль меняет уровень, единица оставляет.
	got := NRZI([]byte{0, 1, 1, 0, 0, 1})
	want := []byte{1, 1, 1, 0, 1, 1}
	if !bytes.Equal(got, want) {
		t.Errorf("levels=%v, want %v", got, want)
	}
}
