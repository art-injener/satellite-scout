package ccsds

import (
	"math/bits"

	"github.com/art-injener/satellite-scout/internal/fec"
)

// Deframer выделяет CADU из уровней линии: ASM (и инверсию бит — захват Costas
// на 180°), снятие скремблера, RS dual-basis. Нулевое значение готово к работе;
// не потокобезопасен.
type Deframer struct {
	shift   uint32
	inFrame bool
	invert  bool
	cur     byte
	nbits   int
	buf     []byte

	good, bad       int
	syncs, syncErrs int
	fecCorr         int
}

// Push дописывает в dst данные кадров после успешного RS (без чётности, 223 байт).
func (d *Deframer) Push(dst [][]byte, levels []byte) [][]byte {
	for _, lv := range levels {
		b := lv & 1
		if !d.inFrame {
			d.shift = d.shift<<1 | uint32(b)
			d.trySync()
			continue
		}
		if d.invert {
			b ^= 1
		}
		d.cur = d.cur<<1 | b
		d.nbits++
		if d.nbits < 8 {
			continue
		}
		d.buf = append(d.buf, d.cur)
		d.nbits = 0
		if len(d.buf) < CodeblockLen {
			continue
		}
		if data, nCorr, ok := checkCodeblock(d.buf[:CodeblockLen]); ok {
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

func (d *Deframer) trySync() bool {
	errs := bits.OnesCount32(d.shift ^ SyncWord)
	if errs <= syncMaxErrors {
		d.syncs++
		d.syncErrs += errs
		d.inFrame = true
		d.invert = false
		d.buf, d.nbits = d.buf[:0], 0
		return true
	}
	errsInv := bits.OnesCount32(d.shift ^ ^SyncWord)
	if errsInv <= syncMaxErrors {
		d.syncs++
		d.syncErrs += errsInv
		d.inFrame = true
		d.invert = true
		d.buf, d.nbits = d.buf[:0], 0
		return true
	}
	return false
}

func checkCodeblock(scrambled []byte) ([]byte, int, bool) {
	b := append([]byte(nil), scrambled...)
	fec.CCSDSScramble(b)
	data, n, err := fec.DecodeRSDual(b)
	if err != nil {
		return nil, 0, false
	}
	return data, n, true
}

func (d *Deframer) endFrame() {
	d.inFrame = false
	d.shift = 0
	d.invert = false
	d.buf = d.buf[:0]
	d.nbits = 0
}

// Good — кадры с верным RS.
func (d *Deframer) Good() int { return d.good }

// Bad — синхрослово без верного кодового слова.
func (d *Deframer) Bad() int { return d.bad }

// SyncWords — число найденных ASM.
func (d *Deframer) SyncWords() int { return d.syncs }

// SyncBitErrors — сумма ошибок бит в ASM.
func (d *Deframer) SyncBitErrors() int { return d.syncErrs }

// FECCorrected — сумма исправлений RS с начала работы выделителя.
func (d *Deframer) FECCorrected() int { return d.fecCorr }
