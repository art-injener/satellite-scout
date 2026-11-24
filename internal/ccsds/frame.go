package ccsds

import (
	"encoding/binary"
	"fmt"

	"github.com/art-injener/satellite-scout/internal/fec"
)

const (
	// SyncWord — Attached Sync Marker CCSDS CADU (MSB первым на линии).
	SyncWord uint32 = 0x1ACFFC1D

	syncMaxErrors = 3
	preambleByte  = 0x55

	// CodeblockLen — полный блок RS(255,223) после ASM (interleave I=1).
	CodeblockLen = fec.RSNN
	// DataLen — длина полезной нагрузки в полном кодовом слове.
	DataLen = fec.RSDataLen
)

// Encode собирает уровни линии: preamble, ASM, кодовое слово (данные с нулевым
// дополнением до 223 байт + чётность dual-basis) под скремблером CCSDS.
// data — содержимое до RS; пустой или длиннее DataLen — ошибка.
func Encode(data []byte, preamble, postamble int) ([]byte, error) {
	if len(data) == 0 || len(data) > DataLen {
		return nil, fmt.Errorf("ccsds: data length %d, want 1..%d", len(data), DataLen)
	}
	padded := make([]byte, DataLen)
	copy(padded, data)
	parity, err := fec.EncodeRSDual(padded)
	if err != nil {
		return nil, err
	}
	block := append(padded, parity...)
	fec.CCSDSScramble(block)

	levels := make([]byte, 0, (preamble+4+len(block)+postamble)*8)
	for range preamble {
		levels = appendBits(levels, preambleByte)
	}
	for _, b := range binary.BigEndian.AppendUint32(nil, SyncWord) {
		levels = appendBits(levels, b)
	}
	for _, b := range block {
		levels = appendBits(levels, b)
	}
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
