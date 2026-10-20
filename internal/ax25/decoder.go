package ax25

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidFrame — байты с верной FCS не складываются в кадр UI.
var ErrInvalidFrame = errors.New("invalid ax25 frame")

const (
	// Кадр без информационного поля: два адреса, control и FCS.
	minFrameLen = 2*(callsignLen+1) + 1 + 2
	// Наибольший кадр: 10 адресов, control, PID, 256 байт информации и FCS.
	maxAddresses = 10
	maxFrameLen  = maxAddresses*(callsignLen+1) + 2 + 256 + 2
	// Флаг HDLC — ноль и шесть единиц до завершающего нуля: столько бит
	// попадает в кадр, пока флаг не распознан.
	flagHeadBits = 7
	// Бит P/F в поле control; кадр UI бывает с ним и без.
	controlPF = 0x10
)

// Deframer выделяет кадры HDLC из уровней линии демодулятора: снимает NRZI,
// ищет флаги 0x7E, удаляет вставленные нули, сбрасывает кадр на семи единицах
// подряд и проверяет FCS. Нулевое значение готово к работе; не потокобезопасен.
type Deframer struct {
	prev    byte
	ones    int
	inFrame bool
	bits    []byte // биты текущего кадра после удаления вставленных нулей

	good, bad int
}

// Push дописывает в dst кадры с верной FCS (без FCS) и возвращает расширенный срез.
// Возвращённые кадры принадлежат вызывающему.
func (d *Deframer) Push(dst [][]byte, levels []byte) [][]byte {
	for _, lv := range levels {
		lv &= 1
		// NRZI: уровень не изменился — единица, изменился — ноль.
		bit := byte(0)
		if lv == d.prev {
			bit = 1
		}
		d.prev = lv

		if bit == 1 {
			d.ones++
			if d.ones > 6 {
				d.inFrame = false // передатчик прервал кадр
			}
			d.appendBit(1)
			continue
		}
		switch d.ones {
		case 5: // вставленный ноль
		case 6:
			dst = d.closeFrame(dst)
			d.inFrame = true
			d.bits = d.bits[:0]
		default:
			d.appendBit(0)
		}
		d.ones = 0
	}
	return dst
}

func (d *Deframer) appendBit(b byte) {
	if !d.inFrame {
		return
	}
	d.bits = append(d.bits, b)
	if len(d.bits) > maxFrameLen*8+flagHeadBits {
		d.inFrame = false
	}
}

// closeFrame проверяет накопленное до флага и дописывает кадр в dst, если FCS верна.
// Битыми считаются только отрезки подходящей длины: на шуме их тоже набирается.
func (d *Deframer) closeFrame(dst [][]byte) [][]byte {
	if !d.inFrame {
		return dst
	}
	n := len(d.bits) - flagHeadBits
	if n < minFrameLen*8 || n%8 != 0 {
		return dst
	}
	frame := make([]byte, n/8)
	for i, b := range d.bits[:n] {
		frame[i/8] |= b << (i % 8)
	}
	body := frame[:len(frame)-2]
	if FCS(body) != binary.LittleEndian.Uint16(frame[len(frame)-2:]) {
		d.bad++
		return dst
	}
	d.good++
	return append(dst, body)
}

// Good — сколько кадров принято с верной FCS.
func (d *Deframer) Good() int { return d.good }

// Bad — сколько отрезков между флагами подходящей длины не прошли FCS.
func (d *Deframer) Bad() int { return d.bad }

// Parse разбирает кадр без FCS в кадр UI. Ретрансляторы в адресном поле
// пропускаются: у маяков КА их обычно нет, а информационное поле важнее пути.
func Parse(b []byte) (Frame, error) {
	end := 0
	for i := callsignLen; i < len(b) && i < maxAddresses*(callsignLen+1); i += callsignLen + 1 {
		if b[i]&0x01 != 0 {
			end = i + 1
			break
		}
	}
	if end < 2*(callsignLen+1) {
		return Frame{}, fmt.Errorf("%w: no end of address field", ErrInvalidFrame)
	}
	if len(b) < end+2 {
		return Frame{}, fmt.Errorf("%w: %d bytes, no control and PID", ErrInvalidFrame, len(b))
	}
	if b[end]&^controlPF != controlUI {
		return Frame{}, fmt.Errorf("%w: control 0x%02X is not UI", ErrInvalidFrame, b[end])
	}
	return Frame{
		Dest: decodeAddress(b[0 : callsignLen+1]),
		Src:  decodeAddress(b[callsignLen+1 : 2*(callsignLen+1)]),
		Info: append([]byte(nil), b[end+2:]...),
	}, nil
}

// decodeAddress — обратное к encodeAddress: символы сдвинуты на бит вправо, пробелы срезаны.
func decodeAddress(b []byte) Address {
	call := make([]byte, callsignLen)
	for i := range callsignLen {
		call[i] = b[i] >> 1
	}
	return Address{
		Call: strings.TrimRight(string(call), " "),
		SSID: b[callsignLen] >> 1 & maxSSID,
	}
}
