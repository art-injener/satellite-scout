package sdr

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// ErrInvalidChannel — настройки демодулятора не подходят к полосе приёмника.
var ErrInvalidChannel = errors.New("invalid channel settings")

// Demods — демодуляторы, которые можно выбрать в панели приёмника. Идентификаторы
// совпадают со значениями селектора «Демодулятор» в UI.
var Demods = []string{
	"fsk9600_g3ruh", "fsk9600_geoscan", "fsk4800", "fsk1200", "gmsk9600", "afsk1200",
	"bpsk1200", "bpsk9600", "qpsk_ccsds", "cw", "fm", "wfm", "usb", "lsb", "ook", "sstv",
}

// MaxChannelBandwidthHz — самая широкая «Полоса канала» узких профилей: шире цепочка
// с промежуточной частотой около 64 кГц ровно не пропускает, и края канала на спектре
// разошлись бы с тем, что слышно. Широкополосная ЧМ — отдельно, WFMBandwidthHz.
const MaxChannelBandwidthHz = 18_000

// WFMBandwidthHz — канал вещательной ЧМ: девиация ±75 кГц и звук до 15 кГц,
// по правилу Карсона около 180 кГц, сетка станций 200 кГц.
const WFMBandwidthHz = 200_000

// DemodMaxBandwidthHz — профили, у которых полоса переносится в звук напрямую и шире не слышна.
// SSB: тон равен расстоянию до несущей за краем полосы, и дальний край должен пройти ФНЧ звука
// (около 4,8 кГц). CW: тон 700 Гц от середины, шире 1 кГц края уходят за нулевые биения.
var DemodMaxBandwidthHz = map[string]uint32{"usb": 4_000, "lsb": 4_000, "cw": 1_000}

// ChannelSettings — что демодулятор вырезает из полосы приёмника и как принимает:
// сдвиг от центра настройки, ширина «Полосы канала», порог и демодулятор.
// Хранятся в памяти хаба на тракт и после перезапуска сервера возвращаются к значениям по умолчанию.
type ChannelSettings struct {
	OffsetHz    int64  `json:"offset_hz"`
	BandwidthHz uint32 `json:"bandwidth_hz"`
	// SquelchDBFS — уровень в полосе канала, ниже которого демодулятор не работает.
	// Держится в dBFS: при смене усиления его значение в dBm сдвигается.
	SquelchDBFS float64 `json:"squelch_dbfs"`
	Demod       string  `json:"demod"`
	// DemodOff — демодулятор выключен: спектр и водопад идут, цепочка не принимает.
	// Нулевое значение оставляет приём включённым, поэтому старые запросы без поля его не гасят.
	DemodOff bool `json:"demod_off,omitempty"`
	// DopplerNorad — КА, за Доплером которого канал следует по его орбите; 0 — спутника ещё нет.
	DopplerNorad int `json:"doppler_norad,omitempty"`
	// DopplerAuto — канал следует за Доплером настроенного КА. Ложь — сдвиг фиксированный.
	// Поле без omitempty: ложь должна дойти до панели, иначе «Фикс.» не отличить от значения по умолчанию.
	DopplerAuto bool `json:"doppler_auto"`
}

// DefaultChannelSettings — настройки до первого включения: AFSK 1200, как у маяка имитатора.
// Демодулятор выключен: спектр идёт, приём начинается только по переключателю.
func DefaultChannelSettings() ChannelSettings {
	return ChannelSettings{
		OffsetHz: 0, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: "afsk1200", DemodOff: true,
		DopplerAuto: true,
	}
}

// Validate проверяет, что канал целиком лежит в годной полосе приёмника шириной sampleRate.
func (c ChannelSettings) Validate(sampleRate uint32) error {
	limit := uint32(MaxChannelBandwidthHz)
	if c.Demod == "wfm" {
		limit = WFMBandwidthHz
	}
	if c.BandwidthHz == 0 || c.BandwidthHz > sampleRate || c.BandwidthHz > limit {
		return fmt.Errorf("%w: bandwidth %d Hz not in (0, %d]", ErrInvalidChannel, c.BandwidthHz,
			min(sampleRate, limit))
	}
	half := int64(sampleRate) / 2
	edge := c.OffsetHz
	if edge < 0 {
		edge = -edge
	}
	if 2*edge+int64(c.BandwidthHz) > 2*half {
		return fmt.Errorf("%w: channel %d±%d Hz outside band ±%d Hz",
			ErrInvalidChannel, c.OffsetHz, c.BandwidthHz/2, half)
	}
	if math.IsNaN(c.SquelchDBFS) || math.IsInf(c.SquelchDBFS, 0) || c.SquelchDBFS > 0 {
		return fmt.Errorf("%w: squelch %v dBFS must be finite and not above 0", ErrInvalidChannel, c.SquelchDBFS)
	}
	if !slices.Contains(Demods, c.Demod) {
		return fmt.Errorf("%w: unknown demod %q", ErrInvalidChannel, c.Demod)
	}
	if limit, ok := DemodMaxBandwidthHz[c.Demod]; ok && c.BandwidthHz > limit {
		return fmt.Errorf("%w: %s bandwidth %d Hz wider than heard, at most %d Hz",
			ErrInvalidChannel, c.Demod, c.BandwidthHz, limit)
	}
	if c.DopplerNorad < 0 {
		return fmt.Errorf("%w: doppler_norad %d must not be negative", ErrInvalidChannel, c.DopplerNorad)
	}
	return nil
}
