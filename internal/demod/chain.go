package demod

import (
	"math"
	"math/cmplx"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sstv"
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

// clockMeter — демодулятор с ФАПЧ тактов, сообщающий её ошибку фазы.
type clockMeter interface {
	ClockJitter() float64
}

// deviationMeter — демодулятор ЧМ, сообщающий принятую девиацию.
type deviationMeter interface {
	DeviationHz() float64
}

// syncCounter — выделитель кадров по синхрослову, считающий найденные слова и их ошибки.
type syncCounter interface {
	SyncWords() int
	SyncBitErrors() int
}

// pictureRx принимает картинки из IQ канала (SSTV).
type pictureRx interface {
	Push(iq []complex64) []sstv.Event
	Done() int
}

// profile — реализованный демодулятор: прореживание точного фильтра после midRateHz,
// сборка демодулятора на частоте его выхода и выделитель кадров. Профиль картинок
// (newPictures) вместо кадров даёт картинки и слушает канал без порога: развёртка
// идёт по часам, и пропуск звука сдвинул бы строки.
type profile struct {
	fineDecim   int
	newDemod    func(rate float64) lineDemod
	newDeframer func() deframer
	newPictures func(rate float64) pictureRx
}

func newHDLCDeframer() deframer { return &ax25.Deframer{} }

// demodGEOSCAN — профиль кадров CC1125: кроме кадра он даёт и разбор маяка Geoscan.
const demodGEOSCAN = "fsk9600_geoscan"

// DemodSSTV — профиль картинок SSTV поверх ЧМ; режим определяется по заголовку VIS.
const DemodSSTV = "sstv"

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
	// ЧМ с девиацией до 5 кГц и звуком до 2,3 кГц укладывается в 32 кГц.
	DemodSSTV: {fineDecim: 2, newPictures: newSSTVRx},
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

func (d *g3ruhDemod) ClockJitter() float64 { return d.fsk.ClockJitter() }
func (d *g3ruhDemod) DeviationHz() float64 { return d.fsk.DeviationHz() }

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

	// Ход приёма: меры последнего блока с уровнем выше порога (без удержания порога —
	// на хвосте шума ФАПЧ тактов теряет захват). Measured=false — такого блока ещё не было
	// после смены настроек.
	Measured      bool
	CarrierHz     float64 // смещение несущей от центра канала
	DeviationHz   float64 // принятая девиация; 0 — демодулятор её не меряет
	ClockJitter   float64 // ошибка фазы тактов, доли символа
	SyncCounted   bool    // выделитель кадров считает синхрослова
	SyncWords     int
	SyncBitErrors int

	// Picture — картинка, которая сейчас проявляется; nil — её нет. Не меняется после выдачи.
	Picture *PictureProgress
}

// PictureProgress — ход картинки в приёме.
type PictureProgress struct {
	Mode     string `json:"mode"`
	ModeName string `json:"mode_name"`
	Lines    int    `json:"lines"`
	Height   int    `json:"height"`
}

