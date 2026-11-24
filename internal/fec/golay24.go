package fec

import (
	"errors"
	"math/bits"
)

// ErrUncorrectable — кодовое слово Golay(24,12) содержит больше трёх ошибок.
var ErrUncorrectable = errors.New("fec: uncorrectable")

// Матрица H алгоритма Morelos-Zaragoza (The Art of Error Correcting Coding, §2.2.3);
// совпадает с gr-satellites/lib/golay24.c и jradio Golay.
var golayH = [...]uint32{
	0x8008ed, 0x4001db, 0x2003b5, 0x100769, 0x80ed1, 0x40da3,
	0x20b47, 0x1068f, 0x8d1d, 0x4a3b, 0x2477, 0x1ffe,
}

const golayN = 12

// EncodeGolay24 кодирует 12 бит данных в кодовое слово 24 бит:
// старшие 12 — чётность, младшие 12 — данные (как в encode_golay24 gr-satellites).
func EncodeGolay24(data uint32) uint32 {
	r := data & 0xfff
	var s uint32
	for i := 0; i < golayN; i++ {
		s <<= 1
		s |= uint32(bits.OnesCount32(golayH[i]&r) & 1)
	}
	return (s&0xfff)<<golayN | r
}

// DecodeGolay24 исправляет до трёх ошибок в 24-битном слове.
// Возвращает 12 бит данных и число исправленных бит.
func DecodeGolay24(word uint32) (data uint32, corrected int, err error) {
	r := word & 0xffffff
	var s uint32
	for i := 0; i < golayN; i++ {
		s <<= 1
		s |= uint32(bits.OnesCount32(golayH[i]&r) & 1)
	}

	var e uint32
	switch {
	case bits.OnesCount32(s) <= 3:
		e = s << golayN
	default:
		found := false
		for i := 0; i < golayN; i++ {
			b := golayH[i] & 0xfff
			if bits.OnesCount32(s^b) <= 2 {
				e = (s ^ b) << golayN
				e |= 1 << (golayN - i - 1)
				found = true
				break
			}
		}
		if !found {
			var q uint32
			for i := 0; i < golayN; i++ {
				q <<= 1
				q |= uint32(bits.OnesCount32((golayH[i]&0xfff)&s) & 1)
			}
			switch {
			case bits.OnesCount32(q) <= 3:
				e = q
				found = true
			default:
				for i := 0; i < golayN; i++ {
					b := golayH[i] & 0xfff
					if bits.OnesCount32(q^b) <= 2 {
						e = 1 << (2*golayN - i - 1)
						e |= q ^ b
						found = true
						break
					}
				}
			}
		}
		if !found {
			return 0, 0, ErrUncorrectable
		}
	}

	c := r ^ e
	return c & 0xfff, bits.OnesCount32(e), nil
}

// Golay24ToBytes упаковывает 24 бита старшим байтом вперёд (как U482C/AX100).
func Golay24ToBytes(word uint32) []byte {
	w := word & 0xffffff
	return []byte{byte(w >> 16), byte(w >> 8), byte(w)}
}

// BytesToGolay24 собирает 24 бита из трёх байт (старший первым).
func BytesToGolay24(b []byte) uint32 {
	if len(b) < 3 {
		return 0
	}
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}
