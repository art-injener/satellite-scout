package tracker

import (
	"fmt"
	"strings"
	"time"
)

// Константы по умолчанию для конфигурации TLEStore.
const (
	// DefaultTLEUpdateInterval интервал автообновления TLE (6 часов).
	DefaultTLEUpdateInterval = 6 * time.Hour

	// DefaultTLECacheDir директория для файлового кеша TLE.
	DefaultTLECacheDir = "data/tle_cache"

	// DefaultMaxTLEAgeDays максимальный возраст TLE в днях до предупреждения.
	DefaultMaxTLEAgeDays = 7.0
)

// DefaultTLEGroups группы спутников по умолчанию для загрузки.
var DefaultTLEGroups = []string{"stations", "amateur", "cubesat"}

// TLEStoreConfig содержит настройки TLEStore.
type TLEStoreConfig struct {
	// Groups список групп спутников для загрузки с Celestrak.
	// Примеры: "stations", "amateur", "cubesat", "weather", "starlink".
	Groups []string `yaml:"groups"`

	// UpdateInterval интервал автоматического обновления TLE.
	// По умолчанию: 6 часов.
	UpdateInterval time.Duration `yaml:"update_interval"`

	// CacheDir директория для файлового кеша TLE.
	// По умолчанию: "data/tle_cache".
	CacheDir string `yaml:"cache_dir"`

	// MaxTLEAgeDays максимальный возраст TLE в днях.
	// TLE старше этого значения считаются устаревшими.
	// По умолчанию: 7 дней.
	MaxTLEAgeDays float64 `yaml:"max_tle_age_days"`

	// EnableMetadata включает загрузку метаданных спутников из SatNOGS.
	// По умолчанию: false.
	EnableMetadata bool `yaml:"enable_metadata"`
}

// DefaultTLEStoreConfig возвращает конфигурацию TLEStore со значениями по умолчанию.
func DefaultTLEStoreConfig() *TLEStoreConfig {
	return &TLEStoreConfig{
		Groups:         DefaultTLEGroups,
		UpdateInterval: DefaultTLEUpdateInterval,
		CacheDir:       DefaultTLECacheDir,
		MaxTLEAgeDays:  DefaultMaxTLEAgeDays,
		EnableMetadata: false,
	}
}

// Validate проверяет и корректирует конфигурацию TLEStore.
// Возвращает ошибку, если указаны невалидные группы спутников.
func (c *TLEStoreConfig) Validate() error {
	if c.UpdateInterval < time.Minute {
		c.UpdateInterval = DefaultTLEUpdateInterval
	}
	if c.CacheDir == "" {
		c.CacheDir = DefaultTLECacheDir
	}
	if c.MaxTLEAgeDays <= 0 {
		c.MaxTLEAgeDays = DefaultMaxTLEAgeDays
	}
	if len(c.Groups) == 0 {
		c.Groups = DefaultTLEGroups
	}

	// Проверяем, что все указанные группы допустимы
	var invalid []string
	for _, g := range c.Groups {
		if !IsValidGroup(g) {
			invalid = append(invalid, g)
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("unknown TLE groups: %s (available: %s)",
			strings.Join(invalid, ", "),
			strings.Join(AvailableGroupNames(), ", "),
		)
	}

	return nil
}
