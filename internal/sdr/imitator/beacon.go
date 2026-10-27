package imitator

import (
	"fmt"
	"math"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/geoscan"
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

// beaconModemLabels — название модема в тексте маяка: по строке таблицы телеметрии видно,
// какой из маяков принят.
var beaconModemLabels = map[BeaconModem]string{
	"":                   "AFSK 1200",
	BeaconAFSK1200:       "AFSK 1200",
	BeaconFSK9600G3RUH:   "FSK 9600 G3RUH",
	BeaconFSK9600GEOSCAN: "FSK 9600 GEOSCAN",
}

// geoscanBeaconInfo — маяк Geoscan с правдоподобной телеметрией; время борта постоянное.
var geoscanBeaconInfo = mustGeoscanBeacon(map[string]float64{
	"beacon_id":                     1,
	"eps_timestamp":                 float64(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC).Unix()),
	"eps_mode":                      3,
	"eps_switch_count":              2,
	"eps_consumption_current":       260,
	"eps_solar_cells_current":       410,
	"eps_cell_voltage_half":         3.71,
	"eps_cell_voltage_full":         7.42,
	"eps_systems_status":            0x0007,
	"eps_temperature_cell1":         18,
	"eps_temperature_cell2":         19,
	"eps_boot_count":                12,
	"obc_boot_count":                34,
	"obc_active_status":             0x0F,
	"obc_temperature_pos_x":         25,
	"obc_temperature_neg_x":         -12,
	"obc_temperature_pos_y":         31,
	"obc_temperature_neg_y":         -4,
	"gnss_sat_number":               8,
	"adcs_mode":                     2,
	"cam_photos_number":             17,
	"comm_type":                     3,
	"comm_bus_voltage":              5.02,
	"comm_boot_count":               9,
	"comm_rssi":                     -112,
	"comm_rssi_minimal":             -121,
	"comm_received_valid_packets":   14,
	"comm_received_invalid_packets": 2,
	"comm_sent_packets":             230,
	"comm_status":                   0x03,
	"comm_temperature":              22,
	"comm_qso_received":             3,
})

// mustGeoscanBeacon — значения заданы в коде, поэтому ошибка в них — ошибка программы.
func mustGeoscanBeacon(values map[string]float64) []byte {
	info, err := geoscan.BuildBeacon(values)
	if err != nil {
		panic(err)
	}
	return info
}

// DefaultBeaconFrame — кадр маяка имитатора: у GEOSCAN — маяк с телеметрией платформы,
// у остальных — название модема в тексте.
func DefaultBeaconFrame(m BeaconModem) ax25.Frame {
	if m == BeaconFSK9600GEOSCAN {
		return ax25.Frame{
			Dest: ax25.Address{Call: geoscan.BeaconDest},
			Src:  ax25.Address{Call: "SCOUT"},
			Info: append([]byte(nil), geoscanBeaconInfo...),
		}
	}
	info := "Satellite Scout imitator beacon"
	if label, ok := beaconModemLabels[m]; ok {
		info += ": " + label
	}
	return ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "SCOUT"},
		Info: []byte(info),
	}
}

// lineModulator переводит уровни линии в IQ; состояние сохраняется между вызовами.
type lineModulator interface {
	Modulate(dst []complex64, levels []byte) []complex64
}

// beaconGen выдаёт маяк без сдвига: кадр AX.25 в канальном кодировании модема поверх FM,
// затем тишина до конца периода.
type beaconGen struct {
	sampleRate    float64
	levels        []byte // уровни линии одного кадра
	baud          float64
	newMod        func() lineModulator
	periodSamples int

	mod         lineModulator
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
		periodSamples: int(sig.Period.Seconds() * sampleRate),
	}
	hdlc := ax25.NRZI(ax25.HDLCBits(frame, beaconPreambleFlags, beaconPostambleFlags))
	switch sig.Modem {
	case "", BeaconAFSK1200:
		cfg := modem.AFSK1200(sampleRate)
		b.levels = hdlc
		b.baud = cfg.Baud
		b.newMod = func() lineModulator { return modem.NewAFSK(cfg) }
	case BeaconFSK9600G3RUH:
		cfg := modem.FSK9600(sampleRate)
		var s modem.G3RUHScrambler
		b.levels = s.Scramble(nil, hdlc)
		b.baud = cfg.Baud
		b.newMod = func() lineModulator { return modem.NewFSK(cfg) }
	case BeaconFSK9600GEOSCAN:
		cfg := modem.FSK9600(sampleRate)
		if b.levels, err = geoscanLevels(frame); err != nil {
			return nil, err
		}
		b.baud = cfg.Baud
		b.newMod = func() lineModulator { return modem.NewFSK(cfg) }
	default:
		return nil, fmt.Errorf("%w: unknown beacon modem %q", ErrInvalidSignal, sig.Modem)
	}
	b.startBurst()
	return b, nil
}

// geoscanLevels — уровни линии кадра GEOSCAN: кадр AX.25 без FCS, дополненный нулями
// до длинного кадра; преамбула той же длины, что флаги перед кадром HDLC.
func geoscanLevels(frame []byte) ([]byte, error) {
	body := frame[:len(frame)-2]
	if len(body) > geoscan.LongDataLen {
		return nil, fmt.Errorf("%w: frame of %d bytes does not fit into GEOSCAN %d", ErrInvalidSignal,
			len(body), geoscan.LongDataLen)
	}
	data := make([]byte, geoscan.LongDataLen)
	copy(data, body)
	return geoscan.Encode(data, beaconPreambleFlags, beaconPostambleFlags)
}

// burstSamples — длительность одного кадра в отсчётах.
func (b *beaconGen) burstSamples() int {
	return int(math.Round(float64(len(b.levels)) * b.sampleRate / b.baud))
}

func (b *beaconGen) startBurst() {
	b.mod = b.newMod()
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
