package demod

import (
	"math"
	"math/cmplx"
	"slices"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax100"
	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/ccsds"
	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/m10"
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/rs41"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sstv"
	"github.com/art-injener/satellite-scout/internal/usp"
)

const (
	// midRateHz — частота после грубого фильтра: канал 9600 (±8 кГц) с запасом на остаток Доплера.
	midRateHz = 64_000
	// fineTaps — точный фильтр «Полосы канала»: переход около 1 кГц при 64 кГц.
	fineTaps = 255
	// squelchHold — сколько порог остаётся открытым после сигнала, чтобы не срезать хвост кадра.
	squelchHold = 300 * time.Millisecond

	// AudioRate — частота отсчётов звука на выходе цепочки: голос до 3 кГц и тона SSTV с запасом.
	AudioRate = 12_000
	// voiceDeviationHz — девиация, в которую ложится звук ±1; у голоса узкополосной ЧМ до 5 кГц.
	voiceDeviationHz = 5000
	// voiceDeemphasisTau — деэмфазис узкополосной ЧМ любительских станций, излом около 300 Гц.
	voiceDeemphasisTau = 530e-6
	// wfmDeviationHz — девиация вещательной ЧМ: звук ±1 это ±75 кГц.
	wfmDeviationHz = 75_000
	// wfmDeemphasisTau — деэмфазис вещания (Европа и Россия), 50 мкс.
	wfmDeemphasisTau = 50e-6
	// wfmMidHz — частота после грубого фильтра широкополосной ЧМ. 0,15 от неё
	// шире половины канала 200 кГц, точный фильтр ещё прореживает до дискриминатора.
	wfmMidHz = 1_024_000
	// ssbLowHz — нижний край звука боковой полосы: подавленная несущая стоит на столько
	// за краем выделенной полосы.
	ssbLowHz = 200
	// cwToneHz — тон биений CW: несущая в середине полосы звучит этой нотой.
	cwToneHz = 700
	// cwFineTaps — точный фильтр CW: переход около 250 Гц, чтобы полоса 500 Гц была узкой.
	cwFineTaps = 1023
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

// levelFreezer — демодулятор с заморозкой адаптации порогов и AFC на хвосте шумоподавителя.
type levelFreezer interface {
	SetFreeze(freeze bool)
}

// syncCounter — выделитель кадров по синхрослову, считающий найденные слова и их ошибки.
type syncCounter interface {
	SyncWords() int
	SyncBitErrors() int
}

// fecCounter — выделитель с FEC (AX.100 / USP): число исправленных символов/бит.
type fecCounter interface {
	FECCorrected() int
}

// pictureRx принимает картинки из IQ канала (SSTV).
type pictureRx interface {
	Push(iq []complex64) []sstv.Event
	Done() int
	// Audio — звук последнего Push с частотой AudioRate.
	Audio() []float32
}

// audioRx переводит IQ канала в звук с частотой AudioRate.
type audioRx interface {
	Demodulate(dst []float32, iq []complex64) []float32
}

// profile — реализованный демодулятор: прореживание точного фильтра после midRateHz,
// сборка демодулятора на частоте его выхода и выделитель кадров. Профиль картинок
// (newPictures) вместо кадров даёт картинки и слушает канал без порога: развёртка
// идёт по часам, и пропуск звука сдвинул бы строки. Профиль звука (newAudio) даёт
// только звук. Точный фильтр у всех профилей стоит по центру канала; newAudio получает
// частоту, на которой середина полосы звучит в звуке: у SSB (sideband ±1) несущая
// за краем полосы, у CW — тон биений toneHz. maxWidthHz ограничивает полосу тем,
// что слышно; fineTaps — длина точного фильтра, 0 — fineTaps.
// midHz — желаемая частота после грубого фильтра; 0 — midRateHz.
type profile struct {
	fineDecim int
	fineTaps  int
	midHz     float64
	newDemod  func(rate float64) lineDemod
	// newDemodBaud — линия со скоростью из ChannelSettings.Baud; baud уже > 0 (дефолт подставлен).
	newDemodBaud func(rate, baud float64) lineDemod
	newDeframer  func() deframer
	newPictures  func(rate float64) pictureRx
	newAudio     func(rate, beatHz float64) audioRx
	sideband     float64
	toneHz       float64
	maxWidthHz   float64
}

func newVoiceFM(rate, _ float64) audioRx {
	return modem.NewFMAudioDemod(modem.FMAudioConfig{
		SampleRate: rate, DeviationHz: voiceDeviationHz, AudioRate: AudioRate, DeemphasisTau: voiceDeemphasisTau,
	})
}

func newVoiceAM(rate, _ float64) audioRx {
	return modem.NewAMAudioDemod(modem.AMAudioConfig{SampleRate: rate, AudioRate: AudioRate})
}

func newWideFM(rate, _ float64) audioRx {
	return modem.NewFMAudioDemod(modem.FMAudioConfig{
		SampleRate: rate, DeviationHz: wfmDeviationHz, AudioRate: AudioRate, DeemphasisTau: wfmDeemphasisTau,
	})
}

func newSSB(rate, beatHz float64) audioRx {
	return modem.NewSSBDemod(modem.SSBConfig{SampleRate: rate, AudioRate: AudioRate, ShiftHz: beatHz})
}

func newHDLCDeframer() deframer { return &ax25.Deframer{} }

func newAX100Deframer() deframer   { return &ax100.Deframer{} }
func newAX100RSDeframer() deframer { return &ax100.RSDeframer{} }
func newUSPDeframer() deframer     { return &usp.Deframer{} }
func newCCSDSDeframer() deframer   { return &ccsds.Deframer{} }
func newRS41Deframer() deframer    { return &rs41.Deframer{} }
func newM10Deframer() deframer     { return &m10.Deframer{} }

// newNullDeframer оставлен как имя для тестов/старых ссылок: линия режется burstDeframer.
func newNullDeframer() deframer { return newBurstDeframer() }

// demodGEOSCAN — профиль кадров CC1125: кроме кадра он даёт и разбор маяка Geoscan.
const demodGEOSCAN = "fsk9600_geoscan"

// DemodSSTV — профиль картинок SSTV поверх ЧМ; режим определяется по заголовку VIS.
const DemodSSTV = "sstv"

var profiles = map[string]profile{
	// AFSK 1200 хватает 32 кГц.
	"afsk1200": {fineDecim: 2, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewAFSKDemod(modem.AFSK1200(rate))
	}},
	// GFSK 1200: без прореживания — на 64 кГц около 53 отсчёта на символ; кадр HDLC без G3RUH.
	"fsk1200": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.FSK1200(rate))
	}},
	// GFSK 4800: без прореживания, около 13 отсчётов на символ на 64 кГц.
	"fsk4800": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.FSK4800(rate))
	}},
	// FSK 9600 линия: эфир ISM без HDLC/G3RUH; baud из канала; пачки burstDeframer.
	"fsk9600": {fineDecim: 1, newDemodBaud: func(rate, baud float64) lineDemod {
		cfg := modem.FSK9600(rate)
		if baud > 0 {
			cfg.Baud = baud
		}
		return modem.NewFSKDemod(cfg)
	}},
	// Явные id GFSK (BT 0,5): та же физика, что fsk*; 9600 — кадр с G3RUH.
	"gfsk1200": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GFSK1200(rate))
	}},
	"gfsk4800": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GFSK4800(rate))
	}},
	"gfsk9600": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return &g3ruhDemod{fsk: modem.NewFSKDemod(modem.GFSK9600(rate))}
	}},
	// GMSK 9600: BT 0,4; кадр как у G3RUH (любительский UHF).
	"gmsk9600": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return &g3ruhDemod{fsk: modem.NewFSKDemod(modem.GMSK9600(rate))}
	}},
	// GMSK 4800: тот же модем, кадр HDLC без G3RUH.
	"gmsk4800": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GMSK4800(rate))
	}},
	"gmsk2400": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GMSK2400(rate))
	}},
	// MSK 9600: линия без известного кадра (не в «Авто» по CRC).
	"msk9600": {fineDecim: 1, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.MSK9600(rate))
	}},
	// M-FSK: линия без кадра (DMR sync/кадр — не в этом срезе).
	"3fsk4800": {fineDecim: 1, newDemod: func(rate float64) lineDemod {
		return modem.NewMFSKDemod(modem.FSK3_4800(rate))
	}},
	"4fsk4800": {fineDecim: 1, newDemod: func(rate float64) lineDemod {
		return modem.NewMFSKDemod(modem.FSK4DMR4800(rate))
	}},
	"gmfsk4800": {fineDecim: 1, newDemod: func(rate float64) lineDemod {
		return modem.NewMFSKDemod(modem.GMFSK4800(rate))
	}},
	// BPSK 1200: дифференциальный приём, кадр AX.25 HDLC.
	"bpsk1200": {fineDecim: 2, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewBPSKDemod(modem.BPSK1200(rate))
	}},
	// BPSK 9600: DBPSK + AX.25 HDLC (любительский UHF, как bpsk1200).
	"bpsk9600": {fineDecim: 1, newDeframer: newHDLCDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewBPSKDemod(modem.BPSK9600(rate))
	}},
	// QPSK CCSDS: CADU ASM+RS dual-basis (стык под LRPT — тот же CADU).
	"qpsk_ccsds": {fineDecim: 1, newDeframer: newCCSDSDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewQPSKDemod(modem.QPSKCCSDS(rate))
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
	// AX.100 Mode 5 (ASM+Golay): та же линия GMSK/FSK, другой выделитель кадров.
	"gmsk4800_ax100": {fineDecim: 1, newDeframer: newAX100Deframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GMSK4800(rate))
	}},
	"gmsk9600_ax100": {fineDecim: 1, newDeframer: newAX100Deframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GMSK9600(rate))
	}},
	"fsk4800_ax100": {fineDecim: 1, newDeframer: newAX100Deframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.FSK4800(rate))
	}},
	// AX.100 Mode RS: G3RUH внутри выделителя кадров (скремблирован и sync).
	"gmsk9600_ax100_rs": {fineDecim: 1, newDeframer: newAX100RSDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GMSK9600(rate))
	}},
	"fsk9600_ax100_rs": {fineDecim: 1, newDeframer: newAX100RSDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.FSK9600(rate))
	}},
	// USP (SPUTNIX): GMSK + свой кадр (Viterbi + RS dual-basis).
	"gmsk2400_usp": {fineDecim: 1, newDeframer: newUSPDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GMSK2400(rate))
	}},
	"gmsk4800_usp": {fineDecim: 1, newDeframer: newUSPDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GMSK4800(rate))
	}},
	"gmsk9600_usp": {fineDecim: 1, newDeframer: newUSPDeframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GMSK9600(rate))
	}},
	// ЧМ с девиацией до 5 кГц и звуком до 2,3 кГц укладывается в 32 кГц.
	DemodSSTV: {fineDecim: 2, newPictures: newSSTVRx},
	// Голос: АМ, ЧМ и однополосная. 32 кГц не дают наложений даже при самой широкой полосе канала.
	"am": {fineDecim: 2, newAudio: newVoiceAM},
	"fm": {fineDecim: 2, newAudio: newVoiceFM},
	// Вещательная ЧМ: канал 200 кГц не проходит через 64 кГц, поэтому грубый фильтр шире.
	"wfm": {fineDecim: 4, midHz: wfmMidHz, newAudio: newWideFM},
	"usb": {fineDecim: 2, newAudio: newSSB, sideband: 1, maxWidthHz: maxWidth("usb")},
	"lsb": {fineDecim: 2, newAudio: newSSB, sideband: -1, maxWidthHz: maxWidth("lsb")},
	// CW слушается как SSB с тоном биений; полосе до 1 кГц хватает 16 кГц.
	"cw": {fineDecim: 4, fineTaps: cwFineTaps, newAudio: newSSB, toneHz: cwToneHz, maxWidthHz: maxWidth("cw")},
	// OOK: линия без кадрового протокола; пачки burstDeframer. Скорость из Baud канала.
	"ook": {fineDecim: 1, newDemodBaud: func(rate, baud float64) lineDemod {
		return modem.NewOOKDemod(modem.OOKConfig{SampleRate: rate, Baud: baud})
	}},
	// RS41: GFSK 4800 + кадр Vaisala (whitening, dual RS, TLV CRC).
	"rs41": {fineDecim: 1, newDeframer: newRS41Deframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.GFSK4800(rate))
	}},
	// M10: FSK ~9615 (после Manchester на линии) + линейный checksum.
	"m10": {fineDecim: 1, newDeframer: newM10Deframer, newDemod: func(rate float64) lineDemod {
		return modem.NewFSKDemod(modem.FSKConfig{
			SampleRate: rate, Baud: m10.Baud, DeviationHz: 3000, BT: 0.5,
		})
	}},
	// π/4-DQPSK и 16-APSK: линия без кадра; скорость из Baud канала (0 — дефолт модема).
	"pi4_dqpsk": {fineDecim: 1, newDemodBaud: func(rate, baud float64) lineDemod {
		return modem.NewPI4DQPSKDemod(modem.PI4DQPSKConfig{SampleRate: rate, Baud: baud})
	}},
	"apsk16": {fineDecim: 1, newDemodBaud: func(rate, baud float64) lineDemod {
		return modem.NewAPSK16Demod(modem.APSK16Config{SampleRate: rate, Baud: baud})
	}},
}

