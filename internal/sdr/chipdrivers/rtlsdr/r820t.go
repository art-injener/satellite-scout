// Адаптировано из GopherTrunk rtlsdr/tuners (Apache-2.0), e283207.
// https://github.com/MattCheramie/GopherTrunk
// Значения регистров, таблица входов и расчёт синтезатора — как в osmocom tuner_r82xx.c.

package rtlsdr

import (
	"errors"
	"fmt"
	"log/slog"
)

const (
	// r820tAddr — адрес R820T и R820T2 на шине I2C за мостом RTL2832U.
	r820tAddr uint8 = 0x34
	// r820tChipID — регистр 0 так, как он приходит по проводу, без разворота бит.
	r820tChipID byte = 0x69

	// r820tIFHz — ПЧ тюнера при заводском фильтре около 6 МГц.
	r820tIFHz = 3_570_000

	// Диапазон перестройки: снизу ограничивает делитель 64 смесителя, сверху — делитель 2.
	r820tMinHz uint32 = 24_100_000
	r820tMaxHz uint32 = 1_766_000_000

	// Рабочий диапазон ГУН.
	r820tVCOMin = 1_770_000_000
	r820tVCOMax = 2 * r820tVCOMin

	// r820tMaxBurst — данных в одной записи I2C: мост RTL2832U берёт 8 байт вместе с адресом регистра.
	r820tMaxBurst = 7

	r820tShadowStart = 0x05
)

// r820tInitRegs — стартовые значения регистров 0x05..0x1f.
var r820tInitRegs = [32 - r820tShadowStart]byte{
	0x83, 0x32, 0x75,
	0xc0, 0x40, 0xd6, 0x6c,
	0xf5, 0x63, 0x75, 0x68,
	0x6c, 0x83, 0x80, 0x00,
	0x0f, 0x00, 0xc0, 0x30,
	0x48, 0xcc, 0x60, 0x00,
	0x54, 0xae, 0x4a, 0xc0,
}

// r820tBand — вход и следящий фильтр для участка частот гетеродина до upToHz (не включая).
type r820tBand struct {
	upToHz uint32
	openD  byte // 0x17, бит 3
	rfMux  byte // 0x1a, биты 0xc3
	tfC    byte // 0x1b
}

var r820tBands = []r820tBand{
	{50_000_000, 0x08, 0x02, 0xdf},
	{55_000_000, 0x08, 0x02, 0xbe},
	{60_000_000, 0x08, 0x02, 0x8b},
	{65_000_000, 0x08, 0x02, 0x7b},
	{70_000_000, 0x08, 0x02, 0x69},
	{75_000_000, 0x00, 0x02, 0x58},
	{80_000_000, 0x00, 0x02, 0x44},
	{90_000_000, 0x00, 0x02, 0x34},
	{100_000_000, 0x00, 0x02, 0x24},
	{110_000_000, 0x00, 0x02, 0x24},
	{120_000_000, 0x00, 0x02, 0x14},
	{140_000_000, 0x00, 0x02, 0x13},
	{180_000_000, 0x00, 0x02, 0x11},
	{220_000_000, 0x00, 0x02, 0x00},
	{250_000_000, 0x00, 0x02, 0x00},
	{280_000_000, 0x00, 0x02, 0x00},
	{310_000_000, 0x00, 0x02, 0x00},
	{450_000_000, 0x00, 0x41, 0x00},
	{588_000_000, 0x00, 0x40, 0x00},
	{650_000_000, 0x00, 0x40, 0x00},
	{^uint32(0), 0x00, 0x40, 0x00},
}

// Шаги усиления малошумящего усилителя и смесителя, десятые дБ; индекс — значение регистра.
var (
	r820tLNASteps   = [16]int{0, 9, 13, 40, 38, 13, 31, 22, 26, 31, 26, 14, 19, 5, 35, 13}
	r820tMixerSteps = [16]int{0, 5, 10, 10, 19, 9, 10, 25, 17, 10, 8, 16, 13, 6, 3, -8}
)

// Каскад ПЧ: при ручном усилении 16,3 дБ, при АРУ 26,5 дБ.
const (
	r820tVGAManual byte = 0x08
	r820tVGAAuto   byte = 0x0b
)

// r820tStandbyRegs — регистры отключения питания узлов тюнера.
var r820tStandbyRegs = []struct{ reg, val byte }{
	{0x06, 0xb1}, {0x05, 0xa0}, {0x07, 0x3a}, {0x08, 0x40}, {0x09, 0xc0}, {0x0a, 0x36},
	{0x0c, 0x35}, {0x0f, 0x68}, {0x11, 0x03}, {0x17, 0xf4}, {0x19, 0x0c},
}

var errPLLNoDivider = errors.New("r820t: no mixer divider puts the VCO in range")

