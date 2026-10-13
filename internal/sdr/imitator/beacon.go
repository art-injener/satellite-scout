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
	symbolChunk = 64
)

// DefaultBeaconPeriod — интервал между кадрами маяка, если в конфиге он не задан.
const DefaultBeaconPeriod = 10 * time.Second

// DefaultBeaconFrame — кадр маяка имитатора.
func DefaultBeaconFrame() ax25.Frame {
	return ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "SCOUT"},
		Info: []byte("Satellite Scout imitator beacon"),
	}
}

// beaconGen выдаёт маяк без сдвига: кадр AX.25 в AFSK 1200 поверх FM, затем тишина до конца периода.
type beaconGen struct {
	sampleRate    float64
	levels        []byte // уровни линии одного кадра после NRZI
	afsk          modem.AFSKConfig
	periodSamples int

	mod         *modem.AFSK
	modBuf      []complex64 // буфер модулятора, переиспользуется
	pending     []complex64 // промодулировано, но ещё не выдано (часть modBuf)
	symbol      int         // следующий символ кадра
	inBurst     bool
	silenceLeft int // отсчётов тишины до следующего кадра
}

func newBeaconGen(sig Signal, sampleRate float64) (*beaconGen, error) {
	frame, err := sig.Frame.Bytes()
	if err != nil {
		return nil, fmt.Errorf("beacon frame: %w", err)
	}
	b := &beaconGen{
		sampleRate:    sampleRate,
		levels:        ax25.NRZI(ax25.HDLCBits(frame, beaconPreambleFlags, beaconPostambleFlags)),
		afsk:          modem.AFSK1200(sampleRate),
		periodSamples: int(sig.Period.Seconds() * sampleRate),
	}
	b.startBurst()
	return b, nil
}

// burstSamples — длительность одного кадра в отсчётах.
func (b *beaconGen) burstSamples() int {
	return int(math.Round(float64(len(b.levels)) * b.sampleRate / b.afsk.Baud))
}

func (b *beaconGen) startBurst() {
	b.mod = modem.NewAFSK(b.afsk)
	b.pending = nil
	b.symbol = 0
	b.inBurst = true
}

func (b *beaconGen) fillClean(dst []complex64) {
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
			end := min(b.symbol+symbolChunk, len(b.levels))
			b.modBuf = b.mod.Modulate(b.modBuf[:0], b.levels[b.symbol:end])
			b.pending = b.modBuf
			b.symbol = end
		}

		n := copy(dst[i:], b.pending)
		b.pending = b.pending[n:]
		i += n
	}
}