// maxWidth — самая широкая слышимая полоса профиля; шире настройку отклоняет sdr.ChannelSettings.Validate.
func maxWidth(demod string) float64 {
	return float64(sdr.DemodMaxBandwidthHz[demod])
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

func (d *g3ruhDemod) ClockJitter() float64  { return d.fsk.ClockJitter() }
func (d *g3ruhDemod) DeviationHz() float64  { return d.fsk.DeviationHz() }
func (d *g3ruhDemod) SetFreeze(freeze bool) { d.fsk.SetFreeze(freeze) }

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
	FECCounted    bool // профиль с FEC (AX.100 / USP)
	FECCorrected  int  // исправления Golay/RS с начала сеанса профиля
	// SNRDB — превышение сигнала над шумом в полосе канала, дБ. 0 — не измерялось:
	// меряется только на блоке с уровнем выше порога, как и прочие меры хода приёма.
	SNRDB float32

	// Picture — картинка, которая сейчас проявляется; nil — её нет. Не меняется после выдачи.
	Picture *PictureProgress

	// DopplerHz — сдвиг Доплера, на который канал ушёл от сдвига; nil — канал за Доплером
	// не следует. Ставит сервис: цепочка орбит не знает.
	DopplerHz *float64
	// PassbandHz — ширина полосы, которую цепочка пропускает на деле: у SSB и CW она уже
	// «Полосы канала», если та не помещается в звук.
	PassbandHz float64

	// Поля режима «Авто» (Demod=auto): фаза подбора и закреплённый профиль.
	AutoLocked     string              // id профиля после победы по CRC; пусто — подбор
	AutoPhase      string              // "search" | "locked"
	AutoCandidates int                 // число кандидатов в текущем подборе
	AutoIDs        []string            // id кандидатов (порядок подбора)
	AutoStats      []AutoCandidateStat // сводка по активным кандидатам в фазе подбора
	DroppedFrames  uint64              // накопленные пропуски IQ-фреймов в кольце подписки
}

