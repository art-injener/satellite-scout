package ax100

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Порты CSP назначения статусных маяков SUCHAI-2/3 / PlantSat (suchai2.ksy).
const (
	SuchaiPort2      = 16
	SuchaiPort3      = 17
	SuchaiPortPlant  = 18
	suchaiComLen     = 8  // nframe + type + node + ndata
	suchaiStatusLen  = 100 // 25 × u4
	suchaiMinBody    = suchaiComLen + suchaiStatusLen
)

// SuchaiParam — поле статусного маяка для таблицы ТМИ.
type SuchaiParam struct {
	ID    string
	Name  string
	Value string
	Unit  string
	Key   bool
}

var suchaiSatName = map[uint8]string{
	SuchaiPort2:     "SUCHAI-2",
	SuchaiPort3:     "SUCHAI-3",
	SuchaiPortPlant: "PlantSat",
}

type suchaiField struct {
	id   string
	name string
	unit string
	key  bool
	kind suchaiKind
}

type suchaiKind uint8

const (
	suchaiU32 suchaiKind = iota
	suchaiUnix
	suchaiMV // мВ → В
)

// Раскладка status после com_frame; big-endian, как suchai2.ksy.
var suchaiStatusLayout = []suchaiField{
	{id: "index", name: "Индекс"},
	{id: "timestamp", name: "Метка времени", kind: suchaiUnix, unit: "UTC", key: true},
	{id: "obc_opmode", name: "Режим OBC"},
	{id: "rtc_date_time", name: "Время RTC", kind: suchaiUnix, unit: "UTC"},
	{id: "obc_last_reset", name: "Посл. сброс"},
	{id: "obc_hrs_alive", name: "Часов в работе", unit: "ч", key: true},
	{id: "obc_hrs_wo_reset", name: "Часов без сброса", unit: "ч"},
	{id: "obc_reset_counter", name: "Сбросы OBC", key: true},
	{id: "obc_executed_cmds", name: "Команд выполнено"},
	{id: "obc_failed_cmds", name: "Команд с ошибкой"},
	{id: "com_count_tm", name: "ТМ пакетов"},
	{id: "com_count_tc", name: "ТС пакетов"},
	{id: "com_last_tc", name: "Посл. ТС"},
	{id: "fpl_last", name: "План: последний"},
	{id: "fpl_queue", name: "План: очередь"},
	{id: "ads_tle_epoch", name: "Эпоха TLE"},
	{id: "eps_vbatt", name: "Напряжение АКБ", kind: suchaiMV, unit: "В", key: true},
	{id: "eps_cur_sun", name: "Ток СБ", unit: "мА", key: true},
	{id: "eps_cur_sys", name: "Ток системы", unit: "мА"},
	{id: "obc_temp_1", name: "Температура OBC", unit: "°C", key: true},
	{id: "eps_temp_bat0", name: "Температура АКБ", unit: "°C", key: true},
	{id: "drp_mach_action", name: "DRP действие"},
	{id: "drp_mach_state", name: "DRP состояние"},
	{id: "drp_mach_payloads", name: "DRP нагрузки"},
	{id: "drp_mach_step", name: "DRP шаг"},
}

// SuchaiBeaconParams подтверждает статусный маяк SUCHAI по порту CSP и длине.
// sat — имя КА; ok=false — другой порт или короткая нагрузка.
func SuchaiBeaconParams(raw []byte) (params []SuchaiParam, sat string, ok bool) {
	h, body, ok := ParseCSP(raw)
	if !ok {
		return nil, "", false
	}
	sat, ok = suchaiSatName[h.DstPort]
	if !ok || len(body) < suchaiMinBody {
		return nil, "", false
	}
	// com_frame: nframe u2, type u1, node u1, ndata u4 (BE).
	status := body[suchaiComLen : suchaiComLen+suchaiStatusLen]
	out := make([]SuchaiParam, 0, len(suchaiStatusLayout)+2)
	out = append(out,
		SuchaiParam{ID: "sat", Name: "КА", Value: sat, Key: true},
		SuchaiParam{ID: "tm_type", Name: "Тип ТМ", Value: strconv.FormatUint(uint64(body[2]), 10)},
		SuchaiParam{ID: "nframe", Name: "Кадр", Value: strconv.FormatUint(uint64(binary.BigEndian.Uint16(body[0:2])), 10)},
	)
	for i, fd := range suchaiStatusLayout {
		off := i * 4
		raw32 := binary.BigEndian.Uint32(status[off : off+4])
		out = append(out, SuchaiParam{
			ID:    fd.id,
			Name:  fd.name,
			Value: formatSuchai(fd, raw32),
			Unit:  fd.unit,
			Key:   fd.key,
		})
	}
	return out, sat, true
}

func formatSuchai(fd suchaiField, v uint32) string {
	switch fd.kind {
	case suchaiUnix:
		if v == 0 {
			return "—"
		}
		return time.Unix(int64(v), 0).UTC().Format("2006-01-02 15:04:05")
	case suchaiMV:
		s := strconv.FormatFloat(float64(v)/1000, 'f', 2, 64)
		return strings.Replace(s, ".", ",", 1)
	default:
		return strconv.FormatUint(uint64(v), 10)
	}
}

// PackSuchaiBeacon собирает CSP+com+status для тестов (big-endian status).
func PackSuchaiBeacon(dstPort uint8, status map[string]uint32) []byte {
	body := make([]byte, suchaiMinBody)
	body[2] = 1 // type
	body[3] = 1 // node
	binary.BigEndian.PutUint32(body[4:8], 1)
	for i, fd := range suchaiStatusLayout {
		if v, ok := status[fd.id]; ok {
			binary.BigEndian.PutUint32(body[suchaiComLen+i*4:], v)
		}
	}
	return PackCSP(CSPHeader{
		Priority: 2, Source: 1, Destination: 10,
		SrcPort: 47, DstPort: dstPort, CRC: true,
	}, body)
}

// FormatSuchaiSat — имя по порту; пусто, если неизвестен.
func FormatSuchaiSat(dstPort uint8) string {
	if s, ok := suchaiSatName[dstPort]; ok {
		return s
	}
	return fmt.Sprintf("CSP dst_port=%d", dstPort)
}
