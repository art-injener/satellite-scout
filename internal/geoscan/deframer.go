package geoscan

import (
	"encoding/binary"
	"math/bits"
)

// syncMaxErrors — сколько бит синхрослова может быть принято с ошибкой.
const syncMaxErrors = 4

// Deframer выделяет кадры GEOSCAN из уровней линии демодулятора: ищет синхрослово
// с допуском syncMaxErrors ошибок, снимает PN9 и проверяет CRC. Длину кадра аппарат
// не передаёт, поэтому CRC проверяется сначала после короткого кадра — он выходит сразу,
// не дожидаясь бит за ним, — затем после длинного. Нулевое значение готово к работе;
// не потокобезопасен.
type Deframer struct {
	shift   uint32 // последние 32 бита линии
	inFrame bool
	cur     byte // собираемый байт
	nbits   int  // бит в cur
	buf     []byte

	good, bad       int
	syncs, syncErrs int
}

// Push дописывает в dst данные кадров с верной CRC (без CRC) и возвращает расширенный срез.
// Возвращённые кадры принадлежат вызывающему.
func (d *Deframer) Push(dst [][]byte, levels []byte) [][]byte {
	for _, lv := range levels {
		b := lv & 1
		if !d.inFrame {
			d.shift = d.shift<<1 | uint32(b)
			if errs := bits.OnesCount32(d.shift ^ SyncWord); errs <= syncMaxErrors {
				d.syncs++
				d.syncErrs += errs
				d.inFrame = true
				d.buf, d.nbits = d.buf[:0], 0
			}
			continue
		}

		d.cur = d.cur<<1 | b
		d.nbits++
		if d.nbits < 8 {
			continue
		}
		d.buf = append(d.buf, d.cur)
		d.nbits = 0

		switch len(d.buf) {
		case ShortDataLen + crcLen:
			if data, ok := checkFrame(d.buf); ok {
				dst = append(dst, data)
				d.good++
				d.endFrame()
			}
		case LongDataLen + crcLen:
			if data, ok := checkFrame(d.buf); ok {
				dst = append(dst, data)
				d.good++
			} else {
				d.bad++
			}
			d.endFrame()
		}
	}
	return dst
}

// endFrame возвращает к поиску синхрослова; биты кадра в него не попадают.
func (d *Deframer) endFrame() {
	d.inFrame = false
	d.shift = 0
}

// checkFrame снимает PN9 с копии кадра и сверяет CRC; возвращает данные без CRC.
func checkFrame(frame []byte) ([]byte, bool) {
	b := append([]byte(nil), frame...)
	Whiten(b)
	n := len(b) - crcLen
	if CRC16(b[:n]) != binary.BigEndian.Uint16(b[n:]) {
		return nil, false
	}
	return b[:n:n], true
}

// Good — сколько кадров принято с верной CRC.
func (d *Deframer) Good() int { return d.good }

// Bad — сколько найденных синхрослов не дали кадра с верной CRC ни при одной длине.
func (d *Deframer) Bad() int { return d.bad }

// SyncWords — сколько синхрослов найдено.
func (d *Deframer) SyncWords() int { return d.syncs }

// SyncBitErrors — сколько бит с ошибкой было во всех найденных синхрословах:
// отношение к 32·SyncWords — оценка BER линии до CRC.
func (d *Deframer) SyncBitErrors() int { return d.syncErrs }
