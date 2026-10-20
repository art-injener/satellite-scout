package modem

// prbsCheckBlock — блок проверки синхронизации: если в нём ошибок больше четверти,
// приёмник потерял или вставил бит, и счётчик синхронизируется заново.
const prbsCheckBlock = 64

// PRBS9Checker считает ошибки в принятой PRBS-9. Первые 9 бит загружаются в свой
// генератор, дальше он идёт сам: ошибка в принятом бите не портит предсказание
// следующих, поэтому каждая ошибка считается один раз. Нулевое значение готово к работе.
type PRBS9Checker struct {
	gen    PRBS9
	seed   uint16
	seeded int
	locked bool

	bits, errors           int
	blockBits, blockErrors int
}

// Push проверяет очередные принятые биты (0 или 1).
func (c *PRBS9Checker) Push(bits []byte) {
	for _, b := range bits {
		b &= 1
		if !c.locked {
			c.seed = (c.seed<<1 | uint16(b)) & 0x1FF
			c.seeded = min(c.seeded+1, 9)
			// Все нули — тишина: генератор из нулевого состояния не выходит.
			if c.seeded < 9 || c.seed == 0 {
				continue
			}
			c.gen = PRBS9{state: c.seed}
			for range 9 {
				c.gen.Next()
			}
			c.locked = true
			c.blockBits, c.blockErrors = 0, 0
			continue
		}

		c.bits++
		c.blockBits++
		if b != c.gen.Next() {
			c.errors++
			c.blockErrors++
		}
		if c.blockBits == prbsCheckBlock {
			if c.blockErrors > prbsCheckBlock/4 {
				c.locked = false
				c.seeded = 0
			}
			c.blockBits, c.blockErrors = 0, 0
		}
	}
}

// Locked — счётчик синхронизирован с последовательностью.
func (c *PRBS9Checker) Locked() bool { return c.locked }

// Bits — сколько бит проверено (без бит, ушедших на синхронизацию).
func (c *PRBS9Checker) Bits() int { return c.bits }

// Errors — сколько проверенных бит не совпало с последовательностью.
func (c *PRBS9Checker) Errors() int { return c.errors }

// BER — доля ошибочных бит; 0, пока ничего не проверено.
func (c *PRBS9Checker) BER() float64 {
	if c.bits == 0 {
		return 0
	}
	return float64(c.errors) / float64(c.bits)
}
