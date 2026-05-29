// Package satnogs предоставляет интеграцию с SatNOGS DB API
// для получения частот, модуляций и других характеристик передатчиков спутников.
package satnogs

import (
	"fmt"
	"sort"
	"strings"
)

// Любительские диапазоны (в Hz) для приоритезации primary-передатчика.
// Наземная станция Satellite Scout ориентирована на любительские КА,
// поэтому FM/AFSK на 2 м или 70 см важнее редких high-band телеметрических каналов.
const (
	amateurVHFLowHz  = 144_000_000 // 144 МГц
	amateurVHFHighHz = 148_000_000 // 148 МГц
	amateurUHFLowHz  = 430_000_000 // 430 МГц
	amateurUHFHighHz = 440_000_000 // 440 МГц
)

// Transmitter — структура передатчика, как её отдаёт SatNOGS DB API
// (`GET /api/transmitters/?satellite__norad_cat_id=…`).
//
// Числовые поля частоты в SatNOGS целые в Hz, baud — float; nullable поля
// пришли как `*int64` / `*float64`, чтобы отличать «нет данных» от нуля.
type Transmitter struct {
	UUID        string `json:"uuid"`
	Description string `json:"description"`
	Alive       bool   `json:"alive"`
	Status      string `json:"status"` // active, inactive, invalid
	Type        string `json:"type"`   // Transmitter, Receiver, Transceiver

	UplinkLow     *int64 `json:"uplink_low"`
	UplinkHigh    *int64 `json:"uplink_high"`
	UplinkDrift   *int64 `json:"uplink_drift"`
	DownlinkLow   *int64 `json:"downlink_low"`
	DownlinkHigh  *int64 `json:"downlink_high"`
	DownlinkDrift *int64 `json:"downlink_drift"`

	Mode       string   `json:"mode"`
	ModeID     *int     `json:"mode_id"`
	UplinkMode string   `json:"uplink_mode"`
	Invert     bool     `json:"invert"`
	Baud       *float64 `json:"baud"`

	SatID            string `json:"sat_id"`
	NoradCatID       int    `json:"norad_cat_id"`
	NoradFollowID    *int   `json:"norad_follow_id"`
	Updated          string `json:"updated"`
	Citation         string `json:"citation"`
	Service          string `json:"service"`
	IARUCoordination string `json:"iaru_coordination"`

	FrequencyViolation bool `json:"frequency_violation"`
	Unconfirmed        bool `json:"unconfirmed"`
}

// IsActive — передатчик «живой» и в активном статусе.
// Используется как первичный фильтр перед выбором primary.
func (t *Transmitter) IsActive() bool {
	return t.Alive && strings.EqualFold(t.Status, "active")
}

// HasDownlink — у передатчика есть downlink-частота
// (нам нужны только nullable downlink_low; high может быть пустым).
func (t *Transmitter) HasDownlink() bool {
	return t.DownlinkLow != nil && *t.DownlinkLow > 0
}

// IsAmateurDownlink — downlink в любительском диапазоне 2 м или 70 см.
// Эти КА — целевые для Satellite Scout, поэтому имеют приоритет в SelectPrimary.
func (t *Transmitter) IsAmateurDownlink() bool {
	if !t.HasDownlink() {
		return false
	}
	hz := *t.DownlinkLow
	if hz >= amateurVHFLowHz && hz <= amateurVHFHighHz {
		return true
	}
	if hz >= amateurUHFLowHz && hz <= amateurUHFHighHz {
		return true
	}
	return false
}

// TransmitterSummary — компактная выжимка передатчика для UI и SSE.
// Содержит готовые форматированные строки, чтобы фронт не собирал их сам.
type TransmitterSummary struct {
	UUID        string  `json:"uuid"`
	Description string  `json:"description"`
	FreqHz      int64   `json:"freq_hz"`        // downlink_low (нижний край downlink)
	FreqMHz     string  `json:"freq_mhz"`       // отформатированная строка, например "145.825"
	Mode        string  `json:"mode"`           // FM, AFSK, BPSK, …
	Modulation  string  `json:"modulation"`     // короткая подпись для таблицы: "FM" или "AFSK 1200"
	Baud        float64 `json:"baud,omitempty"` // скорость, если задана
	Type        string  `json:"type,omitempty"` // Transmitter / Transceiver
}

// summary — внутренний конструктор TransmitterSummary из исходного передатчика.
func summary(t *Transmitter) *TransmitterSummary {
	if t == nil || !t.HasDownlink() {
		return nil
	}
	hz := *t.DownlinkLow
	mod := t.Mode
	if t.Baud != nil && *t.Baud > 0 {
		mod = fmt.Sprintf("%s %s", t.Mode, formatBaud(*t.Baud))
	}
	mod = strings.TrimSpace(mod)
	baud := 0.0
	if t.Baud != nil {
		baud = *t.Baud
	}
	return &TransmitterSummary{
		UUID:        t.UUID,
		Description: t.Description,
		FreqHz:      hz,
		FreqMHz:     formatMHz(hz),
		Mode:        t.Mode,
		Modulation:  mod,
		Baud:        baud,
		Type:        t.Type,
	}
}

// SelectPrimary выбирает «главный» передатчик из списка.
//
// Алгоритм:
//  1. Фильтр: `status==active && alive && есть downlink_low`.
//  2. Сортировка стабильная, по приоритету:
//     - любительский диапазон downlink (VHF/UHF) — раньше;
//     - наличие baud (есть данные о скорости) — раньше (более полная запись);
//     - меньшая downlink_low — раньше (детерминированный tiebreaker).
//  3. Первый из отсортированного списка → TransmitterSummary.
//
// Возвращает nil, если ни один передатчик не подходит под фильтры.
func SelectPrimary(transmitters []Transmitter) *TransmitterSummary {
	candidates := make([]*Transmitter, 0, len(transmitters))
	for i := range transmitters {
		t := &transmitters[i]
		if !t.IsActive() || !t.HasDownlink() {
			continue
		}
		candidates = append(candidates, t)
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		// Любительский downlink — приоритет.
		ai, bi := a.IsAmateurDownlink(), b.IsAmateurDownlink()
		if ai != bi {
			return ai
		}
		// Запись с baud — приоритет (более полная информация).
		ab, bb := a.Baud != nil && *a.Baud > 0, b.Baud != nil && *b.Baud > 0
		if ab != bb {
			return ab
		}
		// Tiebreaker — более низкая частота (детерминизм между запусками).
		return *a.DownlinkLow < *b.DownlinkLow
	})

	return summary(candidates[0])
}

// formatMHz переводит частоту в Hz в строку MHz с точностью до кГц.
// Примеры: 145825000 → "145.825", 437800000 → "437.800", 2400500000 → "2400.500".
func formatMHz(hz int64) string {
	if hz <= 0 {
		return ""
	}
	mhz := float64(hz) / 1_000_000.0
	return fmt.Sprintf("%.3f", mhz)
}

// formatBaud форматирует baudrate без лишних нулей: 1200 → "1200", 9.6 → "9.6".
func formatBaud(b float64) string {
	if b == float64(int64(b)) {
		return fmt.Sprintf("%d", int64(b))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", b), "0"), ".")
}
