package usp

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
)

// Типы сообщений USP внутри AX.25 (EtherType-подобные, LE).
// Источник раскладки: SatsDecoder systems/usp.py и satnogs-decoders sputnixusp.ksy.
const (
	TypeBeacon         uint16 = 0x4216 // общий маяк СЭП+УКВ
	TypeExtendedBeacon uint16 = 0x4217 // расширенный маяк (магнит, ПН)
	TypeUHFBeacon      uint16 = 0x4246 // короткий УКВ-маяк

	msgHeaderLen = 8 // type + sender + receiver + size

	BeaconGeneralLen  = 66
	BeaconUHFLen      = 25
	BeaconExtendedLen = 81
)

// Param — поле маяка для таблицы ТМИ.
type Param struct {
	ID    string
	Name  string
	Value string
	Unit  string
	Key   bool
}

type fieldKind uint8

const (
	kindU16 fieldKind = iota
	kindS16
	kindU8
	kindS8
	kindU32
	kindUnix
	kindRSSI // uint8, значение = −raw дБм
	kindMV   // uint16/int16 мВ → вольты с запятой
	kindSMV  // int16 мВ → вольты
	kindF32  // float32 LE
	kindHex
)

type beaconField struct {
	id   string
	name string
	size int
	kind fieldKind
	unit string
	key  bool
}

// generalLayout — тело 0x4216 после заголовка сообщения.
var generalLayout = []beaconField{
	{id: "usb1", name: "Напряжение СБ 1", size: 2, kind: kindMV, unit: "В"},
	{id: "usb2", name: "Напряжение СБ 2", size: 2, kind: kindMV, unit: "В"},
	{id: "usb3", name: "Напряжение СБ 3", size: 2, kind: kindMV, unit: "В"},
	{id: "isb1", name: "Ток СБ 1", size: 2, kind: kindU16, unit: "мА"},
	{id: "isb2", name: "Ток СБ 2", size: 2, kind: kindU16, unit: "мА"},
	{id: "isb3", name: "Ток СБ 3", size: 2, kind: kindU16, unit: "мА"},
	{id: "iab", name: "Ток АКБ", size: 2, kind: kindS16, unit: "мА", key: true},
	{id: "ich1", name: "Ток канала 1", size: 2, kind: kindU16, unit: "мА"},
	{id: "ich2", name: "Ток канала 2", size: 2, kind: kindU16, unit: "мА"},
	{id: "ich3", name: "Ток канала 3", size: 2, kind: kindU16, unit: "мА"},
	{id: "ich4", name: "Ток канала 4", size: 2, kind: kindU16, unit: "мА"},
	{id: "t1_pw", name: "Температура АКБ 1", size: 2, kind: kindS16, unit: "°C", key: true},
	{id: "t2_pw", name: "Температура АКБ 2", size: 2, kind: kindS16, unit: "°C"},
	{id: "t3_pw", name: "Температура АКБ 3", size: 2, kind: kindS16, unit: "°C"},
	{id: "t4_pw", name: "Температура АКБ 4", size: 2, kind: kindS16, unit: "°C"},
	{id: "flags0", name: "Флаги СЭП", size: 4, kind: kindU32},
	{id: "uab", name: "Напряжение АКБ", size: 2, kind: kindSMV, unit: "В", key: true},
	{id: "reg_tel_id", name: "№ телеметрии СЭП", size: 4, kind: kindU32},
	{id: "time_ps", name: "Время СЭП", size: 4, kind: kindUnix, unit: "UTC"},
	{id: "nres_ps", name: "Перезагрузки СЭП", size: 1, kind: kindU8},
	{id: "fl_ps", name: "Флаги PS", size: 1, kind: kindU8},
	{id: "t_amp", name: "Температура УМ УКВ", size: 1, kind: kindS8, unit: "°C"},
	{id: "t_uhf", name: "Температура УКВ", size: 1, kind: kindS8, unit: "°C", key: true},
	{id: "rssi_rx", name: "RSSI приём", size: 1, kind: kindRSSI, unit: "дБм", key: true},
	{id: "rssi_idle", name: "RSSI простой", size: 1, kind: kindRSSI, unit: "дБм"},
	{id: "pf", name: "Мощность прямая", size: 1, kind: kindS8, unit: "дБм"},
	{id: "pb", name: "Мощность обратная", size: 1, kind: kindS8, unit: "дБм"},
	{id: "nres_uhf", name: "Перезагрузки УКВ", size: 1, kind: kindU8},
	{id: "fl_uhf", name: "Флаги УКВ", size: 1, kind: kindU8},
	{id: "time_uhf", name: "Время УКВ", size: 4, kind: kindUnix, unit: "UTC"},
	{id: "uptime_uhf", name: "Uptime УКВ", size: 4, kind: kindU32, unit: "с", key: true},
	{id: "current_uhf", name: "Ток УКВ", size: 2, kind: kindU16, unit: "мА"},
	{id: "uuhf", name: "Напряжение УКВ", size: 2, kind: kindSMV, unit: "В"},
}

