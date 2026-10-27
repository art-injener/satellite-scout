package geoscan

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
)

var (
	// ErrUnknownField — в раскладке маяка нет поля с таким идентификатором.
	ErrUnknownField = errors.New("unknown geoscan beacon field")
	// ErrOutOfRange — значение не помещается в поле маяка.
	ErrOutOfRange = errors.New("geoscan beacon value out of range")
)

const (
	// BeaconLen — длина поля данных AX.25 маяка платформы Geoscan (кадр 74 байта).
	BeaconLen = 56
	// BeaconDest — получатель кадров маяка; кадры GNSS и снимков идут без заголовка AX.25.
	BeaconDest = "BEACON"
)

// Param — параметр маяка в виде для показа.
type Param struct {
	ID    string
	Name  string
	Value string
	Unit  string
	Key   bool // ключевой: показывается в строке таблицы, остальные — по запросу
}

type fieldKind uint8

const (
	kindUint fieldKind = iota
	kindInt
	kindTime // время Unix, секунды
	kindBits // битовое поле, показывается в hex
	kindEnum
)

// beaconField — поле маяка; поля идут подряд, little-endian.
type beaconField struct {
	id    string
	name  string
	size  int
	kind  fieldKind
	scale float64 // множитель сырого значения; 0 — целое без масштаба
	unit  string
	key   bool
	enum  map[uint64]string
}

// beaconLayout — раскладка маяка Geoscan-1…6 и 239Alferov. Источник описания формата —
// satnogs-decoders, ksy/geoscan.ksy (названия, типы и единицы полей).
var beaconLayout = []beaconField{
	{id: "beacon_id", name: "ID маяка", size: 1},
	{id: "eps_timestamp", name: "Время борта", size: 4, kind: kindTime, unit: "UTC", key: true},
	{id: "eps_mode", name: "Режим СЭП", size: 1, kind: kindEnum, key: true, enum: map[uint64]string{
		0: "инициализация", 1: "резерв", 2: "безопасный", 3: "штатный",
	}},
	{id: "eps_switch_count", name: "Переключения СЭП", size: 1},
	{id: "eps_consumption_current", name: "Ток потребления", size: 2, unit: "мА", key: true},
	{id: "eps_solar_cells_current", name: "Ток солнечных батарей", size: 2, unit: "мА", key: true},
	{id: "eps_cell_voltage_half", name: "Напряжение банки АКБ", size: 2, scale: 0.001, unit: "В"},
	{id: "eps_cell_voltage_full", name: "Напряжение АКБ", size: 2, scale: 0.001, unit: "В", key: true},
	{id: "eps_systems_status", name: "Статус систем", size: 2, kind: kindBits},
	{id: "eps_temperature_cell1", name: "Температура банки 1", size: 1, kind: kindInt, unit: "°C", key: true},
	{id: "eps_temperature_cell2", name: "Температура банки 2", size: 1, kind: kindInt, unit: "°C", key: true},
	{id: "eps_boot_count", name: "Загрузки СЭП", size: 2},
	{id: "eps_heater_mode", name: "Режим нагревателя", size: 1, kind: kindBits},
	{id: "eps_reserved", name: "Резерв СЭП", size: 2, kind: kindBits},
	{id: "obc_boot_count", name: "Загрузки БК", size: 2},
	{id: "obc_active_status", name: "Активные устройства БК", size: 1, kind: kindBits},
	{id: "obc_temperature_pos_x", name: "Температура панели X+", size: 1, kind: kindInt, unit: "°C"},
	{id: "obc_temperature_neg_x", name: "Температура панели X−", size: 1, kind: kindInt, unit: "°C"},
	{id: "obc_temperature_pos_y", name: "Температура панели Y+", size: 1, kind: kindInt, unit: "°C"},
	{id: "obc_temperature_neg_y", name: "Температура панели Y−", size: 1, kind: kindInt, unit: "°C"},
	{id: "gnss_sat_number", name: "Спутники GNSS", size: 1, key: true},
	{id: "adcs_mode", name: "Режим ориентации", size: 1, kind: kindEnum, enum: map[uint64]string{
		0: "выключена", 1: "B-dot", 2: "трёхосная",
	}},
	{id: "adcs_reserved", name: "Резерв ориентации", size: 1, kind: kindBits},
	{id: "cam_photos_number", name: "Снимков", size: 1},
	{id: "cam_mode", name: "Режим камеры", size: 1, kind: kindEnum, enum: map[uint64]string{
		0: "ожидание", 1: "снимок", 2: "серия", 3: "видео", 4: "управление", 255: "выключена",
	}},
	{id: "cam_reserved", name: "Резерв камеры", size: 4, kind: kindBits},
	{id: "comm_type", name: "Модуль связи", size: 1, kind: kindEnum, enum: map[uint64]string{
		3: "№1", 13: "№2",
	}},
	{id: "comm_bus_voltage", name: "Напряжение шины", size: 2, scale: 0.001, unit: "В"},
	{id: "comm_boot_count", name: "Загрузки модуля связи", size: 2},
	{id: "comm_rssi", name: "RSSI", size: 1, kind: kindInt, unit: "дБм", key: true},
	{id: "comm_rssi_minimal", name: "RSSI минимальный", size: 1, kind: kindInt, unit: "дБм"},
	{id: "comm_received_valid_packets", name: "Принято пакетов", size: 1},
	{id: "comm_received_invalid_packets", name: "Принято битых пакетов", size: 1},
	{id: "comm_sent_packets", name: "Отправлено пакетов", size: 1},
	{id: "comm_status", name: "Статус модуля связи", size: 1, kind: kindBits},
	{id: "comm_mode", name: "Режим связи", size: 1, kind: kindEnum, enum: map[uint64]string{
		0: "I", 1: "III", 2: "II",
	}},
	{id: "comm_temperature", name: "Температура модуля связи", size: 1, kind: kindInt, unit: "°C"},
	{id: "comm_qso_received", name: "Принято QSO", size: 1},
	{id: "comm_reserved", name: "Резерв связи", size: 2, kind: kindBits},
}

