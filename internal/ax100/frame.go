package ax100

import (
	"encoding/binary"
	"fmt"

	"github.com/art-injener/satellite-scout/internal/fec"
)

const (
	// SyncWord — ASM AX.100 / U482C (то же значение, что у CC1125 GEOSCAN).
	SyncWord uint32 = 0x930B51DE

	syncMaxErrors = 4
	golayBytes    = 3
	preambleByte  = 0xAA

	// MinPayload — минимальная длина после Golay: RS-чётность + хотя бы 1 байт данных.
	MinPayload = fec.RSParityLen + 1
	// MaxPayload — u482c берёт длину из младших 8 бит Golay-поля.
	MaxPayload = 255
)

// Encode собирает уровни линии Mode 5: preamble, синхрослово, Golay(длина),
// полезная нагрузка под CCSDS-скремблером и RS. data — кадр до RS (обычно CSP).
func Encode(data []byte, preamble, postamble int) ([]byte, error) {
	if len(data) == 0 || len(data) > fec.RSDataLen {
		return nil, fmt.Errorf("ax100: data length %d, want 1..%d", len(data), fec.RSDataLen)
	}
	parity, err := fec.EncodeRS(data)
	if err != nil {
		return nil, err
	}
	payload := append(append([]byte(nil), data...), parity...)
	if len(payload) < MinPayload || len(payload) > MaxPayload {
		return nil, fmt.Errorf("ax100: coded length %d out of range", len(payload))
	}
	fec.CCSDSScramble(payload)

	cw := fec.EncodeGolay24(uint32(len(payload)))
	levels := make([]byte, 0, (preamble+4+golayBytes+len(payload)+postamble)*8)
	for range preamble {
		levels = appendBits(levels, preambleByte)
	}
	for _, b := range binary.BigEndian.AppendUint32(nil, SyncWord) {
		levels = appendBits(levels, b)
	}
	for _, b := range fec.Golay24ToBytes(cw) {
		levels = appendBits(levels, b)
	}
	for _, b := range payload {
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
