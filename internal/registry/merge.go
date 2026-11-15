package registry

import (
	"slices"
	"strings"
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
)

// txPriority порядок источников передатчиков: первый побеждает на совпадающем передатчике.
var txPriority = []string{SourceCurated, SourceHeavensAbove, SourceAMSAT, SourceSonik, SourceSatNOGS}

// active сообщает, что передатчик работает и у него есть частота вниз.
// Источник без статуса (curated, HA, AMSAT) публикует только рабочие передатчики.
func (t Transmitter) active() bool {
	if t.DownlinkHz <= 0 {
		return false
	}
	if t.Status == "" {
		return true
	}
	return strings.EqualFold(t.Status, "active") && (t.Alive == nil || *t.Alive)
}

// sameTransmitter: совпадение по uuid, иначе по частоте вниз в пределах допуска канала.
func sameTransmitter(a, b Transmitter) bool {
	if a.UUID != "" && a.UUID == b.UUID {
		return true
	}
	d := a.DownlinkHz - b.DownlinkHz
	return d >= -freqplan.NearHz && d <= freqplan.NearHz
}

// sameRecord сравнивает записи без времени получения.
func (t Transmitter) sameRecord(o Transmitter) bool {
	if (t.Alive == nil) != (o.Alive == nil) || (t.Alive != nil && *t.Alive != *o.Alive) {
		return false
	}
	return t.UUID == o.UUID && t.NoradID == o.NoradID && t.Name == o.Name &&
		t.DownlinkHz == o.DownlinkHz && t.UplinkHz == o.UplinkHz && t.Mode == o.Mode &&
		t.Baud == o.Baud && t.Status == o.Status && t.Source == o.Source
}

// mergeTransmitters сливает рабочие передатчики одного КА по приоритету источников.
// Неработающий передатчик в слиянии не участвует и не перекрывает рабочий у младшего источника.
func mergeTransmitters(bySource map[string][]Transmitter) []Transmitter {
	var out []Transmitter
	for _, src := range txPriority {
		for _, tx := range bySource[src] {
			if !tx.active() || covered(out, tx) {
				continue
			}
			out = append(out, tx)
		}
	}
	return out
}

func covered(have []Transmitter, tx Transmitter) bool {
	for _, h := range have {
		if sameTransmitter(h, tx) {
			return true
		}
	}
	return false
}

// mergeSatellite собирает карточку КА из сведений источников, передатчиков и орбиты.
func mergeSatellite(norad int, facts map[string]SatFact, txs map[string][]Transmitter, orbit *Orbit) Satellite {
	s := Satellite{NoradID: norad}
	sonik, hasSonik := facts[SourceSonik]

	// Имя: curated, sonik, Space-Track (строка имени TLE). У передатчиков SatNOGS имени КА нет.
	switch {
	case len(txs[SourceCurated]) > 0 && txs[SourceCurated][0].Name != "":
		s.Name, s.NameSource = txs[SourceCurated][0].Name, SourceCurated
	case hasSonik && sonik.Name != "":
		s.Name, s.NameSource = sonik.Name, SourceSonik
	case orbit != nil:
		s.Name, s.NameSource = orbit.Name, orbit.Source
	}
	if hasSonik {
		s.CallSign, s.Operator, s.Launched = sonik.CallSign, sonik.Operator, sonik.Launched
	}
	s.Status, s.StatusSource, s.Decay = mergeStatus(facts, txs)
	return s
}

// mergeStatus возвращает статус, его источник и дату схода.
// Порядок: сход с орбиты по SATCAT, без записи SATCAT по sonik; затем SatNOGS, HA и AMSAT, sonik.
// Запись SATCAT без даты схода значит, что КА проверен и летает: re-entered от sonik её не перебивает.
// SatNOGS только признаёт КА мёртвым: флаг alive ставят редакторы базы, к свежим приёмам он не
// привязан и стоит даже у КА 1960-х годов.
func mergeStatus(facts map[string]SatFact, txs map[string][]Transmitter) (string, string, time.Time) {
	satcat, hasSatcat := facts[SourceSatcat]
	sonik, hasSonik := facts[SourceSonik]
	if hasSatcat && !satcat.Decay.IsZero() {
		return StatusReEntered, SourceSatcat, satcat.Decay
	}
	if !hasSatcat && hasSonik && sonik.Status == StatusReEntered {
		return StatusReEntered, SourceSonik, time.Time{}
	}
	if list := txs[SourceSatNOGS]; len(list) > 0 && !slices.ContainsFunc(list, func(tx Transmitter) bool {
		return tx.Alive != nil && *tx.Alive
	}) {
		return StatusDead, SourceSatNOGS, time.Time{}
	}
	for _, src := range []string{SourceHeavensAbove, SourceAMSAT} {
		if len(txs[src]) > 0 {
			return StatusAlive, src, time.Time{}
		}
	}
	if hasSonik && sonik.Status != "" && sonik.Status != StatusReEntered {
		return sonik.Status, SourceSonik, time.Time{}
	}
	if hasSatcat {
		return StatusInOrbit, SourceSatcat, time.Time{}
	}
	return "", "", time.Time{}
}