// AutoCandidateStat — компактная сводка по одному кандидату режима «Авто».
type AutoCandidateStat struct {
	ID        string `json:"id"`
	SyncWords int    `json:"sync_words,omitempty"`
	Good      int    `json:"good,omitempty"`
	Bad       int    `json:"bad,omitempty"`
}

// PictureProgress — ход картинки в приёме.
type PictureProgress struct {
	Mode     string `json:"mode"`
	ModeName string `json:"mode_name"`
	Lines    int    `json:"lines"`
	Height   int    `json:"height"`
}

// channelNoise оценивает SNR блока канала через спектр: шум — нижний перцентиль
// мощностей бинов внутри полосы пропускания, сигнал — сумма бинов минус шум.
// Окно сглаживает утечку сильных тонов в соседние бины, коэффициент усиления окна
// сокращается в отношении сигнал/шум. Работает в горутине потока цепочки.
type channelNoise struct {
	spec   *dsp.Spectrum
	window []float64
	buf    []complex64
	size   int
}

// ensure подбирает размер БПФ под длину блока канала: степень двойки от 256 до 1024,
// не больше числа отсчётов в блоке. Спектр и окно перестраиваются при смене размера.
func (n *channelNoise) ensure(blockLen int) {
	size := 256
	for size*2 <= 1024 && size*2 <= blockLen {
		size *= 2
	}
	if n.size == size && n.spec != nil {
		return
	}
	n.size = size
	n.spec = dsp.NewSpectrum(size)
	n.window = dsp.MakeWindowCoeffs(size, dsp.WindowBlackmanHarris)
	n.buf = make([]complex64, size)
}

