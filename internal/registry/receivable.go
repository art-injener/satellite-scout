package registry

import (
	"strconv"
	"strings"
)

// lowOrbitMinMeanMotion — граница SGP4 и SDP4: период обращения 225 минут.
// Выше неё геостационарные, высокоэллиптические и навигационные КА, которых станция
// с приёмником RTL-SDR и всенаправленной антенной не слушает.
const lowOrbitMinMeanMotion = 1440.0 / 225

// livenessSources источники, которые подтверждают, что КА жив.
var livenessSources = []string{SourceHeavensAbove, SourceAMSAT}

// Band диапазон перестройки приёмника, Гц.
type Band struct {
	MinHz int64
	MaxHz int64
}

// Bands диапазоны приёмников станции. Пустой набор ничего не отсекает: у станции нет радиотрактов.
type Bands []Band

// Allow сообщает, что частоту может настроить хотя бы один приёмник.
func (bs Bands) Allow(hz int64) bool {
	if len(bs) == 0 {
		return true
	}
	for _, b := range bs {
		if hz >= b.MinHz && hz <= b.MaxHz {
			return true
		}
	}
	return false
}

// KnowsLiveness сообщает, что в реестре есть передатчики хотя бы одного источника живости.
// До этого отбор по «жив» не включается, иначе группа была бы пустой.
func (r *Registry) KnowsLiveness() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.knowsLivenessLocked()
}

// lowOrbit проверяет среднее движение (колонки 53–63 второй строки TLE).
// Строку, которую не удалось разобрать, не отсекает: такие элементы отбросит разбор TLE в расчёте пролётов.
func lowOrbit(line2 string) bool {
	const from, to = 52, 63
	if len(line2) < to {
		return true
	}
	mm, err := strconv.ParseFloat(strings.TrimSpace(line2[from:to]), 64)
	if err != nil {
		return true
	}
	return mm > lowOrbitMinMeanMotion
}
