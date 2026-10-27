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
	"bpsk1200", "bpsk9600", "qpsk_ccsds", "cw", "fm", "ook",
}

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
}

// DefaultChannelSettings — настройки до первого «Установить»: AFSK 1200, как у маяка имитатора.
func DefaultChannelSettings() ChannelSettings {
	return ChannelSettings{OffsetHz: 0, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: "afsk1200"}
}

// Validate проверяет, что канал целиком лежит в полосе приёмника шириной sampleRate.
func (c ChannelSettings) Validate(sampleRate uint32) error {
	if c.BandwidthHz == 0 || c.BandwidthHz > sampleRate {
		return fmt.Errorf("%w: bandwidth %d Hz not in (0, %d]", ErrInvalidChannel, c.BandwidthHz, sampleRate)
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
	return nil
}
