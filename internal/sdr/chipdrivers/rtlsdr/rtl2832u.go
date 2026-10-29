// Адаптировано из GopherTrunk rtlsdr/ (Apache-2.0), e283207.
// https://github.com/MattCheramie/GopherTrunk
// Последовательности регистров совпадают с osmocom librtlsdr: это поведение чипа, на которое
// рассчитана его прошивка.

// Package rtlsdr — протокол донгла RTL-SDR: демодулятор RTL2832U и тюнер R820T/R820T2
// поверх управляющих запросов USB.
package rtlsdr

import (
	"fmt"
	"time"
)

// controller — управляющие запросы USB; их даёт usbdrivers.Device.
type controller interface {
	Control(requestType, request uint8, value, index uint16, data []byte, timeout time.Duration) (int, error)
}

// Тип запроса: у RTL2832U только запросы производителя к устройству.
const (
	reqOut uint8 = 0x40
	reqIn  uint8 = 0xc0
)

const ctrlTimeout = 300 * time.Millisecond

// Блоки регистров: номер блока идёт в старшем байте wIndex.
const (
	blockUSB uint8 = 1
	blockSys uint8 = 2
	blockI2C uint8 = 6
)

// Регистры блоков USB и системного.
const (
	regSysctl    uint16 = 0x2000
	regEPAMaxpkt uint16 = 0x2158
	regEPACtl    uint16 = 0x2148 // 0x1000 останавливает конечную точку, 0x0002 сбрасывает FIFO
	regDemodCtl  uint16 = 0x3000
	regDemodCtl1 uint16 = 0x300b
)

// xtalHz — опорный кварц RTL2832U; от него же работает тюнер R820T.
const xtalHz = 28_800_000

// Границы частоты дискретизации. Между 300 и 900 кГц резамплер не работает, а выше 3,2 МГц
// USB не успевает.
const (
	minSampleRate uint32 = 225_001
	maxSampleRate uint32 = 3_200_000
	gapLow        uint32 = 300_000
	gapHigh       uint32 = 900_000
)

// firDefault — фильтр перед децимацией: половина симметричного фильтра из 32 отводов,
// первые 8 — 8-битные, следующие 8 — 12-битные. Как у драйвера Windows для DAB/FM.
var firDefault = [16]int{
	-54, -36, -41, -40, -32, -14, 14, 53,
	101, 156, 215, 273, 327, 372, 404, 421,
}

// chip — регистры RTL2832U и его мост I2C к тюнеру. Не потокобезопасен: вызовы
// упорядочивает приёмник.
type chip struct {
	c        controller
	repeater bool
}

func newChip(c controller) *chip { return &chip{c: c} }

// validSampleRate — частота, на которую резамплер настраивается без артефактов.
func validSampleRate(hz uint32) bool {
	if hz < minSampleRate || hz > maxSampleRate {
		return false
	}
	return hz <= gapLow || hz > gapHigh
}

func (c *chip) out(value, index uint16, data []byte) error {
	n, err := c.c.Control(reqOut, 0, value, index, data, ctrlTimeout)
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("short write %d of %d bytes", n, len(data))
	}
	return nil
}

func (c *chip) in(value, index uint16, n int) ([]byte, error) {
	buf := make([]byte, n)
	got, err := c.c.Control(reqIn, 0, value, index, buf, ctrlTimeout)
	if err != nil {
		return nil, err
	}
	if got != n {
		return nil, fmt.Errorf("short read %d of %d bytes", got, n)
	}
	return buf, nil
}

// regBytes — значение регистра на проводе: два байта идут старшим вперёд.
func regBytes(val uint16, n int) []byte {
	if n == 2 {
		return []byte{byte(val >> 8), byte(val)}
	}
	return []byte{byte(val)}
}

func (c *chip) writeReg(block uint8, addr, val uint16, n int) error {
	if err := c.out(addr, uint16(block)<<8|0x10, regBytes(val, n)); err != nil {
		return fmt.Errorf("rtl2832u: write block %d reg 0x%04x: %w", block, addr, err)
	}
	return nil
}