// extendedLayout — тело 0x4217 (SatsDecoder extended_beacon).
var extendedLayout = []beaconField{
	{id: "t_mb", name: "Температура платы", size: 2, kind: kindS16, unit: "°C", key: true},
	{id: "mx", name: "Магнит X", size: 4, kind: kindF32, unit: "мкТл"},
	{id: "my", name: "Магнит Y", size: 4, kind: kindF32, unit: "мкТл"},
	{id: "mz", name: "Магнит Z", size: 4, kind: kindF32, unit: "мкТл", key: true},
	{id: "vx", name: "Скорость X", size: 4, kind: kindF32, unit: "°/с"},
	{id: "vy", name: "Скорость Y", size: 4, kind: kindF32, unit: "°/с"},
	{id: "vz", name: "Скорость Z", size: 4, kind: kindF32, unit: "°/с"},
	{id: "nres", name: "Перезагрузки", size: 1, kind: kindU8, key: true},
	{id: "rcon", name: "Причина сброса", size: 1, kind: kindHex},
	{id: "fl", name: "Флаги", size: 1, kind: kindHex},
	{id: "time", name: "Время борта", size: 4, kind: kindUnix, unit: "UTC", key: true},
	{id: "fl_pl", name: "Флаги ПН", size: 1, kind: kindHex},
	{id: "time_send", name: "Время ПН", size: 4, kind: kindUnix, unit: "UTC"},
	{id: "t_plate", name: "Температура ПН", size: 2, kind: kindS16, unit: "°C"},
	{id: "t_cpu", name: "Температура MCU ПН", size: 2, kind: kindS16, unit: "°C", key: true},
	{id: "cursens1", name: "Ток ПН 1", size: 2, kind: kindU16, unit: "мА"},
	{id: "cursens2", name: "Ток ПН 2", size: 2, kind: kindU16, unit: "мА"},
	{id: "nrst", name: "Сбросы ПН", size: 1, kind: kindU8},
	{id: "time_rst", name: "Время сброса ПН", size: 4, kind: kindUnix, unit: "UTC"},
	{id: "ch1_rate", name: "Счёт канал 1", size: 2, kind: kindU16},
	{id: "ch2_rate", name: "Счёт канал 2", size: 2, kind: kindU16},
	{id: "ch3_rate", name: "Счёт канал 3", size: 2, kind: kindU16},
	{id: "ch4_rate", name: "Счёт канал 4", size: 2, kind: kindU16},
	{id: "ch5_rate", name: "Счёт канал 5", size: 2, kind: kindU16},
	{id: "ch6_rate", name: "Счёт канал 6", size: 2, kind: kindU16},
	{id: "ptr_end1", name: "Указ. конец 1", size: 2, kind: kindHex},
	{id: "ptr_crt1", name: "Указ. запись 1", size: 2, kind: kindHex},
	{id: "ptr_end2", name: "Указ. конец 2", size: 2, kind: kindHex},
	{id: "ptr_crt2", name: "Указ. запись 2", size: 2, kind: kindHex},
	{id: "ptr_end3", name: "Указ. конец 3", size: 2, kind: kindHex},
	{id: "ptr_crt3", name: "Указ. запись 3", size: 2, kind: kindHex},
	{id: "ev_ch1_1", name: "Событие CH1-1", size: 1, kind: kindHex},
	{id: "ev_ch1_2", name: "Событие CH1-2", size: 1, kind: kindHex},
	{id: "ev_ch1_3", name: "Событие CH1-3", size: 1, kind: kindHex},
	{id: "ev_ch2_1", name: "Событие CH2-1", size: 1, kind: kindHex},
	{id: "ev_ch2_2", name: "Событие CH2-2", size: 1, kind: kindHex},
	{id: "ev_ch2_3", name: "Событие CH2-3", size: 1, kind: kindHex},
}

