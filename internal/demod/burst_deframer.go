package demod

// Пороги выделителя пачек линии (OOK / FSK без кадрового протокола).
const (
	burstIdleBits = 32   // пауза «нет несущей» — конец пачки
	burstMinBits  = 32   // короче — шум, не отдаём
	burstMaxBits  = 4096 // принудительный срез длинной непрерывной линии
)

// burstDeframer режет поток уровней 0/1 на пачки: пауза из нулей или max длина.
// Короткие нули внутри пачки становятся данными только если снова пришёл mark.
// Good — число отданных пачек (не CRC). Bad всегда 0.
type burstDeframer struct {
	bits []byte
	idle int
	good int
}

func newBurstDeframer() deframer {
	return &burstDeframer{bits: make([]byte, 0, 256)}
}

func (d *burstDeframer) Good() int { return d.good }
func (d *burstDeframer) Bad() int  { return 0 }

func (d *burstDeframer) Push(dst [][]byte, levels []byte) [][]byte {
	for _, b := range levels {
		if b != 0 {
			for i := 0; i < d.idle; i++ {
				d.bits = append(d.bits, 0)
			}
			d.idle = 0
			d.bits = append(d.bits, 1)
			if len(d.bits) >= burstMaxBits {
				dst = d.emit(dst)
			}
			continue
		}
		if len(d.bits) == 0 && d.idle == 0 {
			continue // ведущие нули
		}
		d.idle++
		if d.idle >= burstIdleBits {
			dst = d.emit(dst)
			d.idle = 0
		}
	}
	return dst
}

// Flush отдаёт накопленную пачку при закрытии порога (без паузы в линии).
func (d *burstDeframer) Flush(dst [][]byte) [][]byte {
	// Хвост без закрывающей паузы: короткие pending-нули не данные.
	d.idle = 0
	return d.emit(dst)
}

func (d *burstDeframer) emit(dst [][]byte) [][]byte {
	n := len(d.bits)
	if n < burstMinBits {
		d.bits = d.bits[:0]
		return dst
	}
	raw := packLineBits(d.bits)
	d.bits = d.bits[:0]
	d.good++
	return append(dst, raw)
}

// packLineBits — биты 0/1 (старший первым в байте) в сырые байты.
func packLineBits(bits []byte) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		if b != 0 {
			out[i/8] |= 1 << (7 - uint(i%8))
		}
	}
	return out
}

// lineFlusher — выделитель, который отдаёт хвост при закрытии порога.
type lineFlusher interface {
	Flush(dst [][]byte) [][]byte
}
