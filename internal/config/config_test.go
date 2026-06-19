package config

import (
	"path/filepath"
	"testing"
)

// TestDefaultConfig_Values проверяет, что DefaultConfig() возвращает корректный
// набор значений по умолчанию во всех вложенных секциях.
func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Version != CurrentVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, CurrentVersion)
	}
	if cfg.Server.Port != defaultPort {
		t.Errorf("Server.Port = %q, want %q", cfg.Server.Port, defaultPort)
	}
	if cfg.UI.Theme != defaultTheme {
		t.Errorf("UI.Theme = %q, want %q", cfg.UI.Theme, defaultTheme)
	}
	if cfg.Station.Observer.Lat != defaultObserverLat {
		t.Errorf("Observer.Lat = %f, want %f", cfg.Station.Observer.Lat, defaultObserverLat)
	}
	if cfg.Station.Observer.Lon != defaultObserverLon {
		t.Errorf("Observer.Lon = %f, want %f", cfg.Station.Observer.Lon, defaultObserverLon)
	}
	if cfg.Station.Observer.AltM != defaultObserverAlt {
		t.Errorf("Observer.AltM = %f, want %f", cfg.Station.Observer.AltM, defaultObserverAlt)
	}
	if !cfg.SatNOGS.Enabled {
		t.Error("SatNOGS.Enabled = false, want true")
	}
	if cfg.SatNOGS.CacheTTL != Duration(defaultSatNOGSCacheTTL) {
		t.Errorf("SatNOGS.CacheTTL = %v, want %v", cfg.SatNOGS.CacheTTL, Duration(defaultSatNOGSCacheTTL))
	}
	if cfg.SatNOGS.Timeout != Duration(defaultSatNOGSTimeout) {
		t.Errorf("SatNOGS.Timeout = %v, want %v", cfg.SatNOGS.Timeout, Duration(defaultSatNOGSTimeout))
	}
	if cfg.SatNOGS.MaxRetries != defaultSatNOGSMaxRetries {
		t.Errorf("SatNOGS.MaxRetries = %d, want %d", cfg.SatNOGS.MaxRetries, defaultSatNOGSMaxRetries)
	}
	if cfg.SatNOGS.Workers != defaultSatNOGSWorkers {
		t.Errorf("SatNOGS.Workers = %d, want %d", cfg.SatNOGS.Workers, defaultSatNOGSWorkers)
	}
	if cfg.TLE.CacheDir == "" {
		t.Error("TLE.CacheDir is empty")
	}
	if len(cfg.TLE.Groups) == 0 {
		t.Error("TLE.Groups is empty")
	}
	if cfg.TLE.UpdateInterval <= 0 {
		t.Errorf("TLE.UpdateInterval = %v, want > 0", cfg.TLE.UpdateInterval)
	}
}

// TestDefaultConfig_EmptyRadioPaths гарантирует, что при первом запуске
// дефолтная конфигурация — "basic": список радиотрактов пустой. Пользователь
// сам добавляет SDR-оборудование (или имитатор) через UI настроек или правку
// config.json с последующим перезапуском.
func TestDefaultConfig_EmptyRadioPaths(t *testing.T) {
	cfg := DefaultConfig()

	if got := len(cfg.Station.RadioPaths); got != 0 {
		t.Fatalf("RadioPaths len = %d, want 0 (basic station)", got)
	}
	if cfg.Station.StationType() != StationTypeBasic {
		t.Errorf("StationType() = %q, want %q", cfg.Station.StationType(), StationTypeBasic)
	}
}

// TestDefaultConfig_ExcludeNoradPath — путь к файлу исключений по умолчанию
// должен лежать внутри каталога кеша TLE и иметь стандартное имя.
func TestDefaultConfig_ExcludeNoradPath(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ExcludeNoradFile == "" {
		t.Fatal("ExcludeNoradFile is empty")
	}
	if filepath.Base(cfg.ExcludeNoradFile) != defaultExcludeNoradFilename {
		t.Errorf("ExcludeNoradFile basename = %q, want %q",
			filepath.Base(cfg.ExcludeNoradFile), defaultExcludeNoradFilename)
	}
	if filepath.Dir(cfg.ExcludeNoradFile) != cfg.TLE.CacheDir {
		t.Errorf("ExcludeNoradFile dir = %q, want %q",
			filepath.Dir(cfg.ExcludeNoradFile), cfg.TLE.CacheDir)
	}
}

// TestConfig_TLEStoreConfig — преобразование TLE-секции в форму, понятную
// пакету tracker. Гарантирует, что Groups копируются (не shared slice).
func TestConfig_TLEStoreConfig(t *testing.T) {
	cfg := DefaultConfig()
	tle := cfg.TLEStoreConfig()

	if tle.CacheDir != cfg.TLE.CacheDir {
		t.Errorf("CacheDir mismatch")
	}
	if tle.UpdateInterval != cfg.TLE.UpdateInterval.Duration() {
		t.Errorf("UpdateInterval mismatch")
	}
	if tle.MaxTLEAgeDays != cfg.TLE.MaxTLEAgeDays {
		t.Errorf("MaxTLEAgeDays mismatch")
	}
	if len(tle.Groups) != len(cfg.TLE.Groups) {
		t.Fatalf("Groups len mismatch")
	}
	tle.Groups[0] = "modified"
	if cfg.TLE.Groups[0] == "modified" {
		t.Error("TLEStoreConfig() returned shared Groups slice — must be copy")
	}
}

func TestConfig_Addr(t *testing.T) {
	tests := []struct {
		name string
		port string
		want string
	}{
		{"default port", "8080", ":8080"},
		{"custom port", "3000", ":3000"},
		{"empty port", "", ":"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Server: ServerConfig{Port: tt.port}}
			if got := cfg.Addr(); got != tt.want {
				t.Errorf("Addr() = %q, want %q", got, tt.want)
			}
		})
	}
}
