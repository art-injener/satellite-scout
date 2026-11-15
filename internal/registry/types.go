// Package registry — единый реестр КА и передатчиков с ключом NORAD.
// Каждый источник пишет свой снимок, слияние по приоритетам идёт при чтении,
// поэтому каждое значение помнит источник и время получения.
// Данные живут в памяти и сохраняются в SQLite (data/scout.db).
package registry

import (
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// Источники записей. Имена источников передатчиков совпадают с freqplan.
const (
	SourceCurated      = freqplan.SourceCurated
	SourceHeavensAbove = freqplan.SourceHeavensAbove
	SourceAMSAT        = freqplan.SourceAMSAT
	SourceSonik        = "sonik"
	SourceSatNOGS      = freqplan.SourceSatNOGS
	SourceSpaceTrack   = tracker.TLESourceSpaceTrack
	SourceSatcat       = "satcat"

	// SourceSonikTx загрузка передатчиков sonik в журнале загрузок; записи пишутся под SourceSonik.
	SourceSonikTx = "sonik_tx"
)

// Статусы КА.
const (
	StatusAlive     = "alive"
	StatusDead      = "dead"
	StatusInOrbit   = "in orbit"
	StatusFuture    = "future"
	StatusReEntered = "re-entered"
)

// SatFact сведения о КА от одного источника.
type SatFact struct {
	NoradID   int
	Source    string
	Name      string
	Status    string // статус в терминах источника; пусто, если источник статус не даёт
	CallSign  string
	Operator  string
	Launched  string
	Decay     time.Time // SATCAT: дата схода; нулевая значит КА на орбите
	FetchedAt time.Time
}

// Orbit элементы орбиты КА.
type Orbit struct {
	NoradID   int
	Name      string // строка имени TLE
	Line1     string
	Line2     string
	Epoch     time.Time
	Source    string
	FetchedAt time.Time
}

// Transmitter передатчик КА от одного источника.
type Transmitter struct {
	UUID       string // общий у SatNOGS и sonik; пусто у curated, HA и AMSAT
	NoradID    int
	Name       string
	DownlinkHz int64
	UplinkHz   int64
	Mode       string
	Baud       float64
	Status     string // active, inactive, invalid; пусто, если источник статус не даёт
	Alive      *bool  // признак SatNOGS; nil, если источник его не даёт
	Source     string
	FetchedAt  time.Time
}

// SourceLoad журнал загрузок источника.
type SourceLoad struct {
	Source   string
	LastOK   time.Time
	LastFail time.Time
	Err      string
}

// Satellite сведения о КА после слияния источников.
type Satellite struct {
	NoradID      int
	Name         string
	NameSource   string
	Status       string
	StatusSource string
	Decay        time.Time
	CallSign     string
	Operator     string
	Launched     string
}