// snrDB возвращает превышение сигнала над шумом в полосе пропускания passbandHz, дБ.
// Блок идёт на частоте rate. 0 — блок слишком мал или полоса не видна.
// Шум — 20-й перцентиль мощностей бинов внутри полосы, сигнал — сумма бинов минус шум.
func (n *channelNoise) snrDB(ch []complex64, rate, passbandHz float64) float32 {
	n.ensure(len(ch))
	if n.spec == nil || len(ch) < n.size || passbandHz <= 0 || rate <= 0 {
		return 0
	}
	copy(n.buf, ch[:n.size])
	dsp.ApplyWindow(n.buf, n.window)
	bins := n.spec.PowerSpectrum(n.buf)

	binHz := rate / float64(n.size)
	half := int(math.Round(passbandHz / 2 / binHz))
	center := n.size / 2
	lo := max(0, center-half)
	hi := min(n.size, center+half)
	if hi <= lo {
		return 0
	}
	inBand := make([]float64, 0, hi-lo)
	var total float64
	for i := lo; i < hi; i++ {
		inBand = append(inBand, bins[i])
		total += bins[i]
	}
	slices.Sort(inBand)
	// 20-й перцентиль — плотность шума: узкие тоны (AFSK, SSTV) его не поднимают,
	// у широкого FSK он всё же ближе к шуму, чем медиана.
	noiseDensity := inBand[len(inBand)/5]
	noisePower := noiseDensity * float64(len(inBand))
	if noisePower <= 0 {
		return 0
	}
	signal := total - noisePower
	if signal <= 0 {
		return 0
	}
	snr := 10 * math.Log10(signal/noisePower)
	if snr < 0 {
		return 0
	}
	return float32(snr)
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
	baud      float64 // ChannelSettings.Baud; учитывается профилями с newDemodBaud

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

	// sstvVISOnly — следующий приёмник SSTV только с VIS (после DemodOff).
	sstvVISOnly bool

	audioRx audioRx
	audio   []float32

	dopplerHz float64

	noise channelNoise // оценка SNR по спектру блока канала
}