// uhfLayout — тело 0x4246.
var uhfLayout = []beaconField{
	{id: "t_amp", name: "Температура УМ УКВ", size: 1, kind: kindS8, unit: "°C"},
	{id: "t_uhf", name: "Температура УКВ", size: 1, kind: kindS8, unit: "°C", key: true},
	{id: "rssi_rx", name: "RSSI приём", size: 1, kind: kindRSSI, unit: "дБм", key: true},
	{id: "rssi_idle", name: "RSSI простой", size: 1, kind: kindRSSI, unit: "дБм"},
	{id: "pf", name: "Мощность прямая", size: 1, kind: kindS8, unit: "дБм"},
	{id: "pb", name: "Мощность обратная", size: 1, kind: kindS8, unit: "дБм"},
	{id: "nres_uhf", name: "Перезагрузки УКВ", size: 1, kind: kindU8},
	{id: "fl_uhf", name: "Флаги УКВ", size: 1, kind: kindU8},
	{id: "time_uhf", name: "Время УКВ", size: 4, kind: kindUnix, unit: "UTC"},
	{id: "uptime_uhf", name: "Uptime УКВ", size: 4, kind: kindU32, unit: "с", key: true},
	{id: "rx_bitrate", name: "Скорость uplink", size: 4, kind: kindU32, unit: "бит/с"},
	{id: "current_uhf", name: "Ток УКВ", size: 2, kind: kindU16, unit: "мА"},
	{id: "uuhf", name: "Напряжение УКВ", size: 2, kind: kindSMV, unit: "В", key: true},
	{id: "sch_flags", name: "Флаги расписания", size: 1, kind: kindU8},
}

// BeaconParams разбирает AX.25 (с FCS или без) с сообщением USP-маяка.
// kind: "beacon" | "uhf_beacon"; ok=false — формат не подтверждён.
func BeaconParams(raw []byte) ([]Param, string, bool) {
	info, _, ok := ax25Info(raw)
	if !ok {
		return nil, "", false
	}
	off := 0
	for off+msgHeaderLen <= len(info) {
		msgType := binary.LittleEndian.Uint16(info[off:])
		size := int(binary.LittleEndian.Uint16(info[off+6:]))
		off += msgHeaderLen
		if size < 0 || off+size > len(info) {
			return nil, "", false
		}
		body := info[off : off+size]
		off += size
		switch msgType {
		case TypeBeacon:
			if len(body) < BeaconGeneralLen {
				continue
			}
			return decodeLayout(generalLayout, body[:BeaconGeneralLen]), "beacon", true
		case TypeExtendedBeacon:
			if len(body) < BeaconExtendedLen {
				continue
			}
			return decodeLayout(extendedLayout, body[:BeaconExtendedLen]), "extended_beacon", true
		case TypeUHFBeacon:
			if len(body) < BeaconUHFLen {
				continue
			}
			return decodeLayout(uhfLayout, body[:BeaconUHFLen]), "uhf_beacon", true
		default:
			continue
		}
	}
	return nil, "", false
}

// CallsignsFromRaw — позывные AX.25 для Src/Dest в конверте payload.
func CallsignsFromRaw(raw []byte) (src, dest string, ok bool) {
	body := stripFCS(raw)
	f, err := ax25.Parse(body)
	if err != nil {
		return "", "", false
	}
	return formatCall(f.Src), formatCall(f.Dest), true
}

func ax25Info(raw []byte) (info []byte, src string, ok bool) {
	body := stripFCS(raw)
	f, err := ax25.Parse(body)
	if err != nil {
		return nil, "", false
	}
	if len(f.Info) < msgHeaderLen {
		return nil, "", false
	}
	return f.Info, formatCall(f.Src), true
}

func stripFCS(raw []byte) []byte {
	if len(raw) < 2 {
		return raw
	}
	body := raw[:len(raw)-2]
	want := binary.LittleEndian.Uint16(raw[len(raw)-2:])
	if ax25.FCS(body) == want {
		return body
	}
	return raw
}

func formatCall(a ax25.Address) string {
	if a.SSID == 0 {
		return a.Call
	}
	return fmt.Sprintf("%s-%d", a.Call, a.SSID)
}

