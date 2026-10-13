package modem

// PRBS9Period — длина периода PRBS-9 в битах.
const PRBS9Period = 511

// PRBS9 — псевдослучайная последовательность x⁹+x⁵+1 (ITU-T O.150), начальное состояние — все единицы.
// Последовательность известна заранее, поэтому по ней приёмник считает BER.
type PRBS9 struct {
	state uint16
}

// NewPRBS9 создаёт генератор с начала последовательности.
func NewPRBS9() *PRBS9 {
	return &PRBS9{state: 0x1FF}
}

// Next возвращает очередной бит (0 или 1).
func (p *PRBS9) Next() byte {
	out := byte(p.state>>8) & 1
	fb := (p.state>>8 ^ p.state>>4) & 1
	p.state = (p.state<<1 | fb) & 0x1FF
	return out
}