// writeDemod пишет регистр демодулятора. Запись защёлкивается только следующим чтением
// регистра 0x01 страницы 0x0a, поэтому оно идёт сразу за ней.
func (c *chip) writeDemod(page uint8, addr, val uint16, n int) error {
	if err := c.out(addr<<8|0x20, 0x10|uint16(page), regBytes(val, n)); err != nil {
		return fmt.Errorf("rtl2832u: write demod page %d reg 0x%02x: %w", page, addr, err)
	}
	if _, err := c.in(0x01<<8|0x20, 0x0a, 1); err != nil {
		return fmt.Errorf("rtl2832u: commit demod page %d reg 0x%02x: %w", page, addr, err)
	}
	return nil
}

// demodStep — одна запись в регистр демодулятора при запуске.
type demodStep struct {
	page uint8
	addr uint16
	val  uint16
	n    int
}

// Запуск демодулятора до загрузки фильтра: сброс, без инверсии спектра, ПЧ и сдвиг DDC в ноль.
var demodBeforeFIR = []demodStep{
	{1, 0x01, 0x14, 1}, {1, 0x01, 0x10, 1},
	{1, 0x15, 0x00, 1}, {1, 0x16, 0x0000, 2},
	{1, 0x16, 0x00, 1}, {1, 0x17, 0x00, 1}, {1, 0x18, 0x00, 1},
	{1, 0x19, 0x00, 1}, {1, 0x1a, 0x00, 1}, {1, 0x1b, 0x00, 1},
}

// После фильтра: режим SDR без цифровой АРУ, без АРУ тракта и фильтра PID, нулевая ПЧ
// с подавлением постоянной составляющей и коррекцией IQ, без выхода 4,096 МГц.
var demodAfterFIR = []demodStep{
	{0, 0x19, 0x05, 1},
	{1, 0x93, 0xf0, 1}, {1, 0x94, 0x0f, 1},
	{1, 0x11, 0x00, 1}, {1, 0x04, 0x00, 1},
	{0, 0x61, 0x60, 1}, {0, 0x06, 0x80, 1},
	{1, 0xb1, 0x1b, 1}, {0, 0x0d, 0x83, 1},
}

// initBaseband включает USB-конечную точку и демодулятор и загружает фильтр.
func (c *chip) initBaseband() error {
	if err := c.writeReg(blockUSB, regSysctl, 0x09, 1); err != nil {
		return err
	}
	if err := c.writeReg(blockUSB, regEPAMaxpkt, 0x0002, 2); err != nil {
		return err
	}
	if err := c.writeReg(blockUSB, regEPACtl, 0x1002, 2); err != nil {
		return err
	}
	if err := c.writeReg(blockSys, regDemodCtl1, 0x22, 1); err != nil {
		return err
	}
	if err := c.writeReg(blockSys, regDemodCtl, 0xe8, 1); err != nil {
		return err
	}
	if err := c.runDemod(demodBeforeFIR); err != nil {
		return err
	}
	if err := c.setFIR(firDefault); err != nil {
		return err
	}
	return c.runDemod(demodAfterFIR)
}

func (c *chip) runDemod(steps []demodStep) error {
	for _, s := range steps {
		if err := c.writeDemod(s.page, s.addr, s.val, s.n); err != nil {
			return err
		}
	}
	return nil
}

// powerOff выключает демодулятор и АЦП.
func (c *chip) powerOff() error {
	return c.writeReg(blockSys, regDemodCtl, 0x20, 1)
}

// setFIR загружает фильтр: 8 байт по отводу, затем пары 12-битных отводов по 3 байта.
func (c *chip) setFIR(taps [16]int) error {
	var raw [20]byte
	for i := 0; i < 8; i++ {
		if taps[i] < -128 || taps[i] > 127 {
			return fmt.Errorf("rtl2832u: FIR tap %d = %d out of 8 bits", i, taps[i])
		}
		raw[i] = byte(taps[i])
	}
	for i := 0; i < 4; i++ {
		v0, v1 := taps[8+2*i], taps[9+2*i]
		if v0 < -2048 || v0 > 2047 || v1 < -2048 || v1 > 2047 {
			return fmt.Errorf("rtl2832u: FIR taps %d,%d out of 12 bits", 8+2*i, 9+2*i)
		}
		raw[8+3*i] = byte(v0 >> 4)
		raw[9+3*i] = byte(v0<<4) | byte(v1>>8)&0x0f
		raw[10+3*i] = byte(v1)
	}
	for i, b := range raw {
		if err := c.writeDemod(1, 0x1c+uint16(i), uint16(b), 1); err != nil {
			return err
		}
	}
	return nil
}

