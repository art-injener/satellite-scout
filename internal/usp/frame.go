package usp

import (
	"encoding/binary"
	"fmt"

	"github.com/art-injener/satellite-scout/internal/fec"
)

const (
	// SyncWord — 64-бит ASM USP (MSB первым), gr-satellites usp_deframer.
	SyncWord uint64 = 0x5072F64B2D90B1F5

	syncMaxErrors = 13
	preambleByte  = 0xAA

	// Длины данных после RS (байт) по PLS; как в gr-satellites (опечатка в PDF 1.04).
	DataLenShort = 48
	DataLenLong  = 223
)

var (
	pls0 = computePLS(0)
	pls1 = computePLS(1)
)

func computePLS(code int) []byte {
	const gBits = "0011001100110011001100110011001100110011001100110011001100110011" +
		"0000111100001111000011110000111100001111000011110000111100001111" +
		"0000000011111111000000001111111100000000111111110000000011111111" +
		"0000000000000000111111111111111100000000000000001111111111111111" +
		"0000000000000000000000000000000011111111111111111111111111111111" +
		"1111111111111111111111111111111111111111111111111111111111111110" +
		"1010101010101010101010101010101010101010101010101010101010101010"
	const scramble = "0111000110011101100000111100100101010011010000100010110111111010"
	info := [7]byte{}
	if code == 1 {
		info[6] = 1
	}
	out := make([]byte, 64)
	for i := 0; i < 64; i++ {
		var sum byte
		for j := 0; j < 7; j++ {
			sum ^= (gBits[j*64+i] - '0') & info[j]
		}
		out[i] = sum ^ (scramble[i] - '0')
	}
	return out
}

func dataLenForPLS(pls []byte) (int, bool) {
	if len(pls) < 64 {
		return 0, false
	}
	c0, c1 := 0, 0
	for i := 0; i < 64; i++ {
		b := pls[i] & 1
		if b == pls0[i] {
			c0++
		}
		if b == pls1[i] {
			c1++
		}
	}
	// Порог: больше половины совпадений с одним из кодов.
	if c0 >= c1 && c0 >= 40 {
		return DataLenShort, true
	}
	if c1 > c0 && c1 >= 40 {
		return DataLenLong, true
	}
	return 0, false
}

// Encode собирает уровни линии USP. ax25Frame — сырой AX.25 с FCS.
func Encode(ax25Frame []byte, longFrame bool, preamble, postamble int) ([]byte, error) {
	dataLen := DataLenShort
	pls := pls0
	if longFrame {
		dataLen = DataLenLong
		pls = pls1
	}
	if len(ax25Frame) > dataLen-4 {
		return nil, fmt.Errorf("usp: AX.25 length %d > %d", len(ax25Frame), dataLen-4)
	}
	inner := make([]byte, dataLen)
	binary.LittleEndian.PutUint16(inner[2:4], uint16(len(ax25Frame)))
	copy(inner[4:], ax25Frame)

	parity, err := fec.EncodeRSDual(inner)
	if err != nil {
		return nil, err
	}
	block := append(inner, parity...)
	fec.CCSDSScramble(block)
	coded := fec.EncodeViterbiCCSDSTruncated(block)

	levels := make([]byte, 0, (preamble+16)*8+len(coded)+postamble*8)
	for range preamble {
		levels = appendBits(levels, preambleByte)
	}
	for i := 0; i < 64; i++ {
		levels = append(levels, byte((SyncWord>>uint(63-i))&1))
	}
	levels = append(levels, pls...)
	levels = append(levels, coded...)
	for range postamble {
		levels = appendBits(levels, preambleByte)
	}
	return levels, nil
}

func appendBits(levels []byte, b byte) []byte {
	for i := 7; i >= 0; i-- {
		levels = append(levels, (b>>uint(i))&1)
	}
	return levels
}

func cropAX25(inner []byte) ([]byte, bool) {
	if len(inner) < 4 {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint16(inner[2:4]))
	if n == 0 || 4+n > len(inner) {
		return nil, false
	}
	return append([]byte(nil), inner[4:4+n]...), true
}
