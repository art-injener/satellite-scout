package demod

import (
	"math"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	// midRateHz — частота после грубого фильтра: канал 9600 (±8 кГц) с запасом на остаток Доплера.
	midRateHz = 64_000
	// fineTaps — точный фильтр «Полосы канала»: переход около 1 кГц при 64 кГц.
	fineTaps = 255
	// squelchHold — сколько порог остаётся открытым после сигнала, чтобы не срезать хвост кадра.
	squelchHold = 300 * time.Millisecond
)

// lineDemod переводит IQ канала в уровни линии перед выделением кадров.
type lineDemod interface {
	Demodulate(dst []byte, iq []complex64) []byte
}

// deframer выделяет из уровней линии кадры с верной контрольной суммой.
type deframer interface {
	Push(dst [][]byte, levels []byte) [][]byte
	Good() int
	Bad() int
}

// profile — реализованный демодулятор: прореживание точного фильтра после midRateHz,
// сборка демодулятора на частоте его выхода и выделитель кадров.
type profile struct {
	fineDecim   int
	newDemod    func(rate float64) lineDemod
	newDeframer func() deframer
}

func newHDLCDeframer() deframer { return &ax25.Deframer{} }

// demodGEOSCAN — профиль кадров CC1125: кроме кадра он даёт и разбор маяка Geoscan.
const demodGEOSCAN = "fsk9600_geoscan"

var profiles = map[string]profile{
	// AFSK 1200 хватает 32 кГц.
	"afsk1200": {fineDecim: 2, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewAFSKDemod(modem.AFSK1200(rate))
	}},
	// 9600 на 64 кГц: около 6,7 отсчёта на символ, меньше ФАПЧ тактов не держит.
	"fsk9600_g3ruh": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return &g3ruhDemod{fsk: modem.NewFSKDemod(modem.FSK9600(rate))}
	}},
	// Кадр CC1125: свой скремблер в кадре, на линии NRZ без G3RUH.
	demodGEOSCAN: {fineDecim: 1, newDeframer: func() deframer { return &geoscan.Deframer{} },
		newDemod: func(rate float64) lineDemod {
			return modem.NewFSKDemod(modem.FSK9600(rate))
		}},
}

// g3ruhDemod — FSK с дескремблером G3RUH; NRZI снимает уже выделитель кадров.
type g3ruhDemod struct {
	fsk  *modem.FSKDemod
	desc modem.G3RUHDescrambler
	buf  []byte
}

func (d *g3ruhDemod) Demodulate(dst []byte, iq []complex64) []byte {
	d.buf = d.fsk.Demodulate(d.buf[:0], iq)
	return d.desc.Descramble(dst, d.buf)
}

// Packet — принятый кадр.
type Packet struct {
	TimestampMs uint64 // начало блока IQ, в котором кадр закончился
	FrequencyHz uint64 // частота канала: настройка приёмника плюс сдвиг
	Frame       ax25.Frame
	Raw         []byte // кадр без FCS
}

// Status — состояние демодулятора для панели приёмника.
type Status struct {
	Demod        string
	Supported    bool    // демодулятор реализован
	LevelDBFS    float32 // уровень в полосе канала за последний блок
	SquelchOpen  bool
	Good, Bad    int    // кадры с верной FCS и отрезки, не прошедшие её
	LastPacketMs uint64 // время последнего кадра; 0 — кадров не было
}

// Chain — цепочка приёма одного канала: выделение канала из полосы приёмника
// в два этапа, порог, демодулятор, выделение кадров по профилю и разбор AX.25.
// Фильтры пересоздаются при смене частоты дискретизации, сдвига или полосы,
// выделитель кадров со счётчиками — при смене демодулятора.
// Не потокобезопасна: работает в горутине потока.
type Chain struct {
	rate      uint32
	offsetHz  int64
	bandwidth uint32
	demodName string

	coarse, fine *dsp.XlatingFIR
	demod        lineDemod
	squelch      dsp.Squelch
	deframer     deframer

	status Status
	levels []byte
	frames [][]byte
}

// NewChain создаёт цепочку; фильтры строятся по первому блоку.
func NewChain() *Chain {
	return &Chain{}
}

// Process принимает блок IQ хаба с текущими настройками канала и возвращает принятые кадры.
func (c *Chain) Process(f sdr.IQFrame, s sdr.ChannelSettings) []Packet {
	c.status.Demod = s.Demod
	p, ok := profiles[s.Demod]
	c.status.Supported = ok
	if !ok || f.SampleRate == 0 {
		c.coarse = nil
		c.status.SquelchOpen = false
		return nil
	}
	if c.coarse == nil || f.SampleRate != c.rate || s.OffsetHz != c.offsetHz || s.BandwidthHz != c.bandwidth ||
		s.Demod != c.demodName {
		c.build(f.SampleRate, s.OffsetHz, s.BandwidthHz, s.Demod, p)
	}

	c.squelch.ThresholdDBFS = s.SquelchDBFS
	ch := c.fine.Process(c.coarse.Process(f.Samples))
	c.status.SquelchOpen = c.squelch.Open(ch)
	c.status.LevelDBFS = c.squelch.LevelDBFS()
	if !c.status.SquelchOpen {
		return nil
	}

	c.levels = c.demod.Demodulate(c.levels[:0], ch)
	c.frames = c.deframer.Push(c.frames[:0], c.levels)
	c.status.Good, c.status.Bad = c.deframer.Good(), c.deframer.Bad()

	var out []Packet
	for _, b := range c.frames {
		frame, err := ax25.Parse(b)
		if err != nil {
			continue
		}
		out = append(out, Packet{
			TimestampMs: f.TimestampMs,
			FrequencyHz: uint64(int64(f.CenterHz) + s.OffsetHz),
			Frame:       frame,
			Raw:         b,
		})
		c.status.LastPacketMs = f.TimestampMs
	}
	return out
}

// Status — состояние после последнего блока.
func (c *Chain) Status() Status {
	return c.status
}

// build строит фильтры канала и демодулятор. Грубый фильтр прореживает до midRateHz: пропускает
// плоско до 0,15 от новой частоты, подавляет с 0,65 — наложенное при прореживании ложится дальше
// 0,35 от центра, за точный фильтр. Поэтому «Полоса канала» шире 0,3 от неё обрезается.
func (c *Chain) build(rate uint32, offsetHz int64, bandwidth uint32, demod string, p profile) {
	if c.deframer == nil || demod != c.demodName {
		c.deframer = p.newDeframer()
	}
	c.rate, c.offsetHz, c.bandwidth, c.demodName = rate, offsetHz, bandwidth, demod

	fs := float64(rate)
	decim := max(1, int(math.Round(fs/midRateHz)))
	mid := fs / float64(decim)
	transition := 0.25 * mid
	// Окно Блэкмана-Харриса: половина главного лепестка — 4 отсчёта спектра, то есть 4·fs/N.
	coarseTaps := int(math.Ceil(4*fs/transition)) | 1
	c.coarse = dsp.NewXlatingFIR(float64(offsetHz), fs, decim,
		dsp.DesignLowpass(0.4*mid, fs, coarseTaps, dsp.WindowBlackmanHarris))

	cutoff := min(float64(bandwidth)/2, 0.15*mid)
	c.fine = dsp.NewXlatingFIR(0, mid, p.fineDecim, dsp.DesignLowpass(cutoff, mid, fineTaps, dsp.WindowBlackmanHarris))

	out := mid / float64(p.fineDecim)
	c.demod = p.newDemod(out)
	c.squelch.HoldSamples = int(squelchHold.Seconds() * out)
}