// setSampleRate настраивает резамплер и возвращает частоту, на которой он работает на деле.
// Отношение кварца к частоте хранится в 28 битах с шагом 4; бит 28 чип восстанавливает из бита 27.
func (c *chip) setSampleRate(hz uint32) (uint32, error) {
	if !validSampleRate(hz) {
		return 0, fmt.Errorf("rtl2832u: sample rate %d Hz outside %d–%d Hz or inside the %d–%d Hz gap",
			hz, minSampleRate, maxSampleRate, gapLow, gapHigh)
	}
	ratio := uint32(uint64(xtalHz) << 22 / uint64(hz))
	ratio &= 0x0fff_fffc
	full := ratio | (ratio&0x0800_0000)<<1
	actual := uint32(uint64(xtalHz) << 22 / uint64(full))

	if err := c.writeDemod(1, 0x9f, uint16(ratio>>16), 2); err != nil {
		return 0, err
	}
	if err := c.writeDemod(1, 0xa1, uint16(ratio), 2); err != nil {
		return 0, err
	}
	// Поправка частоты отсчётов в ppm: пока ноль.
	if err := c.writeDemod(1, 0x3f, 0x00, 1); err != nil {
		return 0, err
	}
	if err := c.writeDemod(1, 0x3e, 0x00, 1); err != nil {
		return 0, err
	}
	if err := c.writeDemod(1, 0x01, 0x14, 1); err != nil {
		return 0, err
	}
	if err := c.writeDemod(1, 0x01, 0x10, 1); err != nil {
		return 0, err
	}
	return actual, nil
}

// setIFFreq задаёт ПЧ тюнера: демодулятор переносит её в ноль, 22 бита со знаком минус.
func (c *chip) setIFFreq(hz uint32) error {
	v := -int32(uint64(hz) << 22 / xtalHz)
	if err := c.writeDemod(1, 0x19, uint16(v>>16)&0x3f, 1); err != nil {
		return err
	}
	if err := c.writeDemod(1, 0x1a, uint16(v>>8)&0xff, 1); err != nil {
		return err
	}
	return c.writeDemod(1, 0x1b, uint16(v)&0xff, 1)
}

// resetBuffer сбрасывает FIFO конечной точки перед потоком и снимает её остановку.
func (c *chip) resetBuffer() error {
	if err := c.writeReg(blockUSB, regEPACtl, 0x1002, 2); err != nil {
		return err
	}
	return c.writeReg(blockUSB, regEPACtl, 0x0000, 2)
}

// setRepeater открывает мост I2C к тюнеру. Пока он открыт, демодулятор шину не занимает.
func (c *chip) setRepeater(on bool) error {
	if c.repeater == on {
		return nil
	}
	val := uint16(0x10)
	if on {
		val = 0x18
	}
	if err := c.writeDemod(1, 0x01, val, 1); err != nil {
		return err
	}
	c.repeater = on
	return nil
}

// withRepeater выполняет обмен с тюнером при открытом мосте I2C и закрывает его после.
func (c *chip) withRepeater(fn func() error) error {
	if err := c.setRepeater(true); err != nil {
		return err
	}
	err := fn()
	if offErr := c.setRepeater(false); err == nil {
		err = offErr
	}
	return err
}

func (c *chip) i2cWrite(addr uint8, data []byte) error {
	if err := c.out(uint16(addr), uint16(blockI2C)<<8|0x10, data); err != nil {
		return fmt.Errorf("rtl2832u: i2c write 0x%02x: %w", addr, err)
	}
	return nil
}

func (c *chip) i2cRead(addr uint8, n int) ([]byte, error) {
	b, err := c.in(uint16(addr), uint16(blockI2C)<<8, n)
	if err != nil {
		return nil, fmt.Errorf("rtl2832u: i2c read 0x%02x: %w", addr, err)
	}
	return b, nil
}

// i2cReadReg читает один регистр тюнера: адрес регистра, затем байт.
func (c *chip) i2cReadReg(addr, reg uint8) (uint8, error) {
	if err := c.i2cWrite(addr, []byte{reg}); err != nil {
		return 0, err
	}
	b, err := c.i2cRead(addr, 1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}
