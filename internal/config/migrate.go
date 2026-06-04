package config

import (
	"os"
	"strconv"
	"time"
)

// Имена устаревших переменных окружения, поддерживаемых только для бесшовной
// миграции к единому файлу config.json. Подхватываются ровно один раз — при
// первом запуске на стенде, где файла ещё нет (см. Bootstrap).
const (
	envPort              = "PORT"
	envObserverLat       = "OBSERVER_LAT"
	envObserverLon       = "OBSERVER_LON"
	envObserverAlt       = "OBSERVER_ALT"
	envTLECacheDir       = "TLE_CACHE_DIR"
	envTheme             = "THEME"
	envSatNOGSEnabled    = "SATNOGS_ENABLED"
	envSatNOGSCacheTTL   = "SATNOGS_CACHE_TTL"
	envSatNOGSTimeout    = "SATNOGS_TIMEOUT"
	envSatNOGSMaxRetries = "SATNOGS_MAX_RETRIES"
	envSatNOGSWorkers    = "SATNOGS_WORKERS"
	envExcludeNoradFile  = "EXCLUDE_NORAD_FILE"
)

// loadFromLegacyEnv собирает Config поверх DefaultConfig() с учётом устаревших
// переменных окружения. Используется однократно в Bootstrap при отсутствии
// файла конфигурации; в обычной работе значения берутся из data/config.json.
func loadFromLegacyEnv() *Config {
	cfg := DefaultConfig()

	cfg.Server.Port = getEnv(envPort, cfg.Server.Port)
	cfg.UI.Theme = getEnv(envTheme, cfg.UI.Theme)

	cfg.Station.Observer.Lat = getEnvFloat(envObserverLat, cfg.Station.Observer.Lat)
	cfg.Station.Observer.Lon = getEnvFloat(envObserverLon, cfg.Station.Observer.Lon)
	cfg.Station.Observer.AltM = getEnvFloat(envObserverAlt, cfg.Station.Observer.AltM)

	if dir := os.Getenv(envTLECacheDir); dir != "" {
		cfg.TLE.CacheDir = dir
		cfg.ExcludeNoradFile = joinDefaultExcludePath(dir)
	}

	cfg.SatNOGS.Enabled = getEnvBool(envSatNOGSEnabled, cfg.SatNOGS.Enabled)
	cfg.SatNOGS.CacheTTL = getEnvDuration(envSatNOGSCacheTTL, cfg.SatNOGS.CacheTTL)
	cfg.SatNOGS.Timeout = getEnvDuration(envSatNOGSTimeout, cfg.SatNOGS.Timeout)
	cfg.SatNOGS.MaxRetries = getEnvInt(envSatNOGSMaxRetries, cfg.SatNOGS.MaxRetries)
	cfg.SatNOGS.Workers = getEnvInt(envSatNOGSWorkers, cfg.SatNOGS.Workers)

	cfg.ExcludeNoradFile = getEnv(envExcludeNoradFile, cfg.ExcludeNoradFile)

	return cfg
}

// joinDefaultExcludePath собирает путь "<tle_cache_dir>/exclude_norad.txt".
func joinDefaultExcludePath(tleCacheDir string) string {
	return tleCacheDir + string(os.PathSeparator) + defaultExcludeNoradFilename
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

// getEnvInt читает целое число из переменной окружения.
// Невалидное значение → defaultVal.
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return defaultVal
}

// getEnvDuration читает значение времени из переменной окружения (например
// "24h", "30m", "1h30m"). Невалидное или нулевое значение → defaultVal.
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			return d
		}
	}
	return defaultVal
}