// BeaconParams разбирает маяк платформы Geoscan. ok=false — кадр не маяк: другой получатель
// или длина данных.
func BeaconParams(f ax25.Frame) ([]Param, bool) {
	if len(f.Info) != BeaconLen || !isBeaconDest(f.Dest.Call) {
		return nil, false
	}
	params := make([]Param, 0, len(beaconLayout))
	off := 0
	for _, fd := range beaconLayout {
		params = append(params, Param{
			ID:    fd.id,
			Name:  fd.name,
			Value: fd.format(f.Info[off : off+fd.size]),
			Unit:  fd.unit,
			Key:   fd.key,
		})
		off += fd.size
	}
	return params, true
}

// BuildBeacon собирает данные маяка из значений полей в единицах показа; остальные поля нулевые.
func BuildBeacon(values map[string]float64) ([]byte, error) {
	info := make([]byte, BeaconLen)
	offsets := make(map[string]int, len(beaconLayout))
	off := 0
	for _, fd := range beaconLayout {
		offsets[fd.id] = off
		off += fd.size
	}
	for id, v := range values {
		fd, found := fieldByID(id)
		if !found {
			return nil, fmt.Errorf("%w: %q", ErrUnknownField, id)
		}
		if fd.scale != 0 {
			v /= fd.scale
		}
		v = math.Round(v)
		full := math.Exp2(float64(8 * fd.size))
		lo, hi := 0.0, full
		if fd.kind == kindInt {
			lo, hi = -full/2, full/2
		}
		if v < lo || v >= hi {
			return nil, fmt.Errorf("%w: field %q, value %v", ErrOutOfRange, id, values[id])
		}
		if v < 0 {
			v += full
		}
		raw := uint64(v)
		for i := range fd.size {
			info[offsets[id]+i] = byte(raw >> (8 * i) & 0xFF)
		}
	}
	return info, nil
}

// isBeaconDest — получатель маяка: «BEACON» или позывной RS…, как у кадров Geoscan.
func isBeaconDest(call string) bool {
	return strings.HasPrefix(call, "BE") || strings.HasPrefix(call, "RS")
}

func fieldByID(id string) (beaconField, bool) {
	for _, fd := range beaconLayout {
		if fd.id == id {
			return fd, true
		}
	}
	return beaconField{}, false
}

// format — значение поля для показа; дробная часть — через запятую.
func (fd beaconField) format(b []byte) string {
	var raw uint64
	for i, c := range b {
		raw |= uint64(c) << (8 * i)
	}
	switch fd.kind {
	case kindTime:
		return time.Unix(int64(binary.LittleEndian.Uint32(b)), 0).UTC().Format("02.01.2006 15:04:05")
	case kindBits:
		return fmt.Sprintf("0x%0*X", 2*len(b), raw)
	case kindEnum:
		if name, ok := fd.enum[raw]; ok {
			return name
		}
		return strconv.FormatUint(raw, 10)
	case kindUint, kindInt:
	}
	v := float64(raw)
	if fd.kind == kindInt && v >= math.Exp2(float64(8*len(b)-1)) {
		v -= math.Exp2(float64(8 * len(b)))
	}
	if fd.scale == 0 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	decimals := int(math.Round(-math.Log10(fd.scale)))
	return strings.Replace(strconv.FormatFloat(v*fd.scale, 'f', decimals, 64), ".", ",", 1)
}
