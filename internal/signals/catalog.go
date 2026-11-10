// Package signals хранит каталог видов передачи и сопоставляет его с уже
// измеренной модуляцией, частотой и типом сигнала. Проба и разбор содержимого
// здесь не делаются.
package signals

import (
	_ "embed"
	"encoding/json"
	"math"
	"os"
	"sync"
)

const (
	// IDBroadcast — вещательная ЧМ.
	IDBroadcast = "broadcast"
	// IDAmateur — любительская ЧМ в диапазоне 2 м или 70 см.
	IDAmateur = "amateur_voice"
	// IDAmateurData — узкая цифра в любительском диапазоне, чаще 2FSK.
	IDAmateurData = "amateur_data"
	// IDISM — датчик или брелок в ЛПД 433.
	IDISM = "ism_burst"
	// IDLoRa — LoRa по полосе ЛЧМ.
	IDLoRa = "lora"
	// IDGSM — базовая станция GSM вниз.
	IDGSM = "gsm_base"

	// ProbeNone — виду передачи достаточно совпадения сигнатуры.
	ProbeNone = "none"
	// ProbeRequired — вид ставится только после пробы.
	ProbeRequired = "required"

	overridePath = "data/signatures.json"
)

//go:embed signatures.json
var embedded []byte

// Band — включительный участок частот, Гц.
type Band struct {
	LoHz float64 `json:"lo_hz"`
	HiHz float64 `json:"hi_hz"`
}

// Signature — условия одного вида передачи.
type Signature struct {
	ID          string    `json:"id"`
	Label       string    `json:"label"`
	Modulations []string  `json:"modulations"`
	Bands       []Band    `json:"bands,omitempty"`
	WidthMinHz  float64   `json:"width_min_hz,omitempty"`
	WidthMaxHz  float64   `json:"width_max_hz,omitempty"`
	WidthHz     []float64 `json:"width_hz,omitempty"`
	WidthTolHz  float64   `json:"width_tol_hz,omitempty"`
	BaudMin     float64   `json:"baud_min,omitempty"`
	BaudMax     float64   `json:"baud_max,omitempty"`
	Emission    string    `json:"emission,omitempty"`
	BurstsMin   int       `json:"bursts_min,omitempty"`
	BurstMinS   float64   `json:"burst_min_s,omitempty"`
	BurstMaxS   float64   `json:"burst_max_s,omitempty"`
	Probe       string    `json:"probe"`
}

// Input — что уже известно об участке. Service непустой: сопоставление не нужно.
type Input struct {
	Modulation string
	Service    string
	OccupiedHz float64
	Baud       float64
	CenterHz   float64
	Emission   string
	OnS        float64
	Bursts     int
}

// Hit — одна подошедшая сигнатура.
type Hit struct {
	ID    string
	Label string
	Probe string
}

var (
	loadOnce sync.Once
	loaded   []Signature
)

// Parse читает каталог из JSON.
func Parse(data []byte) ([]Signature, error) {
	var cat []Signature
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, err
	}
	return cat, nil
}

// Merge накладывает over на base: та же id заменяет строку, новая id дописывается.
func Merge(base, over []Signature) []Signature {
	out := make([]Signature, len(base))
	copy(out, base)
	index := make(map[string]int, len(out))
	for i, s := range out {
		index[s.ID] = i
	}
	for _, s := range over {
		if i, ok := index[s.ID]; ok {
			out[i] = s
			continue
		}
		index[s.ID] = len(out)
		out = append(out, s)
	}
	return out
}

// Default — встроенный каталог и, если файл есть и читается, правки data/signatures.json.
func Default() []Signature {
	loadOnce.Do(func() {
		base, err := Parse(embedded)
		if err != nil {
			panic("signals: embedded catalog: " + err.Error())
		}
		if raw, err := os.ReadFile(overridePath); err == nil {
			if over, err := Parse(raw); err == nil {
				base = Merge(base, over)
			}
		}
		loaded = base
	})
	return loaded
}

// Match возвращает все сигнатуры каталога, которым подходит участок.
// Пустой список и несколько ответов для вызывающего значат одно: вид сам не выбран.
func Match(cat []Signature, in Input) []Hit {
	if in.Service != "" {
		return nil
	}
	var hits []Hit
	for _, s := range cat {
		if !s.matches(in) {
			continue
		}
		probe := s.Probe
		if probe == "" {
			probe = ProbeNone
		}
		hits = append(hits, Hit{ID: s.ID, Label: s.Label, Probe: probe})
	}
	return hits
}

func (s Signature) matches(in Input) bool {
	if len(s.Modulations) == 0 || !contains(s.Modulations, in.Modulation) {
		return false
	}
	if len(s.Bands) > 0 && !inBands(s.Bands, in.CenterHz) {
		return false
	}
	if s.Emission != "" && in.Emission != s.Emission {
		return false
	}
	if s.WidthMinHz > 0 || s.WidthMaxHz > 0 {
		if in.OccupiedHz < s.WidthMinHz || (s.WidthMaxHz > 0 && in.OccupiedHz > s.WidthMaxHz) {
			return false
		}
	}
	if len(s.WidthHz) > 0 {
		tol := s.WidthTolHz
		if tol <= 0 {
			tol = 40_000
		}
		near := false
		for _, w := range s.WidthHz {
			if math.Abs(in.OccupiedHz-w) <= tol {
				near = true
				break
			}
		}
		if !near {
			return false
		}
	}
	if s.BaudMin > 0 || s.BaudMax > 0 {
		if in.Baud < s.BaudMin || (s.BaudMax > 0 && in.Baud > s.BaudMax) {
			return false
		}
	}
	if s.BurstsMin > 0 && in.Bursts < s.BurstsMin {
		return false
	}
	if s.BurstMaxS > 0 {
		if in.Bursts < 1 {
			return false
		}
		dur := in.OnS / float64(in.Bursts)
		if dur < s.BurstMinS || dur > s.BurstMaxS {
			return false
		}
	}
	return true
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func inBands(bands []Band, hz float64) bool {
	for _, b := range bands {
		if hz >= b.LoHz && hz <= b.HiHz {
			return true
		}
	}
	return false
}