// r820t — тюнер R820T/R820T2. Записываемые регистры обратно не читаются, поэтому их
// значения хранятся в тени; запись того же значения пропускается.
type r820t struct {
	chip *chip
	regs [32]byte
	// ifHz — промежуточная частота текущего фильтра. Пока фильтр заводской, это 3,57 МГц.
	ifHz uint32
}

func newR820T(c *chip) *r820t { return &r820t{chip: c, ifHz: r820tIFHz} }

// detectR820T проверяет, что за мостом I2C отвечает R820T.
func detectR820T(c *chip) bool {
	var id byte
	err := c.withRepeater(func() error {
		var err error
		id, err = c.i2cReadReg(r820tAddr, 0x00)
		return err
	})
	return err == nil && id == r820tChipID
}

// bitReverse разворачивает биты байта: так тюнер отдаёт регистры при чтении.
func bitReverse(b byte) byte {
	b = b>>4 | b<<4
	b = (b&0xcc)>>2 | (b&0x33)<<2
	return (b&0xaa)>>1 | (b&0x55)<<1
}

// prepareDemod переводит демодулятор на низкую ПЧ тюнера: без нулевой ПЧ, только вход I,
// ПЧ 3,57 МГц и инверсия спектра.
func prepareDemod(c *chip) error {
	if err := c.writeDemod(1, 0xb1, 0x1a, 1); err != nil {
		return err
	}
	if err := c.writeDemod(0, 0x08, 0x4d, 1); err != nil {
		return err
	}
	if err := c.setIFFreq(r820tIFHz); err != nil {
		return err
	}
	return c.writeDemod(1, 0x15, 0x01, 1)
}

// init загружает стартовые регистры.
func (t *r820t) init() error {
	return t.chip.withRepeater(func() error {
		copy(t.regs[r820tShadowStart:], r820tInitRegs[:])
		if err := t.write(r820tShadowStart, r820tInitRegs[:]); err != nil {
			return fmt.Errorf("r820t init: %w", err)
		}
		return nil
	})
}

// standby отключает питание узлов тюнера; обратно включает init.
func (t *r820t) standby() error {
	return t.chip.withRepeater(func() error {
		for _, r := range r820tStandbyRegs {
			if err := t.writeReg(r.reg, r.val); err != nil {
				return fmt.Errorf("r820t standby: %w", err)
			}
		}
		return nil
	})
}

// setFreq перестраивает гетеродин на hz плюс ПЧ.
func (t *r820t) setFreq(hz uint32) error {
	if hz < r820tMinHz || hz > r820tMaxHz {
		return fmt.Errorf("r820t: %d Hz outside %d–%d Hz", hz, r820tMinHz, r820tMaxHz)
	}
	lo := hz + t.ifHz
	return t.chip.withRepeater(func() error {
		if err := t.setMux(lo); err != nil {
			return fmt.Errorf("r820t mux: %w", err)
		}
		if err := t.setPLL(lo); err != nil {
			return fmt.Errorf("r820t pll %d Hz: %w", lo, err)
		}
		return nil
	})
}

// r820tBlogIFHz — ПЧ, вокруг которой драйвер RTL-SDR Blog строит фильтр.
// На 3,57 МГц таблица углов уже не попадает в калиброванные ступени, и полоса
// либо дырявая, либо пропускает соседа за Найквистом.
const r820tBlogIFHz = 6_000_000

// setBandwidth ставит фильтр ПЧ шириной bw симметрично вокруг 6 МГц и запоминает эту ПЧ.
// Вызывающий переводит на неё демодулятор до перестройки гетеродина.
// Верхний угол делится на 1000, а не на 1100 как у Blog: с 1100 угол ФНЧ уходит внутрь
// окна, и нижняя по частоте половина спектра проваливается.
func (t *r820t) setBandwidth(bw uint32) (uint32, error) {
	half := int(bw / 2)
	hpf := max(0, (r820tBlogIFHz-half)/1000)
	lpf := (r820tBlogIFHz + half) / 1000
	if err := t.setIFFilter(r820tBlogIFHz, hpf, lpf); err != nil {
		return 0, err
	}
	return r820tBlogIFHz, nil
}

// setIFFilter ставит углы фильтра ПЧ hpf и lpf в кГц и запоминает ПЧ ifHz для гетеродина.
func (t *r820t) setIFFilter(ifHz uint32, hpf, lpf int) error {
	reg0a, reg0b := r820tIFFilter(hpf, lpf)
	err := t.chip.withRepeater(func() error {
		if err := t.writeMask(0x0a, reg0a, 0x1f); err != nil {
			return err
		}
		return t.writeMask(0x0b, reg0b, 0xef)
	})
	if err != nil {
		return fmt.Errorf("r820t bandwidth: %w", err)
	}
	t.ifHz = ifHz
	return nil
}