func decodeLayout(layout []beaconField, body []byte) []Param {
	out := make([]Param, 0, len(layout))
	off := 0
	for _, fd := range layout {
		if off+fd.size > len(body) {
			break
		}
		out = append(out, Param{
			ID:    fd.id,
			Name:  fd.name,
			Value: fd.format(body[off : off+fd.size]),
			Unit:  fd.unit,
			Key:   fd.key,
		})
		off += fd.size
	}
	return out
}

func (fd beaconField) format(b []byte) string {
	switch fd.kind {
	case kindU8:
		return strconv.FormatUint(uint64(b[0]), 10)
	case kindS8:
		return strconv.FormatInt(int64(int8(b[0])), 10)
	case kindRSSI:
		return strconv.FormatInt(-int64(b[0]), 10)
	case kindU16:
		return strconv.FormatUint(uint64(binary.LittleEndian.Uint16(b)), 10)
	case kindS16:
		return strconv.FormatInt(int64(int16(binary.LittleEndian.Uint16(b))), 10)
	case kindMV:
		v := float64(binary.LittleEndian.Uint16(b)) / 1000
		return formatComma(v)
	case kindSMV:
		v := float64(int16(binary.LittleEndian.Uint16(b))) / 1000
		return formatComma(v)
	case kindU32:
		return strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b)), 10)
	case kindUnix:
		sec := int64(int32(binary.LittleEndian.Uint32(b)))
		if sec <= 0 {
			return "—"
		}
		return time.Unix(sec, 0).UTC().Format("2006-01-02 15:04:05")
	case kindF32:
		bits := binary.LittleEndian.Uint32(b)
		v := float64(math.Float32frombits(bits))
		s := strconv.FormatFloat(v, 'f', 2, 64)
		return strings.Replace(s, ".", ",", 1)
	case kindHex:
		return fmt.Sprintf("%X", b)
	default:
		return fmt.Sprintf("%X", b)
	}
}

func formatComma(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	return strings.Replace(s, ".", ",", 1)
}

// PackMessage собирает заголовок USP + тело (type/sender/receiver/size + data).
func PackMessage(msgType, sender, receiver uint16, body []byte) []byte {
	out := make([]byte, msgHeaderLen+len(body))
	binary.LittleEndian.PutUint16(out[0:], msgType)
	binary.LittleEndian.PutUint16(out[2:], sender)
	binary.LittleEndian.PutUint16(out[4:], receiver)
	binary.LittleEndian.PutUint16(out[6:], uint16(len(body)))
	copy(out[msgHeaderLen:], body)
	return out
}

// SampleGeneralBody — правдоподобное тело 0x4216 для имитатора и unit.
func SampleGeneralBody() []byte {
	body := make([]byte, BeaconGeneralLen)
	binary.LittleEndian.PutUint16(body[0:], 8200)
	binary.LittleEndian.PutUint16(body[2:], 8100)
	binary.LittleEndian.PutUint16(body[4:], 8000)
	binary.LittleEndian.PutUint16(body[6:], 150)
	binary.LittleEndian.PutUint16(body[8:], 140)
	binary.LittleEndian.PutUint16(body[10:], 130)
	putI16 := func(off int, v int16) { binary.LittleEndian.PutUint16(body[off:], uint16(v)) }
	putI16(12, -20) // ток АКБ, мА
	binary.LittleEndian.PutUint16(body[14:], 40)
	putI16(22, 22)
	putI16(34, 7420) // напряжение АКБ, мВ
	binary.LittleEndian.PutUint32(body[36:], 42)
	binary.LittleEndian.PutUint32(body[40:], uint32(time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC).Unix())) //nolint:gosec
	body[46] = 15
	body[47] = 18
	body[48] = 55
	body[49] = 95
	binary.LittleEndian.PutUint32(body[58:], 86400)
	binary.LittleEndian.PutUint16(body[62:], 210)
	putI16(64, 3300)
	return body
}

// SampleGeneralAX25 — кадр AX.25 с FCS: маяк 0x4216 (для USP Encode).
func SampleGeneralAX25() ([]byte, error) {
	info := PackMessage(TypeBeacon, 1, 2, SampleGeneralBody())
	return ax25.Frame{
		Dest: ax25.Address{Call: "CQ"},
		Src:  ax25.Address{Call: "RS32S"},
		Info: info,
	}.Bytes()
}
