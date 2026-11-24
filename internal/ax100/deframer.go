package ax100

import (
	"math/bits"

	"github.com/art-injener/satellite-scout/internal/fec"
)

// Deframer выделяет кадры AX.100 Mode 5 из уровней линии: синхрослово,
// Golay-длина, CCSDS-скремблер, RS. Нулевое значение готово к работе;
// не потокобезопасен.
type Deframer struct {
	shift   uint32
	inFrame bool
	invert  bool // синхрослово найдено в инверсной полярности
	cur     byte
	nbits   int
	buf     []byte
	need    int // байт полезной нагрузки после Golay; 0 — ещё читаем Golay

	good, bad       int
	syncs, syncErrs int
	fecCorr         int // исправленные символы/биты Golay+RS за сеанс
}

// Push дописывает в dst данные кадров после успешного RS (без чётности).
func (d *Deframer) Push(dst [][]byte, levels []byte) [][]byte {
	for _, lv := range levels {
		b := lv & 1
		if !d.inFrame {
			d.shift = d.shift<<1 | uint32(b)
			errs := bits.OnesCount32(d.shift ^ SyncWord)
			if errs <= syncMaxErrors {
				d.syncs++
				d.syncErrs += errs
				d.inFrame, d.invert = true, false
				d.buf, d.nbits, d.need = d.buf[:0], 0, 0
			} else if invErrs := 32 - errs; invErrs <= syncMaxErrors {
				d.syncs++
				d.syncErrs += invErrs
				d.inFrame, d.invert = true, true
				d.buf, d.nbits, d.need = d.buf[:0], 0, 0
			}
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

		if d.need == 0 {
			if len(d.buf) < golayBytes {
				continue
			}
			data, nCorr, err := fec.DecodeGolay24(fec.BytesToGolay24(d.buf[:golayBytes]))
			if err != nil {
				d.bad++
				d.endFrame()
				continue
			}
			d.fecCorr += nCorr
			// Как u482c: длина — младшие 8 бит 12-битного поля.
			n := int(data & 0xff)
			if n < MinPayload || n > MaxPayload {
				d.bad++
				d.endFrame()
				continue
			}
			d.need = n
			d.buf = d.buf[:0]
			continue
		}

		if len(d.buf) < d.need {
			continue
		}
		if data, nCorr, ok := checkPayload(d.buf[:d.need]); ok {
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

func checkPayload(scrambled []byte) ([]byte, int, bool) {
	b := append([]byte(nil), scrambled...)
	fec.CCSDSScramble(b)
	data, n, err := fec.DecodeRS(b)
	if err != nil {
		return nil, 0, false
	}
	return data, n, true
}

func (d *Deframer) endFrame() {
	d.inFrame = false
	d.invert = false
	d.shift = 0
	d.need = 0
	d.buf = d.buf[:0]
	d.nbits = 0
}

// Good — кадры с верным RS.
func (d *Deframer) Good() int { return d.good }

// Bad — синхрослово или Golay без верного кадра.
func (d *Deframer) Bad() int { return d.bad }

// SyncWords — число найденных синхрослов.
func (d *Deframer) SyncWords() int { return d.syncs }

// SyncBitErrors — сумма ошибок бит в синхрословах.
func (d *Deframer) SyncBitErrors() int { return d.syncErrs }

// FECCorrected — сумма исправлений Golay и RS с начала работы выделителя.
func (d *Deframer) FECCorrected() int { return d.fecCorr }
