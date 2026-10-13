package ax25

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidCallsign = errors.New("invalid callsign")
	ErrInvalidSSID     = errors.New("invalid ssid")
)

const (
	callsignLen = 6
	maxSSID     = 15

	// Кадр UI: ненумерованная информация без протокола третьего уровня.
	controlUI = 0x03
	pidNoL3   = 0xF0

	flagByte = 0x7E
)

// Address — позывной станции с SSID (0–15).
type Address struct {
	Call string
	SSID uint8
}

// Frame — кадр UI без ретрансляторов: передача маяка КА.
type Frame struct {
	Dest Address
	Src  Address
	Info []byte
}

// Bytes собирает кадр: адреса, control, PID, информационное поле и FCS (младший байт первым).
func (f Frame) Bytes() ([]byte, error) {
	dest, err := encodeAddress(f.Dest, true, false)
	if err != nil {
		return nil, fmt.Errorf("dest: %w", err)
	}
	src, err := encodeAddress(f.Src, false, true)
	if err != nil {
		return nil, fmt.Errorf("src: %w", err)
	}

	out := make([]byte, 0, len(dest)+len(src)+2+len(f.Info)+2)
	out = append(out, dest...)
	out = append(out, src...)
	out = append(out, controlUI, pidNoL3)
	out = append(out, f.Info...)
	return binary.LittleEndian.AppendUint16(out, FCS(out)), nil
}

// encodeAddress кодирует позывной: символы сдвинуты на бит влево, дополнены пробелами до 6.
// command выставляет бит команды в SSID-байте, last — бит конца адресного поля.
func encodeAddress(a Address, command, last bool) ([]byte, error) {
	call := strings.ToUpper(a.Call)
	if call == "" || len(call) > callsignLen {
		return nil, fmt.Errorf("%w: %q", ErrInvalidCallsign, a.Call)
	}
	for _, c := range call {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return nil, fmt.Errorf("%w: %q", ErrInvalidCallsign, a.Call)
		}
	}
	if a.SSID > maxSSID {
		return nil, fmt.Errorf("%w: %d", ErrInvalidSSID, a.SSID)
	}

	out := make([]byte, callsignLen+1)
	for i := range callsignLen {
		c := byte(' ')
		if i < len(call) {
			c = call[i]
		}
		out[i] = c << 1
	}
	// Биты 5–6 SSID-байта зарезервированы и равны 1.
	ssid := byte(0x60) | a.SSID<<1
	if command {
		ssid |= 0x80
	}
	if last {
		ssid |= 0x01
	}
	out[callsignLen] = ssid
	return out, nil
}

// FCS считает CRC-16/X.25: отражённый полином 0x8408, начальное 0xFFFF, инверсия результата.
func FCS(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

// HDLCBits раскладывает кадр в биты (младший бит байта первым) со вставкой нуля
// после пяти единиц подряд и обрамляет флагами 0x7E: preamble в начале, postamble в конце.
// Флаги не проходят вставку нулей. Результат — срез из значений 0 и 1.
func HDLCBits(frame []byte, preamble, postamble int) []byte {
	bits := make([]byte, 0, (preamble+postamble)*8+len(frame)*8+len(frame)*8/5)
	for range preamble {
		bits = appendByteBits(bits, flagByte)
	}

	ones := 0
	for _, b := range frame {
		for i := range 8 {
			bit := b >> i & 1
			bits = append(bits, bit)
			if bit == 0 {
				ones = 0
				continue
			}
			ones++
			if ones == 5 {
				bits = append(bits, 0)
				ones = 0
			}
		}
	}

	for range postamble {
		bits = appendByteBits(bits, flagByte)
	}
	return bits
}

func appendByteBits(bits []byte, b byte) []byte {
	for i := range 8 {
		bits = append(bits, b>>i&1)
	}
	return bits
}

// NRZI кодирует биты в уровни линии: ноль меняет уровень, единица сохраняет.
// Начальный уровень — 0.
func NRZI(bits []byte) []byte {
	out := make([]byte, len(bits))
	level := byte(0)
	for i, b := range bits {
		if b == 0 {
			level ^= 1
		}
		out[i] = level
	}
	return out
}
