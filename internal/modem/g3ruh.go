package modem

// Скремблер G3RUH — многочлен 1 + x^12 + x^17, стандарт AX.25 на 9600 Бод. Убирает длинные
// серии одинаковых бит: без них у FSK не уходит порог решения и держится синхронизация тактов.
// Самосинхронизирующийся: приёмнику не нужно знать начальное состояние, он входит в синхронизм
// через 17 бит, а одна ошибка на линии даёт три ошибки после дескремблера.
const (
	g3ruhTap12 = 11 // бит регистра с выходом 12 тактов назад
	g3ruhTap17 = 16 // бит регистра с выходом 17 тактов назад
)

// G3RUHScrambler — скремблер передатчика; нулевое значение готово к работе.
type G3RUHScrambler struct {
	state uint32 // последние выходные биты, младший — предыдущий
}

// Scramble дописывает в dst скремблированные биты (0 или 1) и возвращает расширенный срез.
func (s *G3RUHScrambler) Scramble(dst, bits []byte) []byte {
	for _, b := range bits {
		out := (b ^ byte(s.state>>g3ruhTap12) ^ byte(s.state>>g3ruhTap17)) & 1
		s.state = s.state<<1 | uint32(out)
		dst = append(dst, out)
	}
	return dst
}

// G3RUHDescrambler — дескремблер приёмника; нулевое значение готово к работе.
type G3RUHDescrambler struct {
	state uint32 // последние принятые биты, младший — предыдущий
}

// Descramble дописывает в dst восстановленные биты и возвращает расширенный срез.
func (d *G3RUHDescrambler) Descramble(dst, bits []byte) []byte {
	for _, b := range bits {
		b &= 1
		dst = append(dst, (b^byte(d.state>>g3ruhTap12)^byte(d.state>>g3ruhTap17))&1)
		d.state = d.state<<1 | uint32(b)
	}
	return dst
}
