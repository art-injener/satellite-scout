package freqplan

// Имена источников в CSV и Entry.Source.
const (
	SourceCurated = "curated"
	SourceSatNOGS = "satnogs"
)

// nearHz — допуск «тот же канал»: 5 кГц (типичная сетка ЧМ / уход записи).
const nearHz = 5_000

// Pass — пролёт КА в окне горизонта (достаточно NORAD, имени и AOS/LOS).
type Pass struct {
	NoradID int
	SatName string
	AOS     int64 // Unix мс
	LOS     int64 // Unix мс
}

// Tx — передатчик из одного источника, ещё без привязки к пролёту.
type Tx struct {
	UUID   string
	Name   string
	FreqHz int64
	Mode   string
	Baud   float64
	Source string
}

// Source — каталог передатчиков (curated, SatNOGS, …).
type Source interface {
	Transmitters(noradID int) []Tx
}

// Entry — строка плана частот: передатчик на конкретном пролёте.
type Entry struct {
	NoradID   int
	Name      string
	UUID      string
	FreqHz    int64
	Mode      string
	Baud      float64
	AOS       int64
	LOS       int64
	Source    string
	UpdatedMs int64
}
