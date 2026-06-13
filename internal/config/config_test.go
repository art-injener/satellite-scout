package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestLoad_DefaultsWhenEnvUnset(t *testing.T) {
	clearLegacyEnv()

	cfg := Load()

	if cfg.Server.Port != defaultPort {
		t.Errorf("Server.Port = %q, want %q", cfg.Server.Port, defaultPort)
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
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	t.Setenv("PORT", "3000")
	t.Setenv("OBSERVER_LAT", "51.5074")
	t.Setenv("OBSERVER_LON", "-0.1278")
	t.Setenv("OBSERVER_ALT", "11.0")
	t.Setenv("THEME", "classic")

	cfg := Load()

	if cfg.Server.Port != "3000" {
		t.Errorf("Server.Port = %q, want %q", cfg.Server.Port, "3000")
	}
	if cfg.Station.Observer.Lat != 51.5074 {
		t.Errorf("Observer.Lat = %f, want 51.5074", cfg.Station.Observer.Lat)
	}
	if cfg.Station.Observer.Lon != -0.1278 {
		t.Errorf("Observer.Lon = %f, want -0.1278", cfg.Station.Observer.Lon)
	}
	if cfg.Station.Observer.AltM != 11.0 {
		t.Errorf("Observer.AltM = %f, want 11.0", cfg.Station.Observer.AltM)
	}
	if cfg.UI.Theme != "classic" {
		t.Errorf("UI.Theme = %q, want %q", cfg.UI.Theme, "classic")
	}
}

func TestLoad_InvalidFloatFallsBackToDefault(t *testing.T) {
	t.Setenv("OBSERVER_LAT", "invalid")
	t.Setenv("OBSERVER_LON", "not-a-number")

	cfg := Load()

	if cfg.Station.Observer.Lat != defaultObserverLat {
		t.Errorf("Observer.Lat = %f, want default %f", cfg.Station.Observer.Lat, defaultObserverLat)
	}
	if cfg.Station.Observer.Lon != defaultObserverLon {
		t.Errorf("Observer.Lon = %f, want default %f", cfg.Station.Observer.Lon, defaultObserverLon)
	}
}

func TestLoad_ExcludeNoradFileDefault(t *testing.T) {
	clearLegacyEnv()

	cfg := Load()

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

func TestLoad_ExcludeNoradFileCustom(t *testing.T) {
	t.Setenv("EXCLUDE_NORAD_FILE", "/tmp/custom_exclude.txt")

	cfg := Load()

	if cfg.ExcludeNoradFile != "/tmp/custom_exclude.txt" {
		t.Errorf("ExcludeNoradFile = %q, want %q", cfg.ExcludeNoradFile, "/tmp/custom_exclude.txt")
	}
}

func TestLoad_TLECacheDirEnvShiftsExcludePath(t *testing.T) {
	clearLegacyEnv()
	t.Setenv("TLE_CACHE_DIR", "/var/lib/sat/tle")

	cfg := Load()

	if cfg.TLE.CacheDir != "/var/lib/sat/tle" {
		t.Errorf("TLE.CacheDir = %q, want %q", cfg.TLE.CacheDir, "/var/lib/sat/tle")
	}
	wantExclude := filepath.Join("/var/lib/sat/tle", defaultExcludeNoradFilename)
	if cfg.ExcludeNoradFile != wantExclude {
		t.Errorf("ExcludeNoradFile = %q, want %q", cfg.ExcludeNoradFile, wantExclude)
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

func TestLoad_SatNOGSDefaults(t *testing.T) {
	clearLegacyEnv()

	cfg := Load()
	if !cfg.SatNOGS.Enabled {
		t.Error("SatNOGS.Enabled = false, want true (default)")
	}
	if cfg.SatNOGS.CacheTTL != Duration(24*time.Hour) {
		t.Errorf("SatNOGS.CacheTTL = %v, want 24h", cfg.SatNOGS.CacheTTL.Duration())
	}
}

func TestLoad_SatNOGSCustomValues(t *testing.T) {
	t.Setenv("SATNOGS_ENABLED", "false")
	t.Setenv("SATNOGS_CACHE_TTL", "30m")

	cfg := Load()
	if cfg.SatNOGS.Enabled {
		t.Error("SatNOGS.Enabled = true, want false")
	}
	if cfg.SatNOGS.CacheTTL != Duration(30*time.Minute) {
		t.Errorf("SatNOGS.CacheTTL = %v, want 30m", cfg.SatNOGS.CacheTTL.Duration())
	}
}

func TestLoad_SatNOGSInvalidDurationFallsBackToDefault(t *testing.T) {
	t.Setenv("SATNOGS_CACHE_TTL", "not-a-duration")
	cfg := Load()
	if cfg.SatNOGS.CacheTTL != Duration(24*time.Hour) {
		t.Errorf("SatNOGS.CacheTTL = %v, want 24h (fallback)", cfg.SatNOGS.CacheTTL.Duration())
	}
}

func TestGetEnvDuration(t *testing.T) {
	tests := []struct {
		name       string
		envValue   string
		defaultVal time.Duration
		want       time.Duration
	}{
		{"valid 24h", "24h", time.Hour, 24 * time.Hour},
		{"valid 30m", "30m", time.Hour, 30 * time.Minute},
		{"valid composite", "1h30m", time.Hour, 90 * time.Minute},
		{"empty fallback", "", 5 * time.Minute, 5 * time.Minute},
		{"invalid fallback", "not-a-duration", 5 * time.Minute, 5 * time.Minute},
		{"zero treated as invalid", "0s", time.Hour, time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				_ = os.Unsetenv("TEST_DUR")
			} else {
				t.Setenv("TEST_DUR", tt.envValue)
			}
			if got := getEnvDuration("TEST_DUR", tt.defaultVal); got != tt.want {
				t.Errorf("getEnvDuration(%q) = %v, want %v", tt.envValue, got, tt.want)
			}
		})
	}
}

func TestGetEnvFloat(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		envValue   string
		defaultVal float64
		want       float64
	}{
		{"valid float", "TEST_FLOAT", "123.456", 0.0, 123.456},
		{"invalid float", "TEST_FLOAT", "not-a-float", 99.9, 99.9},
		{"missing env var", "MISSING_FLOAT", "", 42.0, 42.0},
		{"negative float", "TEST_FLOAT", "-12.34", 0.0, -12.34},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			} else {
				_ = os.Unsetenv(tt.key)
			}

			if got := getEnvFloat(tt.key, tt.defaultVal); got != tt.want {
				t.Errorf("getEnvFloat() = %v, want %v", got, tt.want)
			}
		})
	}
}

// clearLegacyEnv очищает все legacy-переменные окружения, чтобы тесты дефолтов
// не зависели от среды разработчика.
func clearLegacyEnv() {
	for _, k := range []string{
		"PORT", "DEV_MODE",
		"OBSERVER_LAT", "OBSERVER_LON", "OBSERVER_ALT",
		"TLE_CACHE_DIR", "THEME",
		"SATNOGS_ENABLED", "SATNOGS_CACHE_TTL",
		"EXCLUDE_NORAD_FILE",
	} {
		_ = os.Unsetenv(k)
	}
}
