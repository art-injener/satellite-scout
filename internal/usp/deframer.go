package usp

import (
	"math/bits"

	"github.com/art-injener/satellite-scout/internal/fec"
)

// Deframer выделяет кадры USP из уровней линии (жёсткие биты 0/1).
type Deframer struct {
	shift     uint64
	inFrame   bool
	invert    bool // синхрослово найдено в инверсной полярности
	plsBuf    []byte
	coded     []byte
	needCoded int
	dataLen   int

	good, bad       int
	syncs, syncErrs int
	fecCorr         int // исправленные символы RS dual-basis
}

// Push дописывает в dst сырые кадры AX.25 (с FCS) после успешного RS.
func (d *Deframer) Push(dst [][]byte, levels []byte) [][]byte {
	for _, lv := range levels {
		b := lv & 1
		if !d.inFrame {
			d.shift = d.shift<<1 | uint64(b)
			errs := bits.OnesCount64(d.shift ^ SyncWord)
			if errs <= syncMaxErrors {
				d.syncs++
				d.syncErrs += errs
				d.inFrame, d.invert = true, false
				d.plsBuf = d.plsBuf[:0]
				d.coded = d.coded[:0]
				d.needCoded = 0
				d.dataLen = 0
			} else if invErrs := 64 - errs; invErrs <= syncMaxErrors {
				d.syncs++
				d.syncErrs += invErrs
				d.inFrame, d.invert = true, true
				d.plsBuf = d.plsBuf[:0]
				d.coded = d.coded[:0]
				d.needCoded = 0
				d.dataLen = 0
			}
			continue
		}
		if d.invert {
			b ^= 1
		}
		if len(d.plsBuf) < 64 {
			d.plsBuf = append(d.plsBuf, b)
			if len(d.plsBuf) < 64 {
				continue
			}
			dl, ok := dataLenForPLS(d.plsBuf)
			if !ok {
				d.bad++
				d.endFrame()
				continue
			}
			d.dataLen = dl
			d.needCoded = (dl + fec.RSParityLen) * 16 // бит после усечённого Viterbi
			continue
		}
		d.coded = append(d.coded, b)
		if len(d.coded) < d.needCoded {
			continue
		}
		if ax, ok := d.decodeFrame(d.coded[:d.needCoded]); ok {
			dst = append(dst, ax)
			d.good++
		} else {
			d.bad++
		}
		d.endFrame()
	}
	return dst
}

func (d *Deframer) decodeFrame(coded []byte) ([]byte, bool) {
	block := fec.DecodeViterbiCCSDSTruncated(coded)
	if len(block) != d.dataLen+fec.RSParityLen {
		return nil, false
	}
	fec.CCSDSScramble(block)
	inner, nCorr, err := fec.DecodeRSDual(block)
	if err != nil {
		return nil, false
	}
	d.fecCorr += nCorr
	return cropAX25(inner)
}

func (d *Deframer) endFrame() {
	d.inFrame = false
	d.invert = false
	d.shift = 0
	d.needCoded = 0
	d.dataLen = 0
	d.plsBuf = d.plsBuf[:0]
	d.coded = d.coded[:0]
}

func (d *Deframer) Good() int          { return d.good }
func (d *Deframer) Bad() int           { return d.bad }
func (d *Deframer) SyncWords() int     { return d.syncs }
func (d *Deframer) SyncBitErrors() int { return d.syncErrs }
func (d *Deframer) FECCorrected() int  { return d.fecCorr }
