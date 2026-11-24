package m10

import "math/bits"

// Заголовок всего 16 бит — допуск 0, иначе ложные синхро на шуме.
const syncMaxErrors = 0

// Deframer ищет заголовок 0x64 0x9F (MSB первым в байте), собирает 101 байт, checksum.
type Deframer struct {
	shift   uint16
	have    int
	inv     bool
	inFrame bool
	cur     byte
	nbits   int
	buf     []byte
	good    int
	bad     int
	syncs   int
}

// Push дописывает в dst кадры с верным checksum.
func (d *Deframer) Push(dst [][]byte, levels []byte) [][]byte {
	for _, lv := range levels {
		b := lv & 1
		if !d.inFrame {
			dst = d.hunt(dst, b)
			continue
		}
		d.cur = d.cur<<1 | b // MSB первым
		d.nbits++
		if d.nbits < 8 {
			continue
		}
		d.buf = append(d.buf, d.cur)
		d.cur, d.nbits = 0, 0
		if len(d.buf) < FrameLen {
			continue
		}
		dst = d.finish(dst)
	}
	return dst
}

func (d *Deframer) hunt(dst [][]byte, b byte) [][]byte {
	d.shift = d.shift<<1 | uint16(b)
	if d.have < headerBits {
		d.have++
	}
	if d.have < headerBits {
		return dst
	}
	if d.match(d.shift, false) {
		return d.start(dst, false)
	}
	if d.match(d.shift, true) {
		return d.start(dst, true)
	}
	return dst
}

func (d *Deframer) match(sh uint16, inv bool) bool {
	want := uint16(StdLenByte)<<8 | uint16(TypeM10)
	got := sh
	if inv {
		got = ^got
	}
	return bits.OnesCount16(got^want) <= syncMaxErrors
}

func (d *Deframer) start(dst [][]byte, inv bool) [][]byte {
	d.syncs++
	d.inFrame = true
	d.inv = inv
	d.buf = d.buf[:0]
	d.cur, d.nbits = 0, 0
	// Заголовок как принят; инверсию линии снимем в finish.
	h := d.shift
	d.buf = append(d.buf, byte(h>>8), byte(h))
	return dst
}

func (d *Deframer) finish(dst [][]byte) [][]byte {
	raw := append([]byte(nil), d.buf[:FrameLen]...)
	if d.inv {
		for i := range raw {
			raw[i] = ^raw[i]
		}
	}
	d.end()
	if !FrameOK(raw) {
		d.bad++
		return dst
	}
	d.good++
	return append(dst, raw)
}

func (d *Deframer) end() {
	d.inFrame = false
	d.have = 0
	d.shift = 0
	d.buf = d.buf[:0]
	d.cur, d.nbits = 0, 0
}

func (d *Deframer) Good() int { return d.good }
func (d *Deframer) Bad() int  { return d.bad }
func (d *Deframer) SyncWords() int { return d.syncs }

// BytesToLevels — байты → уровни MSB-first.
func BytesToLevels(b []byte) []byte {
	out := make([]byte, 0, len(b)*8)
	for _, by := range b {
		for i := 7; i >= 0; i-- {
			if by&(1<<uint(i)) != 0 {
				out = append(out, 1)
			} else {
				out = append(out, 0)
			}
		}
	}
	return out
}