// NewChain создаёт цепочку; фильтры строятся по первому блоку.
func NewChain() *Chain {
	return &Chain{}
}

// Process принимает блок IQ хаба с текущими настройками канала и возвращает принятые кадры.
func (c *Chain) Process(f sdr.IQFrame, s sdr.ChannelSettings) []Packet {
	c.status.Demod = s.Demod
	c.audio = c.audio[:0]
	p, ok := profiles[s.Demod]
	c.status.Supported = ok
	if !ok || f.SampleRate == 0 {
		c.coarse = nil
		c.status.SquelchOpen = false
		return nil
	}
	if f.CenterHz != c.centerHz {
		// Другая частота приёмника — другой передатчик: картинка в приёме бросается.
		// Mid-join выключен: тот же IQ в тесте/срыве LO не должен сразу стать новым кадром.
		c.centerHz = f.CenterHz
		if c.picRx != nil && p.newPictures != nil && s.Demod == c.demodName {
			c.picRx = newSSTVRxVISOnly(c.outRate)
			c.status.Picture = nil
		}
	}
	if c.coarse == nil || f.SampleRate != c.rate || s.BandwidthHz != c.bandwidth ||
		s.Demod != c.demodName || (p.newDemodBaud != nil && s.Baud != c.baud) {
		c.build(f.SampleRate, s.OffsetHz, s.BandwidthHz, s.Demod, s.Baud, p)
	} else if s.OffsetHz != c.offsetHz {
		// Смена сдвига канала выполняется плавно через NCO без пересборки фильтров и модема.
		c.offsetHz = s.OffsetHz
		if c.picRx != nil && p.newPictures != nil {
			// Перестройка канала посреди SSTV означает другой передатчик: картинка сбрасывается.
			c.picRx = newSSTVRxVISOnly(c.outRate)
			c.status.Picture = nil
		}
	}
	// Сдвиг канала плюс Доплер без пересборки фильтров: фаза не рвётся.
	c.coarse.SetShift(float64(s.OffsetHz) + c.dopplerHz)

	c.squelch.ThresholdDBFS = s.SquelchDBFS
	ch := c.fine.Process(c.coarse.Process(f.Samples))
	c.status.SquelchOpen = c.squelch.Open(ch)
	c.status.LevelDBFS = c.squelch.LevelDBFS()
	c.pictures = c.pictures[:0]
	if c.picRx != nil || c.audioRx != nil {
		c.listen(ch)
		// Меры — только с громкого блока (пик или среднее), не с хвоста удержания.
		if c.squelch.AboveThreshold() {
			c.measure(ch)
		}
		c.prevCh = ch[len(ch)-1]
		return nil
	}
	freqHz := uint64(int64(f.CenterHz) + s.OffsetHz) //nolint:gosec // частоты канала > 0
	if !c.status.SquelchOpen {
		// Хвост линии (OOK/burst) отдаём при закрытии порога, иначе пачка теряется.
		return c.packetsFromFrames(f.TimestampMs, freqHz, c.flushLine(nil))
	}

	if lf, okFreeze := c.demod.(levelFreezer); okFreeze {
		lf.SetFreeze(!c.squelch.AboveThreshold())
	}
	c.levels = c.demod.Demodulate(c.levels[:0], ch)
	c.frames = c.deframer.Push(c.frames[:0], c.levels)
	c.status.Good, c.status.Bad = c.deframer.Good(), c.deframer.Bad()
	if fc, ok := c.deframer.(fecCounter); ok {
		c.status.FECCounted = true
		c.status.FECCorrected = fc.FECCorrected()
	}
	if c.squelch.AboveThreshold() {
		c.measure(ch)
	}
	c.prevCh = ch[len(ch)-1]
	return c.packetsFromFrames(f.TimestampMs, freqHz, c.frames)
}

