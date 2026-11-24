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

// Meta — содержимое .sigmf-meta (нужные поля core:* и расширения scout:*, ADR-007 §2.9).
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
	Author      string  `json:"core:author,omitempty"`
	License     string  `json:"core:license,omitempty"`
	Recorder    string  `json:"core:recorder,omitempty"`

	// Расширения Satellite Scout (namespace scout): привязка записи к КА и контекст пролёта.
	NoradID   int    `json:"scout:norad_id,omitempty"`
	SatName   string `json:"scout:sat_name,omitempty"`
	RadioPath string `json:"scout:radio_path,omitempty"`
	TLELine1  string `json:"scout:tle_line1,omitempty"`
	TLELine2  string `json:"scout:tle_line2,omitempty"`
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

// HasScoutSat — в метаданных задан NORAD КА (расширение scout:).
func (m Meta) HasScoutSat() bool {
	return m.Global.NoradID > 0
}

// SaveMeta пишет .sigmf-meta (для тестов и записи пролёта).
func SaveMeta(path string, m Meta) error {
	if err := m.validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
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