// Chain — цепочка приёма одного канала: выделение канала из полосы приёмника
// в два этапа, порог, демодулятор, выделение кадров по профилю и разбор AX.25.
// Фильтры пересоздаются при смене частоты дискретизации, сдвига или полосы,
// выделитель кадров со счётчиками — при смене демодулятора.
// Не потокобезопасна: работает в горутине потока.
type Chain struct {
	rate      uint32
	outRate   float64 // частота отсчётов канала на входе демодулятора
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
	prevCh complex64 // последний отсчёт канала прошлого блока для оценки несущей

	picRx    pictureRx
	pictures []sstv.Event
	centerHz uint64 // настройка приёмника, на которой идёт картинка
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
	if f.CenterHz != c.centerHz {
		// Другая частота приёмника — другой передатчик: картинка в приёме бросается.
		c.centerHz = f.CenterHz
		if c.picRx != nil && p.newPictures != nil && s.Demod == c.demodName {
			c.picRx = p.newPictures(c.outRate)
			c.status.Picture = nil
		}
	}
	if c.coarse == nil || f.SampleRate != c.rate || s.OffsetHz != c.offsetHz || s.BandwidthHz != c.bandwidth ||
		s.Demod != c.demodName {
		c.build(f.SampleRate, s.OffsetHz, s.BandwidthHz, s.Demod, p)
	}

	c.squelch.ThresholdDBFS = s.SquelchDBFS
	ch := c.fine.Process(c.coarse.Process(f.Samples))
	c.status.SquelchOpen = c.squelch.Open(ch)
	c.status.LevelDBFS = c.squelch.LevelDBFS()
	c.pictures = c.pictures[:0]
	if c.picRx != nil {
		c.pictures = c.picRx.Push(ch)
		c.status.Good = c.picRx.Done()
		for _, e := range c.pictures {
			c.status.Picture = nil
			if e.Kind != sstv.EventDone {
				c.status.Picture = &PictureProgress{Mode: e.Mode.ID, ModeName: e.Mode.Name, Lines: e.Lines, Height: e.Mode.Height}
			}
		}
		if float64(c.status.LevelDBFS) >= s.SquelchDBFS {
			c.measure(ch)
		}
		c.prevCh = ch[len(ch)-1]
		return nil
	}
	if !c.status.SquelchOpen {
		return nil
	}

	c.levels = c.demod.Demodulate(c.levels[:0], ch)
	c.frames = c.deframer.Push(c.frames[:0], c.levels)
	c.status.Good, c.status.Bad = c.deframer.Good(), c.deframer.Bad()
	if float64(c.status.LevelDBFS) >= s.SquelchDBFS {
		c.measure(ch)
	}
	c.prevCh = ch[len(ch)-1]

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

// Pictures — события картинок последнего блока; срез действителен до следующего Process.
func (c *Chain) Pictures() []sstv.Event {
	return c.pictures
}

// Status — состояние после последнего блока.
func (c *Chain) Status() Status {
	return c.status
}

// measure снимает меры хода приёма с блока канала. Несущая — средний поворот фазы
// между соседними отсчётами: у ЧМ со сбалансированными данными он равен смещению несущей.
func (c *Chain) measure(ch []complex64) {
	var sum complex128
	prev := complex128(c.prevCh)
	for _, s := range ch {
		x := complex128(s)
		sum += x * cmplx.Conj(prev)
		prev = x
	}
	st := &c.status
	st.Measured = true
	st.CarrierHz = cmplx.Phase(sum) * c.outRate / (2 * math.Pi)
	if m, ok := c.demod.(clockMeter); ok {
		st.ClockJitter = m.ClockJitter()
	}
	if m, ok := c.demod.(deviationMeter); ok {
		st.DeviationHz = m.DeviationHz()
	}
	if sc, ok := c.deframer.(syncCounter); ok {
		st.SyncCounted = true
		st.SyncWords, st.SyncBitErrors = sc.SyncWords(), sc.SyncBitErrors()
	}
}

// midRate — частота отсчётов после грубого фильтра: ближайшая к midRateHz, кратная частоте приёмника.
func midRate(fs float64) float64 {
	return fs / float64(max(1, int(math.Round(fs/midRateHz))))
}

// build строит фильтры канала и демодулятор. Грубый фильтр прореживает до midRateHz: пропускает
// плоско до 0,15 от новой частоты, подавляет с 0,65 — наложенное при прореживании ложится дальше
// 0,35 от центра, за точный фильтр. Поэтому «Полоса канала» шире 0,3 от неё обрезается.
func (c *Chain) build(rate uint32, offsetHz int64, bandwidth uint32, demod string, p profile) {
	newProfile := demod != c.demodName
	newRate := rate != c.rate
	newOffset := offsetHz != c.offsetHz
	c.rate, c.offsetHz, c.bandwidth, c.demodName = rate, offsetHz, bandwidth, demod

	fs := float64(rate)
	mid := midRate(fs)
	decim := int(math.Round(fs / mid))
	transition := 0.25 * mid
	// Окно Блэкмана-Харриса: половина главного лепестка — 4 отсчёта спектра, то есть 4·fs/N.
	coarseTaps := int(math.Ceil(4*fs/transition)) | 1
	c.coarse = dsp.NewXlatingFIR(float64(offsetHz), fs, decim,
		dsp.DesignLowpass(0.4*mid, fs, coarseTaps, dsp.WindowBlackmanHarris))

	cutoff := min(float64(bandwidth)/2, 0.15*mid)
	c.fine = dsp.NewXlatingFIR(0, mid, p.fineDecim, dsp.DesignLowpass(cutoff, mid, fineTaps, dsp.WindowBlackmanHarris))

	c.outRate = mid / float64(p.fineDecim)
	if p.newPictures != nil {
		// Картинка в приёме переживает смену полосы канала; новый сдвиг — другой передатчик,
		// и заголовок VIS ищется заново.
		if c.picRx == nil || newProfile || newRate || newOffset {
			c.picRx = p.newPictures(c.outRate)
			c.status.Picture = nil
		}
		c.demod, c.deframer = nil, nil
		c.status.Bad = 0
	} else {
		c.picRx, c.status.Picture = nil, nil
		c.demod = p.newDemod(c.outRate)
		if c.deframer == nil || newProfile {
			c.deframer = p.newDeframer()
		}
	}
	c.squelch.HoldSamples = int(squelchHold.Seconds() * c.outRate)
	c.prevCh = 0
	m := &c.status
	m.Measured, m.CarrierHz, m.DeviationHz, m.ClockJitter = false, 0, 0, 0
	m.SyncCounted, m.SyncWords, m.SyncBitErrors = false, 0, 0
}
