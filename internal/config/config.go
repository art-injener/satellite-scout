package config

import (
	"os"
	"strconv"
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
)

// Config содержит конфигурацию приложения.
type Config struct {
	// Настройки сервера
	Port string

	// Местоположение наблюдателя (по умолчанию: Ростов-на-Дону)
	ObserverLat float64
	ObserverLon float64
	ObserverAlt float64 // метры над уровнем моря
}

// Load возвращает конфигурацию из переменных окружения с значениями по умолчанию.
func Load() *Config {
	cfg := &Config{
		Port:        getEnv(envPort, "8080"),
		ObserverLat: getEnvFloat(envObserverLat, defaultObserverLat),
		ObserverLon: getEnvFloat(envObserverLon, defaultObserverLon),
		ObserverAlt: getEnvFloat(envObserverAlt, defaultObserverAlt),
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

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}
