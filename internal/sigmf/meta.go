// Package sigmf читает записи Signal Metadata Format (ADR-007 §2.9).
package sigmf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Поддерживаемые core:datatype для воспроизведения в имитаторе.
const (
	DatatypeCF32LE = "cf32_le"
	DatatypeCI16LE = "ci16_le"
)

// Meta — содержимое .sigmf-meta (нужные поля core:*).
type Meta struct {
	Global   Global    `json:"global"`
	Captures []Capture `json:"captures"`
}

// Global — блок global записи SigMF.
type Global struct {
	Datatype    string  `json:"core:datatype"`
	SampleRate  float64 `json:"core:sample_rate"`
	Version     string  `json:"core:version"`
	Description string  `json:"core:description,omitempty"`
}

// Capture — сегмент захвата; для MVP берём первый с sample_start == 0.
type Capture struct {
	SampleStart uint64  `json:"core:sample_start"`
	Frequency   float64 `json:"core:frequency"`
	Datetime    string  `json:"core:datetime"`
}

// LoadMeta читает и проверяет .sigmf-meta.
func LoadMeta(path string) (Meta, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return Meta{}, fmt.Errorf("sigmf: разбор %s: %w", path, err)
	}
	if err := m.validate(); err != nil {
		return Meta{}, err
	}
	return m, nil
}

func (m Meta) validate() error {
	switch m.Global.Datatype {
	case DatatypeCF32LE, DatatypeCI16LE:
	case "":
		return fmt.Errorf("sigmf: не задан core:datatype")
	default:
		return fmt.Errorf("sigmf: datatype %q не поддерживается (нужны %s, %s)",
			m.Global.Datatype, DatatypeCF32LE, DatatypeCI16LE)
	}
	if m.Global.SampleRate <= 0 {
		return fmt.Errorf("sigmf: core:sample_rate должен быть положительным")
	}
	if len(m.Captures) == 0 {
		return fmt.Errorf("sigmf: нет captures")
	}
	if m.Captures[0].Frequency <= 0 {
		return fmt.Errorf("sigmf: core:frequency в captures[0] должна быть положительной")
	}
	return nil
}

// SampleRate возвращает частоту дискретизации, Гц.
func (m Meta) SampleRate() uint32 {
	return uint32(m.Global.SampleRate + 0.5)
}

// CenterHz возвращает частоту центра первого сегмента, Гц.
func (m Meta) CenterHz() uint64 {
	return uint64(m.Captures[0].Frequency + 0.5)
}

// dataPath рядом с meta: foo.sigmf-meta → foo.sigmf-data.
func dataPath(metaPath string) string {
	const suffix = ".sigmf-meta"
	if strings.HasSuffix(metaPath, suffix) {
		return strings.TrimSuffix(metaPath, suffix) + ".sigmf-data"
	}
	// На случай нестандартного имени: отрезать расширение и добавить .sigmf-data
	return strings.TrimSuffix(metaPath, filepath.Ext(metaPath)) + ".sigmf-data"
}
