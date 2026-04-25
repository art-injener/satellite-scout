package config

import (
	"os"
	"strconv"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

const (
	// Координаты Ростова-на-Дону по умолчанию.
	defaultObserverLat = 47.315813
	defaultObserverLon = 39.788243
	defaultObserverAlt = 70.0

	// Имена переменных окружения.
	envPort        = "PORT"
	envObserverLat = "OBSERVER_LAT"
	envObserverLon = "OBSERVER_LON"
	envObserverAlt = "OBSERVER_ALT"
	envDevMode     = "DEV_MODE"
	envTLECacheDir = "TLE_CACHE_DIR"
	envTheme       = "THEME"

	// Тема по умолчанию — Operations Center.
	defaultTheme = "default"
)

// Config содержит конфигурацию приложения.
type Config struct {
	// Настройки сервера
	Port string

	// Режим разработки: шаблоны и статика читаются с диска (горячая перезагрузка).
	// В production (Docker) — используется embed.FS.
	DevMode bool

	// Местоположение наблюдателя (по умолчанию: Ростов-на-Дону)
	ObserverLat float64
	ObserverLon float64
	ObserverAlt float64 // метры над уровнем моря

	// Настройки TLE (загрузка, кеширование, обновление)
	TLE *tracker.TLEStoreConfig

	// Цветовая тема UI: default, classic, light, breeze, breeze-steel, breeze-dark.
	// Стартовый файл static/css/colors-{Theme}.css; в браузере тему можно сменить (localStorage ss-ui-theme).
	Theme string
}

// Load возвращает конфигурацию из переменных окружения с значениями по умолчанию.
func Load() *Config {
	tleCfg := tracker.DefaultTLEStoreConfig()
	if dir := os.Getenv(envTLECacheDir); dir != "" {
		tleCfg.CacheDir = dir
	}

	cfg := &Config{
		Port:        getEnv(envPort, "8080"),
		DevMode:     getEnvBool(envDevMode, true),
		ObserverLat: getEnvFloat(envObserverLat, defaultObserverLat),
		ObserverLon: getEnvFloat(envObserverLon, defaultObserverLon),
		ObserverAlt: getEnvFloat(envObserverAlt, defaultObserverAlt),
		TLE:         tleCfg,
		Theme:       getEnv(envTheme, defaultTheme),
	}
	return cfg
}

// Addr возвращает адрес сервера в формате ":port".
func (c *Config) Addr() string {
	return ":" + c.Port
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
