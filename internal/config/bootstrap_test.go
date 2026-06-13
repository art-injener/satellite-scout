package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestBootstrap_CreatesFileFromEnvWhenMissing — ключевой кейс миграции:
// если файла на диске нет, Bootstrap собирает конфиг из устаревших ENV и
// записывает его в указанный путь. Повторный запуск уже найдёт файл.
func TestBootstrap_CreatesFileFromEnvWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	clearLegacyEnv()
	t.Setenv("OBSERVER_LAT", "55.75")
	t.Setenv("OBSERVER_LON", "37.62")
	t.Setenv("THEME", "classic")

	store, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("config file not created: %v", statErr)
	}

	got := store.Get()
	if got.Station.Observer.Lat != 55.75 {
		t.Errorf("Lat = %f, want 55.75", got.Station.Observer.Lat)
	}
	if got.UI.Theme != "classic" {
		t.Errorf("Theme = %q, want %q", got.UI.Theme, "classic")
	}

	// Проверяем содержимое файла на диске.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var fromFile Config
	if unmarshalErr := json.Unmarshal(raw, &fromFile); unmarshalErr != nil {
		t.Fatalf("unmarshal: %v", unmarshalErr)
	}
	if fromFile.Version != CurrentVersion {
		t.Errorf("Version on disk = %d, want %d", fromFile.Version, CurrentVersion)
	}
	if fromFile.Station.Observer.Lon != 37.62 {
		t.Errorf("Lon on disk = %f, want 37.62", fromFile.Station.Observer.Lon)
	}
}

// TestBootstrap_UsesExistingFile — повторный запуск: ENV игнорируются, читаем
// только файл. Это и есть «уход» от смешения env+file (пункт пользователя 1).
func TestBootstrap_UsesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := DefaultConfig()
	cfg.Station.Observer.Lat = 1.234
	cfg.UI.Theme = "breeze"
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	clearLegacyEnv()
	t.Setenv("OBSERVER_LAT", "99.0")
	t.Setenv("THEME", "light")

	store, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	got := store.Get()

	if got.Station.Observer.Lat != 1.234 {
		t.Errorf("Lat = %f, want 1.234 (from file, not env)", got.Station.Observer.Lat)
	}
	if got.UI.Theme != "breeze" {
		t.Errorf("Theme = %q, want %q (from file)", got.UI.Theme, "breeze")
	}
}

// TestBootstrap_RuntimeDevModeFromEnv — DevMode не сериализуется в файл и читается
// из env на каждом запуске.
func TestBootstrap_RuntimeDevModeFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	clearLegacyEnv()
	t.Setenv("DEV_MODE", "false")

	store, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	if store.Get().DevMode {
		t.Error("DevMode = true, want false (from env DEV_MODE=false)")
	}

	// На диске поля DevMode нет — секрет Json:"-".
	raw, _ := os.ReadFile(path)
	if string(raw) == "" {
		t.Fatal("file empty after bootstrap")
	}
	if contains(raw, "DevMode") || contains(raw, "dev_mode") {
		t.Errorf("file unexpectedly contains DevMode: %s", string(raw))
	}
}

// TestResolveConfigPath_DefaultAndOverride — путь по умолчанию и переопределение
// через SS_CONFIG.
func TestResolveConfigPath_DefaultAndOverride(t *testing.T) {
	_ = os.Unsetenv("SS_CONFIG")
	if got := ResolveConfigPath(); got != DefaultConfigPath {
		t.Errorf("ResolveConfigPath default = %q, want %q", got, DefaultConfigPath)
	}

	t.Setenv("SS_CONFIG", "/etc/satellite-scout/config.json")
	if got := ResolveConfigPath(); got != "/etc/satellite-scout/config.json" {
		t.Errorf("ResolveConfigPath override = %q", got)
	}
}

func contains(b []byte, s string) bool {
	if len(s) == 0 {
		return true
	}
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}