func (c *Chain) flushLine(dst [][]byte) [][]byte {
	if c.deframer == nil {
		return dst
	}
	if f, ok := c.deframer.(lineFlusher); ok {
		dst = f.Flush(dst)
		c.status.Good, c.status.Bad = c.deframer.Good(), c.deframer.Bad()
	}
	return dst
}

func (c *Chain) packetsFromFrames(tsMs, freqHz uint64, frames [][]byte) []Packet {
	var out []Packet
	for _, b := range frames {
		// Сырой кадр отдаём всегда: разбор kind — в payload; AX.25 может не сойтись (снимок GEOSCAN).
		frame, _ := ax25.Parse(b)
		out = append(out, Packet{
			TimestampMs: tsMs,
			FrequencyHz: freqHz,
			Frame:       frame,
			Raw:         append([]byte(nil), b...),
		})
		c.status.LastPacketMs = tsMs
	}
	return out
}

// listen ведёт профили, слушающие канал без порога: картинки и звук.
func (c *Chain) listen(ch []complex64) {
	if c.audioRx != nil {
		c.audio = c.gate(c.audioRx.Demodulate(c.audio, ch))
		return
	}
	c.pictures = c.picRx.Push(ch)
	c.status.Good = c.picRx.Done()
	for _, e := range c.pictures {
		c.status.Picture = nil
		if e.Kind != sstv.EventDone {
			c.status.Picture = &PictureProgress{
				Mode:     e.Mode.ID,
				ModeName: e.Mode.Name,
				Lines:    e.Lines,
				Height:   e.Mode.Height,
			}
		}
	}
	c.audio = c.gate(append(c.audio, c.picRx.Audio()...))
}

// SetDopplerHz задаёт сдвиг Доплера для следующих блоков: канал встаёт на сдвиг плюс Доплер.
func (c *Chain) SetDopplerHz(hz float64) {
	c.dopplerHz = hz
}

// Pictures — события картинок последнего блока; срез действителен до следующего Process.
func (c *Chain) Pictures() []sstv.Event {
	return c.pictures
}

// Audio — звук последнего блока с частотой AudioRate; срез действителен до следующего Process.
// Пусто — профиль без звука. Под закрытым порогом идёт тишина той же длины, чтобы
// проигрывание держало темп потока.
func (c *Chain) Audio() []float32 {
	return c.audio
}

// gate глушит звук блока, пока порог закрыт.
func (c *Chain) gate(audio []float32) []float32 {
	if !c.status.SquelchOpen {
		clear(audio)
	}
	return audio
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
	st.SNRDB = c.noise.snrDB(ch, c.outRate, st.PassbandHz)
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
	if fc, ok := c.deframer.(fecCounter); ok {
		st.FECCounted = true
		st.FECCorrected = fc.FECCorrected()
	}
}

