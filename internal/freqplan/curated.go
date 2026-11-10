package freqplan

import (
	"encoding/json"
	"fmt"
	"os"
)

// curatedFile — JSON со списком целей приёма (локальный приоритетный источник).
type curatedFile struct {
	Transmitters []curatedTx `json:"transmitters"`
}

type curatedTx struct {
	NoradID int     `json:"norad_id"`
	Name    string  `json:"name"`
	FreqHz  int64   `json:"freq_hz"`
	Mode    string  `json:"mode"`
	Baud    float64 `json:"baud"`
	Kind    string  `json:"kind"`
	Note    string  `json:"note"`
}

// Curated — Source из локального JSON.
type Curated struct {
	byNorad map[int][]Tx
}

// Transmitters — реализация Source.
func (c *Curated) Transmitters(noradID int) []Tx {
	if c == nil {
		return nil
	}
	return c.byNorad[noradID]
}

// LoadCurated читает curated JSON. Нет файла — пустой источник без ошибки.
func LoadCurated(path string) (*Curated, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Curated{byNorad: map[int][]Tx{}}, nil
		}
		return nil, fmt.Errorf("freqplan: curated: %w", err)
	}
	var file curatedFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("freqplan: curated json: %w", err)
	}
	out := &Curated{byNorad: make(map[int][]Tx, len(file.Transmitters))}
	for _, row := range file.Transmitters {
		if row.NoradID <= 0 || row.FreqHz <= 0 {
			continue
		}
		out.byNorad[row.NoradID] = append(out.byNorad[row.NoradID], Tx{
			Name: row.Name, FreqHz: row.FreqHz, Mode: row.Mode,
			Baud: row.Baud, Source: SourceCurated,
		})
	}
	return out, nil
}
