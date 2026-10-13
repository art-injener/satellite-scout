package imitator

import (
	"fmt"
	"math"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/modem"
)

const (
	// Флаги перед кадром: передатчик выходит на режим, приёмник ловит синхронизацию.
	beaconPreambleFlags  = 32
	beaconPostambleFlags = 2
	// Сколько символов модулировать за раз при заполнении блока.
	beaconSymbolChunk = 64
)

// DefaultBeaconHz — частота маяка по умолчанию: любительский спутниковый участок 435–438 МГц.
const DefaultBeaconHz = 436_500_000

// DefaultBeaconOptions — имитатор с маяком на DefaultBeaconHz: кадр раз в 10 с, SNR 15 дБ в канале 12,5 кГц.
// Уровень −40 dBFS опускает шум по всей полосе ниже полной шкалы, как у реального приёмника.
func DefaultBeaconOptions() Options {
	return Options{Beacon: &BeaconConfig{
		FrequencyHz: DefaultBeaconHz,
		Frame: ax25.Frame{
			Dest: ax25.Address{Call: "CQ"},
			Src:  ax25.Address{Call: "SCOUT"},
			Info: []byte("Satellite Scout imitator beacon"),
		},
		Period:           10 * time.Second,
		SNRdB:            15,
		NoiseBandwidthHz: 12_500,
		LevelDBFS:        -40,
		Seed:             1,
	}}
}

// BeaconConfig — маяк КА: кадр AX.25 в AFSK 1200 поверх FM, раз в Period.
type BeaconConfig struct {
	FrequencyHz uint64 // несущая передатчика
	Frame       ax25.Frame
	Period      time.Duration // интервал между началами кадров

	// DopplerHz — сдвиг Доплера в момент t от начала потока. nil — без Доплера.
	DopplerHz func(t time.Duration) float64

	SNRdB            float64 // SNR в полосе NoiseBandwidthHz
	NoiseBandwidthHz float64 // 0 — без шума
	// LevelDBFS — мощность сигнала на входе приёмника, dBFS; шум масштабируется вместе с ним,
	// SNR не меняется. 0 — полная шкала.
	LevelDBFS float64
	Seed      uint64
}

// beaconSource генерирует IQ маяка блоками. Не потокобезопасен: работает в горутине потока.
type beaconSource struct {
	cfg        BeaconConfig
	sampleRate float64
	levels     []byte // уровни линии одного кадра после NRZI
	afsk       modem.AFSKConfig
	channel    *modem.Channel

	mod           *modem.AFSK
	modBuf        []complex64 // буфер модулятора, переиспользуется
	pending       []complex64 // промодулировано, но ещё не выдано (часть modBuf)
	symbol        int         // следующий символ кадра
	inBurst       bool
	silenceLeft   int // отсчётов тишины до следующего кадра
	periodSamples int

	centerHz uint64 // частота настройки приёмника для текущего блока
	emitted  int    // отсчётов выдано с начала потока
	clean    []complex64
	gain     float32 // амплитудный множитель уровня LevelDBFS
}

func newBeaconSource(cfg BeaconConfig, sampleRate float64) (*beaconSource, error) {
	frame, err := cfg.Frame.Bytes()
	if err != nil {
		return nil, fmt.Errorf("beacon frame: %w", err)
	}

	b := &beaconSource{
		cfg:           cfg,
		sampleRate:    sampleRate,
		levels:        ax25.NRZI(ax25.HDLCBits(frame, beaconPreambleFlags, beaconPostambleFlags)),
		afsk:          modem.AFSK1200(sampleRate),
		periodSamples: int(cfg.Period.Seconds() * sampleRate),
		gain:          float32(math.Pow(10, cfg.LevelDBFS/20)),
	}
	b.channel = modem.NewChannel(modem.ChannelConfig{
		SampleRate:       sampleRate,
		OffsetHz:         b.offsetHz,
		SNRdB:            cfg.SNRdB,
		NoiseBandwidthHz: cfg.NoiseBandwidthHz,
		Seed:             cfg.Seed,
	})
	b.startBurst()
	return b, nil
}

// burstSamples — длительность одного кадра в отсчётах.
func (b *beaconSource) burstSamples() int {
	return int(math.Round(float64(len(b.levels)) * b.sampleRate / b.afsk.Baud))
}

// offsetHz — сдвиг сигнала от центра приёмника: расстройка плюс Доплер.
func (b *beaconSource) offsetHz(t time.Duration) float64 {
	off := float64(b.cfg.FrequencyHz) - float64(b.centerHz)
	if b.cfg.DopplerHz != nil {
		off += b.cfg.DopplerHz(t)
	}
	return off
}

func (b *beaconSource) startBurst() {
	b.mod = modem.NewAFSK(b.afsk)
	b.pending = nil
	b.symbol = 0
	b.inBurst = true
}

// fill заполняет dst отсчётами маяка для приёмника, настроенного на centerHz.
func (b *beaconSource) fill(dst []complex64, centerHz uint64) {
	b.centerHz = centerHz
	if cap(b.clean) < len(dst) {
		b.clean = make([]complex64, len(dst))
	}
	clean := b.clean[:len(dst)]
	b.fillClean(clean)

	// Сигнал за пределами полосы приёмника не виден: без этого сдвиг частоты дал бы наложение спектра.
	t := time.Duration(float64(b.emitted) / b.sampleRate * float64(time.Second))
	if math.Abs(b.offsetHz(t)) > b.sampleRate/2 {
		clear(clean)
	}
	b.emitted += len(dst)

	b.channel.Process(dst[:0], clean)
	if b.gain != 1 {
		g := complex(b.gain, 0)
		for i := range dst {
			dst[i] *= g
		}
	}
}

// fillClean пишет в dst сигнал маяка без сдвига и шума: кадр, затем тишина до конца периода.
func (b *beaconSource) fillClean(dst []complex64) {
	i := 0
	for i < len(dst) {
		if !b.inBurst {
			n := min(b.silenceLeft, len(dst)-i)
			clear(dst[i : i+n])
			i += n
			b.silenceLeft -= n
			if b.silenceLeft == 0 {
				b.startBurst()
			}
			continue
		}

		if len(b.pending) == 0 {
			if b.symbol == len(b.levels) {
				b.inBurst = false
				b.silenceLeft = max(b.periodSamples-b.burstSamples(), 0)
				if b.silenceLeft == 0 {
					b.startBurst()
				}
				continue
			}
			end := min(b.symbol+beaconSymbolChunk, len(b.levels))
			b.modBuf = b.mod.Modulate(b.modBuf[:0], b.levels[b.symbol:end])
			b.pending = b.modBuf
			b.symbol = end
		}

		n := copy(dst[i:], b.pending)
		b.pending = b.pending[n:]
		i += n
	}
}