// channelFrontend — общий канальный тракт для параллельного подбора профилей:
// один грубый фильтр с децимацией до 64 кГц, точный фильтр 64 кГц (и при необходимости 32 кГц),
// шумоподавитель и оценка несущей с SNR. При изменении только OffsetHz сдвигает NCO без пересборки.
type channelFrontend struct {
	rate       uint32
	bandwidth  uint32
	midRate    float64
	passbandHz float64

	coarse *dsp.XlatingFIR
	fine64 *dsp.XlatingFIR
	fine32 *dsp.XlatingFIR

	squelch dsp.Squelch
	noise   channelNoise
	prevCh  complex64
}

// build пересобирает фильтры фронтенда при первом блоке или смене частоты дискретизации и полосы.
func (fe *channelFrontend) build(rate uint32, offsetHz int64, bandwidth uint32) {
	fe.rate, fe.bandwidth = rate, bandwidth
	fs := float64(rate)
	mid := fs / float64(max(1, int(math.Round(fs/midRateHz))))
	decim := int(math.Round(fs / mid))
	transition := 0.25 * mid
	coarseTaps := int(math.Ceil(4*fs/transition)) | 1
	fe.coarse = dsp.NewXlatingFIR(float64(offsetHz), fs, decim,
		dsp.DesignLowpass(0.4*mid, fs, coarseTaps, dsp.WindowBlackmanHarris))

	cutoff := min(float64(bandwidth)/2, 0.15*mid)
	fineCoeffs := dsp.DesignLowpass(cutoff, mid, fineTaps, dsp.WindowBlackmanHarris)
	fe.fine64 = dsp.NewXlatingFIR(0, mid, 1, fineCoeffs)
	fe.fine32 = dsp.NewXlatingFIR(0, mid, 2, fineCoeffs)

	fe.midRate = mid
	fe.passbandHz = 2 * cutoff
	fe.squelch.HoldSamples = int(squelchHold.Seconds() * mid)
	fe.prevCh = 0
}

type frontendResult struct {
	ch64      []complex64
	ch32      []complex64
	open      bool
	above     bool
	levelDBFS float32
}

// process прогоняет блок IQ через общий фронтенд ровно один раз.
func (fe *channelFrontend) process(
	f sdr.IQFrame,
	s sdr.ChannelSettings,
	dopplerHz float64,
	need32 bool,
) frontendResult {
	if f.SampleRate == 0 || len(f.Samples) == 0 {
		return frontendResult{}
	}
	if fe.coarse == nil || f.SampleRate != fe.rate || s.BandwidthHz != fe.bandwidth {
		fe.build(f.SampleRate, s.OffsetHz, s.BandwidthHz)
	}
	fe.coarse.SetShift(float64(s.OffsetHz) + dopplerHz)

	coarseOut := fe.coarse.Process(f.Samples)
	ch64 := fe.fine64.Process(coarseOut)
	var ch32 []complex64
	if need32 {
		ch32 = fe.fine32.Process(coarseOut)
	}
	fe.squelch.ThresholdDBFS = s.SquelchDBFS
	open := fe.squelch.Open(ch64)
	return frontendResult{
		ch64:      ch64,
		ch32:      ch32,
		open:      open,
		above:     fe.squelch.AboveThreshold(),
		levelDBFS: fe.squelch.LevelDBFS(),
	}
}

// measureCarrierSNR заполняет CarrierHz и SNRDB по блоку узкого канала 64 кГц.
func (fe *channelFrontend) measureCarrierSNR(ch64 []complex64, st *Status) {
	if len(ch64) == 0 {
		return
	}
	var sum complex128
	prev := complex128(fe.prevCh)
	for _, s := range ch64 {
		x := complex128(s)
		sum += x * cmplx.Conj(prev)
		prev = x
	}
	st.Measured = true
	st.CarrierHz = cmplx.Phase(sum) * fe.midRate / (2 * math.Pi)
	st.SNRDB = fe.noise.snrDB(ch64, fe.midRate, fe.passbandHz)
}

func (fe *channelFrontend) updatePrev(ch64 []complex64) {
	if len(ch64) > 0 {
		fe.prevCh = ch64[len(ch64)-1]
	}
}

// buildPackets оборачивает принятые сырые кадры в Packet с разбором AX.25.
func buildPackets(tsMs, freqHz uint64, frames [][]byte) []Packet {
	if len(frames) == 0 {
		return nil
	}
	out := make([]Packet, 0, len(frames))
	for _, b := range frames {
		var frame ax25.Frame
		if parsed, err := ax25.Parse(b); err == nil {
			frame = parsed
		}
		out = append(out, Packet{
			TimestampMs: tsMs,
			FrequencyHz: freqHz,
			Frame:       frame,
			Raw:         append([]byte(nil), b...),
		})
	}
	return out
}

