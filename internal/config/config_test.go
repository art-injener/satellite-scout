package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_DefaultValues(t *testing.T) {
	// Очищаем переменные окружения
	_ = os.Unsetenv("PORT")
	_ = os.Unsetenv("OBSERVER_LAT")
	_ = os.Unsetenv("OBSERVER_LON")
	_ = os.Unsetenv("OBSERVER_ALT")

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("Expected default port 8080, got %s", cfg.Port)
	}

	if cfg.ObserverLat != 47.315813 {
		t.Errorf("Expected default lat 47.315813, got %f", cfg.ObserverLat)
	}

	if cfg.ObserverLon != 39.788243 {
		t.Errorf("Expected default lon 39.788243, got %f", cfg.ObserverLon)
	}

	if cfg.ObserverAlt != 70.0 {
		t.Errorf("Expected default alt 70.0, got %f", cfg.ObserverAlt)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	// Устанавливаем переменные окружения
	t.Setenv("PORT", "3000")
	t.Setenv("OBSERVER_LAT", "51.5074")
	t.Setenv("OBSERVER_LON", "-0.1278")
	t.Setenv("OBSERVER_ALT", "11.0")

	cfg := Load()

	if cfg.Port != "3000" {
		t.Errorf("Expected port 3000, got %s", cfg.Port)
	}

	if cfg.ObserverLat != 51.5074 {
		t.Errorf("Expected lat 51.5074, got %f", cfg.ObserverLat)
	}

	if cfg.ObserverLon != -0.1278 {
		t.Errorf("Expected lon -0.1278, got %f", cfg.ObserverLon)
	}

	if cfg.ObserverAlt != 11.0 {
		t.Errorf("Expected alt 11.0, got %f", cfg.ObserverAlt)
	}
}

func TestLoad_InvalidFloatValues(t *testing.T) {
	// Устанавливаем невалидные значения
	t.Setenv("OBSERVER_LAT", "invalid")
	t.Setenv("OBSERVER_LON", "not-a-number")

	cfg := Load()

	// Должны использоваться значения по умолчанию
	if cfg.ObserverLat != 47.315813 {
		t.Errorf("Expected default lat 47.315813 for invalid value, got %f", cfg.ObserverLat)
	}

	if cfg.ObserverLon != 39.788243 {
		t.Errorf("Expected default lon 39.788243 for invalid value, got %f", cfg.ObserverLon)
	}
}

func TestConfig_Addr(t *testing.T) {
	tests := []struct {
		name string
		port string
		want string
	}{
		{
			name: "default port",
			port: "8080",
			want: ":8080",
		},
		{
			name: "custom port",
			port: "3000",
			want: ":3000",
		},
		{
			name: "empty port",
			port: "",
			want: ":",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Port: tt.port}
			if got := cfg.Addr(); got != tt.want {
				t.Errorf("Addr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		envValue   string
		defaultVal string
		want       string
	}{
		{
			name:       "existing env var",
			key:        "TEST_KEY",
			envValue:   "test_value",
			defaultVal: "default",
			want:       "test_value",
		},
		{
			name:       "missing env var",
			key:        "MISSING_KEY",
			envValue:   "",
			defaultVal: "default",
			want:       "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			} else {
				_ = os.Unsetenv(tt.key)
			}

			if got := getEnv(tt.key, tt.defaultVal); got != tt.want {
				t.Errorf("getEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad_SatNOGSDefaults(t *testing.T) {
	_ = os.Unsetenv("SATNOGS_ENABLED")
	_ = os.Unsetenv("SATNOGS_CACHE_TTL")

	cfg := Load()
	if !cfg.SatNOGSEnabled {
		t.Error("SatNOGSEnabled = false, want true (default)")
	}
	if cfg.SatNOGSCacheTTL != 24*time.Hour {
		t.Errorf("SatNOGSCacheTTL = %v, want 24h (default)", cfg.SatNOGSCacheTTL)
	}
}

func TestLoad_SatNOGSCustomValues(t *testing.T) {
	t.Setenv("SATNOGS_ENABLED", "false")
	t.Setenv("SATNOGS_CACHE_TTL", "30m")

	cfg := Load()
	if cfg.SatNOGSEnabled {
		t.Error("SatNOGSEnabled = true, want false")
	}
	if cfg.SatNOGSCacheTTL != 30*time.Minute {
		t.Errorf("SatNOGSCacheTTL = %v, want 30m", cfg.SatNOGSCacheTTL)
	}
}

func TestLoad_SatNOGSInvalidDurationFallsBackToDefault(t *testing.T) {
	t.Setenv("SATNOGS_CACHE_TTL", "not-a-duration")
	cfg := Load()
	if cfg.SatNOGSCacheTTL != 24*time.Hour {
		t.Errorf("SatNOGSCacheTTL = %v, want 24h (fallback)", cfg.SatNOGSCacheTTL)
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
		{
			name:       "valid float",
			key:        "TEST_FLOAT",
			envValue:   "123.456",
			defaultVal: 0.0,
			want:       123.456,
		},
		{
			name:       "invalid float",
			key:        "TEST_FLOAT",
			envValue:   "not-a-float",
			defaultVal: 99.9,
			want:       99.9,
		},
		{
			name:       "missing env var",
			key:        "MISSING_FLOAT",
			envValue:   "",
			defaultVal: 42.0,
			want:       42.0,
		},
		{
			name:       "negative float",
			key:        "TEST_FLOAT",
			envValue:   "-12.34",
			defaultVal: 0.0,
			want:       -12.34,
		},
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