// r820tIFFilter повторяет r82xx_set_if_filter из драйвера RTL-SDR Blog: углы ПЧ в кГц
// переводит в ступень ФВЧ и код настройки ФНЧ.
func r820tIFFilter(hpf, lpf int) (reg0a, reg0b byte) {
	var hpCor, cal int
	switch {
	case lpf <= 2500:
		hpCor, cal = 0xe0, 16*(2500-lpf)/(2500-2000)
	case lpf <= 3100:
		hpCor, cal = 0xa0, 16*(3100-lpf)/(3100-2200)
	case lpf <= 3900:
		hpCor, cal = 0x80, 16*(3900-lpf)/(3900-2600)
	case lpf <= 7100:
		hpCor, cal = 0x60, 16*(7100-lpf)/(7100-5300)
	case lpf <= 8700:
		hpCor, cal = 0x20, 16*(8700-lpf)/(8700-6300)
	default:
		hpCor, cal = 0x00, 16*(11000-lpf)/(11000-7500)
	}
	switch {
	case hpf >= 4700:
	case hpf >= 3800:
		hpCor |= 0x01
	case hpf >= 3000:
		hpCor |= 0x02
	case hpf >= 2800:
		hpCor |= 0x03
	case hpf >= 2600:
		hpCor |= 0x04
	case hpf >= 2400:
		hpCor |= 0x05
	case hpf >= 2200:
		hpCor |= 0x06
	case hpf >= 2000:
		hpCor |= 0x07
	case hpf >= 1800:
		hpCor |= 0x08
	case hpf >= 1600:
		hpCor |= 0x09
	case hpf >= 1400:
		hpCor |= 0x0a
	default:
		hpCor |= 0x0b
	}
	if cal < 0 {
		cal = 0
	} else if cal > 15 {
		cal = 15
	}
	return byte(0x10 | cal), byte(hpCor)
}

// ifBandwidthForRate — полоса фильтра ПЧ при частоте дискретизации rate. Углы стоят на fs − годная/2
// от центра: сигнал ближе к краю складывается через Найквиста только в отрезанные края окна,
// а дальше его уже режет фильтр. Шире нельзя: зеркала попадут в годную середину; уже — завал в ней.
func ifBandwidthForRate(rate uint32) uint32 {
	return 2*rate - usableBand(rate)
}

// setMux выбирает вход и следящий фильтр по частоте гетеродина.
func (t *r820t) setMux(lo uint32) error {
	b := r820tBands[len(r820tBands)-1]
	for _, c := range r820tBands {
		if lo < c.upToHz {
			b = c
			break
		}
	}
	if err := t.writeMask(0x17, b.openD, 0x08); err != nil {
		return err
	}
	if err := t.writeMask(0x1a, b.rfMux, 0xc3); err != nil {
		return err
	}
	if err := t.writeReg(0x1b, b.tfC); err != nil {
		return err
	}
	// Ёмкость кварца 0 пФ.
	if err := t.writeMask(0x10, 0x00, 0x0b); err != nil {
		return err
	}
	if err := t.writeMask(0x08, 0x00, 0x3f); err != nil {
		return err
	}
	return t.writeMask(0x09, 0x00, 0x3f)
}