// midRate — частота отсчётов после грубого фильтра: ближайшая к midRateHz, кратная частоте приёмника.
func midRate(fs float64) float64 {
	return fs / float64(max(1, int(math.Round(fs/midRateHz))))
}

// build строит фильтры канала и демодулятор. Грубый фильтр прореживает до midRateHz: пропускает
// плоско до 0,15 от новой частоты, подавляет с 0,65 — наложенное при прореживании ложится дальше
// 0,35 от центра, за точный фильтр. Поэтому «Полоса канала» шире 0,3 от неё обрезается.
func (c *Chain) build(rate uint32, offsetHz int64, bandwidth uint32, demod string, baud float64, p profile) {
	newProfile := demod != c.demodName
	newRate := rate != c.rate
	newOffset := offsetHz != c.offsetHz
	c.rate, c.offsetHz, c.bandwidth, c.demodName, c.baud = rate, offsetHz, bandwidth, demod, baud

	fs := float64(rate)
	target := float64(midRateHz)
	if p.midHz > 0 {
		target = p.midHz
	}
	mid := fs / float64(max(1, int(math.Round(fs/target))))
	decim := int(math.Round(fs / mid))
	transition := 0.25 * mid
	// Окно Блэкмана-Харриса: половина главного лепестка — 4 отсчёта спектра, то есть 4·fs/N.
	coarseTaps := int(math.Ceil(4*fs/transition)) | 1
	c.coarse = dsp.NewXlatingFIR(float64(offsetHz), fs, decim,
		dsp.DesignLowpass(0.4*mid, fs, coarseTaps, dsp.WindowBlackmanHarris))

	cutoff := min(float64(bandwidth)/2, 0.15*mid)
	if p.maxWidthHz > 0 {
		cutoff = min(cutoff, p.maxWidthHz/2)
	}
	taps := fineTaps
	if p.fineTaps > 0 {
		taps = p.fineTaps
	}
	c.fine = dsp.NewXlatingFIR(0, mid, p.fineDecim,
		dsp.DesignLowpass(cutoff, mid, taps, dsp.WindowBlackmanHarris))
	beatHz := p.toneHz + p.sideband*(ssbLowHz+cutoff)

	c.outRate = mid / float64(p.fineDecim)
	c.audioRx = nil
	switch {
	case p.newAudio != nil:
		c.picRx, c.status.Picture = nil, nil
		c.demod, c.deframer = nil, nil
		c.audioRx = p.newAudio(c.outRate, beatHz)
		c.status.Good, c.status.Bad = 0, 0
	case p.newPictures != nil:
		// Первый вход / смена сдвига: mid-join (hop на середину). После DemodOff — только VIS.
		if c.picRx == nil || newProfile || newRate || newOffset {
			if c.sstvVISOnly {
				c.picRx = newSSTVRxVISOnly(c.outRate)
				c.sstvVISOnly = false
			} else {
				c.picRx = p.newPictures(c.outRate)
			}
			c.status.Picture = nil
		}
		c.demod, c.deframer = nil, nil
		c.status.Bad = 0
	default:
		c.picRx, c.status.Picture = nil, nil
		switch {
		case p.newDemodBaud != nil:
			c.demod = p.newDemodBaud(c.outRate, baud)
		default:
			c.demod = p.newDemod(c.outRate)
		}
		if p.newDeframer == nil {
			// Линия без кадрового протокола: пачки по паузе/max (burstDeframer).
			// newDeframer в профиле остаётся nil — не попадает в «Авто» по CRC.
			c.deframer = newBurstDeframer()
		} else if c.deframer == nil || newProfile {
			c.deframer = p.newDeframer()
		}
	}
	c.squelch.HoldSamples = int(squelchHold.Seconds() * c.outRate)
	c.prevCh = 0
	m := &c.status
	m.PassbandHz = 2 * cutoff
	m.Measured, m.CarrierHz, m.DeviationHz, m.ClockJitter = false, 0, 0, 0
	m.SyncCounted, m.SyncWords, m.SyncBitErrors = false, 0, 0
	m.FECCounted, m.FECCorrected = false, 0
	if _, ok := c.deframer.(fecCounter); ok {
		m.FECCounted = true
	}
	m.SNRDB = 0
}
