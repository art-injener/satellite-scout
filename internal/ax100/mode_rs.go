package ax100

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	"github.com/art-injener/satellite-scout/internal/fec"
	"github.com/art-injener/satellite-scout/internal/modem"
)

// Mode RS (Reed Solomon): весь поток, включая синхрослово, под G3RUH;
// после дескремблера — ASM, байт длины L, затем L−1 байт RS-кодового слова.
// Референс: gr-satellites ax100_deframer(mode=RS) / ax100_decode.

const (
	rsMinL = fec.RSParityLen + 2 // длина + ≥1 байт данных + 32 чётности
	rsMaxL = 255
)

// EncodeRSMode собирает уровни линии Mode RS: preamble+sync+[L|data|parity],
// всё под G3RUH. data — кадр до RS (обычно CSP).
func EncodeRSMode(data []byte, preamble, postamble int) ([]byte, error) {
	if len(data) == 0 || len(data) > fec.RSDataLen-1 {
		// −1: в блоке ещё байт длины, кодовое слово не длиннее 254.
		return nil, fmt.Errorf("ax100 rs: data length %d", len(data))
	}
	parity, err := fec.EncodeRS(data)
	if err != nil {
		return nil, err
	}
	coded := append(append([]byte(nil), data...), parity...)
	l := len(coded) + 1
	if l < rsMinL || l > rsMaxL {
		return nil, fmt.Errorf("ax100 rs: frame length %d", l)
	}
	body := append([]byte{byte(l)}, coded...)

	clear := make([]byte, 0, (preamble+4+len(body)+postamble)*8)
	for range preamble {
		clear = appendBits(clear, preambleByte)
	}
	for _, b := range binary.BigEndian.AppendUint32(nil, SyncWord) {
		clear = appendBits(clear, b)
	}
	for _, b := range body {
		clear = appendBits(clear, b)
	}
	for range postamble {
		clear = appendBits(clear, preambleByte)
	}
	var s modem.G3RUHScrambler
	return s.Scramble(nil, clear), nil
}

// RSDeframer — Mode RS: G3RUH на входе, затем sync + длина + RS.
// Нулевое значение готово к работе; не потокобезопасен.
type RSDeframer struct {
	desc modem.G3RUHDescrambler
	bits []byte // буфер дескремблированных бит (переиспользуется)

	shift   uint32
	inFrame bool
	cur     byte
	nbits   int
	buf     []byte
	need    int // полная длина кадра L (включая байт длины); 0 — ещё нет длины

	good, bad       int
	syncs, syncErrs int
	fecCorr         int
}

// Push принимает уровни линии (ещё под G3RUH) и отдаёт CSP после RS.
func (d *RSDeframer) Push(dst [][]byte, levels []byte) [][]byte {
	d.bits = d.desc.Descramble(d.bits[:0], levels)
	for _, b := range d.bits {
		b &= 1
		if !d.inFrame {
			d.shift = d.shift<<1 | uint32(b)
			if errs := bits.OnesCount32(d.shift ^ SyncWord); errs <= syncMaxErrors {
				d.syncs++
				d.syncErrs += errs
				d.inFrame = true
				d.buf, d.nbits, d.need = d.buf[:0], 0, 0
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

		if d.need == 0 {
			l := int(d.buf[0])
			if l < rsMinL || l > rsMaxL {
				d.bad++
				d.endFrame()
				continue
			}
			d.need = l
		}
		if len(d.buf) < d.need {
			continue
		}
		if data, nCorr, ok := checkRSMode(d.buf[:d.need]); ok {
			dst = append(dst, data)
			d.good++
			d.fecCorr += nCorr
		} else {
			d.bad++
		}
		d.endFrame()
	}
	return dst
}

func checkRSMode(frame []byte) ([]byte, int, bool) {
	l := int(frame[0])
	if l != len(frame) || l < rsMinL {
		return nil, 0, false
	}
	// Как ax100_decode: RS на кадре без байта длины, pad = 255 − L + 1.
	block := frame[1:l]
	data, n, err := fec.DecodeRS(block)
	if err != nil {
		return nil, 0, false
	}
	return data, n, true
}

func (d *RSDeframer) endFrame() {
	d.inFrame = false
	d.shift = 0
	d.need = 0
	d.buf = d.buf[:0]
	d.nbits = 0
}

func (d *RSDeframer) Good() int          { return d.good }
func (d *RSDeframer) Bad() int           { return d.bad }
func (d *RSDeframer) SyncWords() int     { return d.syncs }
func (d *RSDeframer) SyncBitErrors() int { return d.syncErrs }
func (d *RSDeframer) FECCorrected() int  { return d.fecCorr }
