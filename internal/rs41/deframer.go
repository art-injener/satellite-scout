package rs41

import "math/bits"

// syncMaxErrors — допуск по битам заголовка 64 бит на линии.
const syncMaxErrors = 6

// Deframer ищет scrambled-заголовок (LSB первым в байте), собирает 320 байт,
// снимает whitening, dual RS, отдаёт кадр при CRC статуса.
type Deframer struct {
	shift   uint64
	have    int // бит в shift (до 64)
	inv     bool
	inFrame bool
	cur     byte
	nbits   int
	buf     []byte
	good    int
	bad     int
	syncs   int
}

// Push дописывает в dst кадры с верным статусом (после ECC) и возвращает расширенный срез.
func (d *Deframer) Push(dst [][]byte, levels []byte) [][]byte {
	for _, lv := range levels {
		b := lv & 1
		if !d.inFrame {
			dst = d.hunt(dst, b)
			continue
		}
		// LSB первым: первый бит байта — младший.
		if b != 0 {
			d.cur |= 1 << uint(d.nbits)
		}
		d.nbits++
		if d.nbits < 8 {
			continue
		}
		d.buf = append(d.buf, d.cur)
		d.cur, d.nbits = 0, 0
		if len(d.buf) < StdFrameLen {
			continue
		}
		dst = d.finish(dst)
	}
	return dst
}

func (d *Deframer) hunt(dst [][]byte, b byte) [][]byte {
	d.shift = d.shift<<1 | uint64(b)
	if d.have < 64 {
		d.have++
	}
	if d.have < 64 {
		return dst
	}
	if d.matchHeader(d.shift, false) {
		return d.startFrame(dst, false)
	}
	if d.matchHeader(d.shift, true) {
		return d.startFrame(dst, true)
	}
	return dst
}

func (d *Deframer) matchHeader(sh uint64, inv bool) bool {
	want := headerBitsMSB()
	got := sh
	if inv {
		got = ^got
	}
	return bits.OnesCount64(got^want) <= syncMaxErrors
}

func (d *Deframer) startFrame(dst [][]byte, inv bool) [][]byte {
	d.syncs++
	d.inFrame = true
	d.inv = inv
	d.buf = d.buf[:0]
	d.cur, d.nbits = 0, 0
	// 64 бита заголовка уже в shift (как приняты). Инверсию линии снимем в finish.
	h := d.shift
	hdr := make([]byte, HeaderLen)
	for i := 0; i < HeaderLen; i++ {
		// Сдвиг копит MSB=первый бит; на линии байт LSB-first — Reverse8.
		chunk := byte(h >> uint(56-8*i))
		hdr[i] = bits.Reverse8(chunk)
	}
	d.buf = append(d.buf, hdr...)
	return dst
}

func headerBitsMSB() uint64 {
	// Биты заголовка 0x10B6… в хронологическом порядке, упакованные в uint64 MSB=первый бит.
	var u uint64
	for _, by := range ScrambledHeader {
		for i := 0; i < 8; i++ {
			u <<= 1
			if by&(1<<uint(i)) != 0 { // LSB first
				u |= 1
			}
		}
	}
	return u
}

func (d *Deframer) finish(dst [][]byte) [][]byte {
	raw := append([]byte(nil), d.buf[:StdFrameLen]...)
	if d.inv {
		for i := range raw {
			raw[i] = ^raw[i]
		}
	}
	d.endFrame()
	clear := Descramble(raw)
	if _, ok := CorrectFrame(clear); !ok {
		d.bad++
		return dst
	}
	if !FrameOK(clear) {
		d.bad++
		return dst
	}
	d.good++
	return append(dst, clear)
}

func (d *Deframer) endFrame() {
	d.inFrame = false
	d.have = 0
	d.shift = 0
	d.buf = d.buf[:0]
	d.cur, d.nbits = 0, 0
}

// Good — кадры с ECC и CRC статуса.
func (d *Deframer) Good() int { return d.good }

// Bad — синхро есть, кадр отвергнут.
func (d *Deframer) Bad() int { return d.bad }

// SyncWords — число найденных заголовков.
func (d *Deframer) SyncWords() int { return d.syncs }

// BytesToLevels — байты кадра на линии (уже scrambled) → уровни LSB-first.
func BytesToLevels(b []byte) []byte {
	out := make([]byte, 0, len(b)*8)
	for _, by := range b {
		for i := 0; i < 8; i++ {
			if by&(1<<uint(i)) != 0 {
				out = append(out, 1)
			} else {
				out = append(out, 0)
			}
		}
	}
	return out
}

// ScrambleFrame — clear → on-air (XOR).
func ScrambleFrame(clear []byte) []byte {
	out := append([]byte(nil), clear...)
	Whiten(out)
	return out
}