// setPLL настраивает синтезатор: ГУН = гетеродин × делитель смесителя
// = 2 × кварц × (nint + sdm/65536).
func (t *r820t) setPLL(lo uint32) error {
	const vcoPowerRef = 2
	loKHz := (lo + 500) / 1000
	xtal := uint64(t.chip.xtal())
	refKHz := uint32((xtal + 500) / 1000)

	if err := t.writeMask(0x10, 0x00, 0x10); err != nil {
		return err
	}
	// Подстройка ФАПЧ с шагом 128 кГц на время захвата.
	if err := t.writeMask(0x1a, 0x00, 0x0c); err != nil {
		return err
	}
	if err := t.writeMask(0x12, 0x80, 0xe0); err != nil {
		return err
	}

	mixDiv, divNum := uint32(2), byte(0)
	for ; mixDiv <= 64; mixDiv, divNum = mixDiv<<1, divNum+1 {
		if v := loKHz * mixDiv; v >= r820tVCOMin/1000 && v < r820tVCOMax/1000 {
			break
		}
	}
	if mixDiv > 64 {
		return errPLLNoDivider
	}

	// Делитель поправляется по тому, у какого края диапазона оказался ГУН.
	status, err := t.read(5)
	if err != nil {
		return err
	}
	switch fine := (status[4] & 0x30) >> 4; {
	case fine > vcoPowerRef && divNum > 0:
		divNum--
	case fine < vcoPowerRef:
		divNum++
	}
	if err := t.writeMask(0x10, divNum<<5, 0xe0); err != nil {
		return err
	}

	vco := uint64(lo) * uint64(mixDiv)
	nint := uint32(vco / (2 * xtal))
	fraKHz := uint32((vco - 2*xtal*uint64(nint)) / 1000)
	if nint > 128/vcoPowerRef-1 {
		return fmt.Errorf("r820t: nint %d out of range", nint)
	}
	ni := byte((nint - 13) / 4)
	si := byte(nint - 4*uint32(ni) - 13)
	if err := t.writeReg(0x14, ni+si<<6); err != nil {
		return err
	}
	// Без дробной части сигма-дельта модулятор выключается.
	pwSDM := byte(0x00)
	if fraKHz == 0 {
		pwSDM = 0x08
	}
	if err := t.writeMask(0x12, pwSDM, 0x08); err != nil {
		return err
	}

	var sdm uint32
	for n := uint32(2); fraKHz > 1 && n <= 0x8000; n <<= 1 {
		if step := 2 * refKHz / n; fraKHz > step {
			sdm += 32768 / (n / 2)
			fraKHz -= step
		}
	}
	if err := t.writeReg(0x16, byte(sdm>>8)); err != nil {
		return err
	}
	if err := t.writeReg(0x15, byte(sdm)); err != nil {
		return err
	}

	for attempt := 0; ; attempt++ {
		st, err := t.read(3)
		if err != nil {
			return err
		}
		if st[2]&0x40 != 0 {
			break
		}
		if attempt > 0 {
			// Как и librtlsdr, перестройку не отменяем: часто захват приходит позже.
			slog.Warn("r820t: PLL not locked", slog.Uint64("lo_hz", uint64(lo)))
			return nil
		}
		// Не захватил: ток ГУН больше.
		if err := t.writeMask(0x12, 0x60, 0xe0); err != nil {
			return err
		}
	}
	// После захвата подстройка с шагом 8 кГц.
	return t.writeMask(0x1a, 0x08, 0x08)
}

// setGain включает АРУ или ставит ручное усиление tenthDB (десятые дБ): ступени
// малошумящего усилителя и смесителя растут по очереди, пока сумма не достигнет цели.
func (t *r820t) setGain(manual bool, tenthDB int) error {
	return t.chip.withRepeater(func() error {
		if !manual {
			if err := t.writeMask(0x05, 0x00, 0x10); err != nil {
				return err
			}
			if err := t.writeMask(0x07, 0x10, 0x10); err != nil {
				return err
			}
			return t.writeMask(0x0c, r820tVGAAuto, 0x9f)
		}
		if err := t.writeMask(0x05, 0x10, 0x10); err != nil {
			return err
		}
		if err := t.writeMask(0x07, 0x00, 0x10); err != nil {
			return err
		}
		if err := t.writeMask(0x0c, r820tVGAManual, 0x9f); err != nil {
			return err
		}
		lna, mix, total := 0, 0, 0
		for i := 0; i < 15 && total < tenthDB; i++ {
			lna++
			total += r820tLNASteps[lna]
			if total >= tenthDB {
				break
			}
			mix++
			total += r820tMixerSteps[mix]
		}
		if err := t.writeMask(0x05, byte(lna), 0x0f); err != nil {
			return err
		}
		return t.writeMask(0x07, byte(mix), 0x0f)
	})
}

// writeReg пишет регистр, если значение в тени другое.
func (t *r820t) writeReg(reg, val byte) error {
	if t.regs[reg] == val {
		return nil
	}
	t.regs[reg] = val
	return t.write(reg, []byte{val})
}

// writeMask меняет биты mask регистра на биты val.
func (t *r820t) writeMask(reg, val, mask byte) error {
	return t.writeReg(reg, t.regs[reg]&^mask|val&mask)
}

// write пишет подряд идущие регистры кусками, которые пропускает мост I2C.
func (t *r820t) write(reg byte, vals []byte) error {
	for len(vals) > 0 {
		n := min(len(vals), r820tMaxBurst)
		msg := append([]byte{reg}, vals[:n]...)
		if err := t.chip.i2cWrite(r820tAddr, msg); err != nil {
			return err
		}
		reg += byte(n)
		vals = vals[n:]
	}
	return nil
}

// read читает n регистров с нулевого.
func (t *r820t) read(n int) ([]byte, error) {
	if err := t.chip.i2cWrite(r820tAddr, []byte{0x00}); err != nil {
		return nil, err
	}
	b, err := t.chip.i2cRead(r820tAddr, n)
	if err != nil {
		return nil, err
	}
	for i := range b {
		b[i] = bitReverse(b[i])
	}
	return b, nil
}
