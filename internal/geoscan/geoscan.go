package geoscan

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrInvalidLength — длина данных кадра не из поддерживаемых.
var ErrInvalidLength = errors.New("invalid geoscan frame length")

const (
	// SyncWord — синхрослово CC1125 перед данными кадра.
	SyncWord uint32 = 0x930B51DE
	// Длина данных без CRC: 64 байта у GEOSCAN-EDELVEIS, 72 у Geoscan-1…6.
	ShortDataLen = 64
	LongDataLen  = 72

	crcLen       = 2
	preambleByte = 0xAA
)

// Whiten накладывает на b скремблер PN9 CC11xx (x⁹ + x⁵ + 1, начальное состояние 0x1FF)
// с начала последовательности. Скремблер аддитивный: повторный вызов возвращает данные.
func Whiten(b []byte) {
	lfsr := uint16(0x1FF)
	for i := range b {
		b[i] ^= byte(lfsr & 0xFF)
		for range 8 {
			fb := (lfsr ^ lfsr>>5) & 1
			lfsr = lfsr>>1 | fb<<8
		}
	}
}

// CRC16 — контрольная сумма CC11xx: многочлен 0x8005, начальное значение 0xFFFF,
// без отражения бит и без финального XOR.
func CRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x8005
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// Encode возвращает уровни линии (0 или 1) кадра: preamble байт 0xAA, синхрослово,
// данные и CRC под PN9, postamble байт 0xAA. Данные — ShortDataLen или LongDataLen байт.
func Encode(data []byte, preamble, postamble int) ([]byte, error) {
	if len(data) != ShortDataLen && len(data) != LongDataLen {
		return nil, fmt.Errorf("%w: %d bytes, want %d or %d", ErrInvalidLength, len(data), ShortDataLen, LongDataLen)
	}
	frame := binary.BigEndian.AppendUint16(append([]byte(nil), data...), CRC16(data))
	Whiten(frame)

	levels := make([]byte, 0, (preamble+4+len(frame)+postamble)*8)
	for range preamble {
		levels = appendBits(levels, preambleByte)
	}
	for _, b := range binary.BigEndian.AppendUint32(nil, SyncWord) {
		levels = appendBits(levels, b)
	}
	for _, b := range frame {
		levels = appendBits(levels, b)
	}
	for range postamble {
		levels = appendBits(levels, preambleByte)
	}
	return levels, nil
}

// appendBits дописывает биты байта старшим вперёд, как их передаёт CC1125.
func appendBits(levels []byte, b byte) []byte {
	for i := 7; i >= 0; i-- {
		levels = append(levels, b>>i&1)
	}
	return levels
}
