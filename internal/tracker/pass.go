package tracker

import (
	"fmt"
	"math"
	"time"
)

// timeFormatHMS — формат времени HH:MM:SS для логов пролётов.
const timeFormatHMS = "15:04:05"

// AzElPoint — точка траектории пролёта в топоцентрических координатах.
// Содержит как сырые Az/El, так и предвычисленные X/Y полярной проекции.
// Фронтенд использует X/Y напрямую для отрисовки SVG мини-проекции.
//
// Полярная проекция:
//
//	r   = 1 - el/(π/2)              — нормализованный радиус (0 = зенит, 1 = горизонт)
//	phi = π/2 - az_rad              — угол в полярных координатах (N = вверх)
//	X   = r * cos(phi)              — горизонтальная координата [-1..1]
//	Y   = -(r * sin(phi))           — вертикальная координата [-1..1] (инвертирована для SVG, где Y растёт вниз)
type AzElPoint struct {
	Az   float64 `json:"az"`   // Азимут, градусы (0..360, от севера по часовой стрелке).
	El   float64 `json:"el"`   // Угол места, градусы (0..90).
	X    float64 `json:"x"`    // Полярная проекция X [-1..1]. E > 0, W < 0.
	Y    float64 `json:"y"`    // Полярная проекция Y [-1..1]. N < 0 (вверх в SVG), S > 0.
	Time int64   `json:"time"` // Unix timestamp, миллисекунды.
}

// Pass — описание одного пролёта спутника над точкой наблюдения.
type Pass struct {
	NoradID     int         `json:"norad_id"`     // NORAD каталожный номер.
	SatName     string      `json:"sat_name"`     // Основное имя спутника (до скобок).
	SatAlias    string      `json:"sat_alias"`    // Альтернативное имя (из скобок в TLE, например "ZARYA" для "ISS (ZARYA)").
	Group       string      `json:"group"`        // TLE-группа спутника (stations, amateur, cubesat и т.д.).
	OrbitNumber int         `json:"orbit_number"` // Номер орбиты (витка) на момент TCA.
	AOS         int64       `json:"aos"`          // Acquisition Of Signal — время появления над горизонтом, Unix ms.
	AOSAz       float64     `json:"aos_az"`       // Азимут в момент AOS, градусы.
	TCA         int64       `json:"tca"`          // Time of Closest Approach — момент максимальной элевации, Unix ms.
	TCAEl       float64     `json:"tca_el"`       // Максимальный угол места, градусы.
	TCAAz       float64     `json:"tca_az"`       // Азимут в момент TCA, градусы.
	LOS         int64       `json:"los"`          // Loss Of Signal — время ухода за горизонт, Unix ms.
	LOSAz       float64     `json:"los_az"`       // Азимут в момент LOS, градусы.
	Duration    float64     `json:"duration"`     // Длительность пролёта, секунды.
	SkyPath     []AzElPoint `json:"sky_path"`     // Траектория пролёта на небесной сфере (для SVG мини-проекции).
}

// ComputeOrbitNumber вычисляет номер орбиты (витка) на заданный момент времени.
// Формула:
//
//	orbitNumber = revNum + floor((meanAnomaly + argPerigee) / 2π)
//	              + (revPerDay + bstar * timeSinceEpoch) * timeSinceEpoch
//
// revNum — номер витка на эпоху TLE.
// meanAnomaly, argPerigee — орбитальные элементы в радианах.
// revPerDay — средняя скорость (оборотов/день).
// bstar — баллистический коэффициент (drag).
// timeSinceEpoch — дни с эпохи TLE до момента наблюдения.
func ComputeOrbitNumber(tle *TLE, t time.Time) int {
	if tle == nil {
		return 0
	}

	const twoPi = 2 * math.Pi

	revNum := float64(tle.RevNumber)
	timeSinceEpoch := t.Sub(tle.Epoch).Hours() / 24.0 // дни
	revPerDay := tle.MeanMotion
	bstar := tle.Bstar
	meanAnomaly := tle.MeanAnomaly * Deg2Rad
	argPerigee := tle.ArgOfPerigee * Deg2Rad

	// Формула: номер витка от эпохи + дробная поправка + витки с учётом drag.
	orbitNum := revNum +
		math.Floor((meanAnomaly+argPerigee)/twoPi) +
		(revPerDay+bstar*timeSinceEpoch)*timeSinceEpoch

	return int(orbitNum)
}

// AOSTime возвращает время AOS как time.Time.
func (p *Pass) AOSTime() time.Time {
	return time.UnixMilli(p.AOS)
}

// TCATime возвращает время TCA как time.Time.
func (p *Pass) TCATime() time.Time {
	return time.UnixMilli(p.TCA)
}

// LOSTime возвращает время LOS как time.Time.
func (p *Pass) LOSTime() time.Time {
	return time.UnixMilli(p.LOS)
}

// DurationSeconds возвращает длительность пролёта в секундах.
func (p *Pass) DurationSeconds() float64 {
	return p.Duration
}

// String возвращает строковое представление пролёта.
func (p *Pass) String() string {
	return fmt.Sprintf("%s #%d (NORAD %d): AOS %s Az=%.0f° → TCA %s El=%.1f° → LOS %s Az=%.0f° [%.0fs]",
		p.SatName, p.OrbitNumber, p.NoradID,
		p.AOSTime().UTC().Format(timeFormatHMS), p.AOSAz,
		p.TCATime().UTC().Format(timeFormatHMS), p.TCAEl,
		p.LOSTime().UTC().Format(timeFormatHMS), p.LOSAz,
		p.Duration,
	)
}
