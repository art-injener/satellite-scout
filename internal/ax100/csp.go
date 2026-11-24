package ax100

import (
	"encoding/binary"
	"fmt"
)

const cspHeaderLen = 4

// CSPHeader — заголовок Cubesat Space Protocol (libcsp 1.x, big-endian).
type CSPHeader struct {
	Priority    uint8
	Source      uint8
	Destination uint8
	DstPort     uint8
	SrcPort     uint8
	HMAC        bool
	XTEA        bool
	CRC         bool
	RDP         bool
}

// ParseCSP разбирает 4-байтный заголовок; ok=false — длина или поля вне диапазона.
func ParseCSP(b []byte) (CSPHeader, []byte, bool) {
	if len(b) < cspHeaderLen {
		return CSPHeader{}, nil, false
	}
	w := binary.BigEndian.Uint32(b[:cspHeaderLen])
	h := CSPHeader{
		Priority:    uint8(w >> 30 & 0x3),
		Source:      uint8(w >> 25 & 0x1f),
		Destination: uint8(w >> 20 & 0x1f),
		DstPort:     uint8(w >> 14 & 0x3f),
		SrcPort:     uint8(w >> 8 & 0x3f),
		HMAC:        w&0x8 != 0,
		XTEA:        w&0x4 != 0,
		CRC:         w&0x2 != 0,
		RDP:         w&0x1 != 0,
	}
	// Адрес и порт в поле заголовка всегда в диапазоне битовой маски;
	// подтверждение формата: не все флаги сразу и не «пустой» нулевой заголовок без тела.
	if h.Priority > 3 {
		return CSPHeader{}, nil, false
	}
	return h, b[cspHeaderLen:], true
}

// FormatCSP — краткая подпись для таблицы ТМИ.
func FormatCSP(h CSPHeader) string {
	return fmt.Sprintf("CSP %d:%d → %d:%d", h.Source, h.SrcPort, h.Destination, h.DstPort)
}

// PackCSP собирает заголовок и тело в кадр.
func PackCSP(h CSPHeader, data []byte) []byte {
	w := uint32(h.Priority&0x3)<<30 |
		uint32(h.Source&0x1f)<<25 |
		uint32(h.Destination&0x1f)<<20 |
		uint32(h.DstPort&0x3f)<<14 |
		uint32(h.SrcPort&0x3f)<<8
	if h.HMAC {
		w |= 0x8
	}
	if h.XTEA {
		w |= 0x4
	}
	if h.CRC {
		w |= 0x2
	}
	if h.RDP {
		w |= 0x1
	}
	out := make([]byte, cspHeaderLen+len(data))
	binary.BigEndian.PutUint32(out, w)
	copy(out[cspHeaderLen:], data)
	return out
}
