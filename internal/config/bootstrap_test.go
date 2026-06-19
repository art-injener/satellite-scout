package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestBootstrap_CreatesDefaultFileWhenMissing — при отсутствии файла Bootstrap
// записывает на диск дефолтный конфиг (DefaultConfig). Повторный запуск уже
// найдёт файл.
func TestBootstrap_CreatesDefaultFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	store, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("config file not created: %v", statErr)
	}

	got := store.Get()
	want := DefaultConfig()

	if got.Server.Port != want.Server.Port {
		t.Errorf("Server.Port = %q, want %q", got.Server.Port, want.Server.Port)
	}
	if got.UI.Theme != want.UI.Theme {
		t.Errorf("UI.Theme = %q, want %q", got.UI.Theme, want.UI.Theme)
	}
	if got.Station.Observer.Lat != want.Station.Observer.Lat {
		t.Errorf("Observer.Lat = %f, want %f", got.Station.Observer.Lat, want.Station.Observer.Lat)
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
}

// TestBootstrap_UsesExistingFile — при наличии файла Bootstrap читает только
// его, не подменяя значения дефолтами.
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

	store, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	got := store.Get()

	if got.Station.Observer.Lat != 1.234 {
		t.Errorf("Lat = %f, want 1.234 (from file)", got.Station.Observer.Lat)
	}
	if got.UI.Theme != "breeze" {
		t.Errorf("Theme = %q, want %q (from file)", got.UI.Theme, "breeze")
	}
}

// TestBootstrap_RuntimeDevModeFromEnv — DevMode не сериализуется в файл и
// читается из env SCOUT_DEV_MODE на каждом запуске.
func TestBootstrap_RuntimeDevModeFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	t.Setenv("SCOUT_DEV_MODE", "false")

	store, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	if store.Get().DevMode {
		t.Error("DevMode = true, want false (from env SCOUT_DEV_MODE=false)")
	}

	// На диске поля DevMode нет — json:"-".
	raw, _ := os.ReadFile(path)
	if string(raw) == "" {
		t.Fatal("file empty after bootstrap")
	}
	if contains(raw, "DevMode") || contains(raw, "dev_mode") {
		t.Errorf("file unexpectedly contains DevMode: %s", string(raw))
	}
}

// TestResolveConfigPath_DefaultAndOverride — путь по умолчанию и переопределение
// через SCOUT_CONFIG_PATH.
func TestResolveConfigPath_DefaultAndOverride(t *testing.T) {
	_ = os.Unsetenv("SCOUT_CONFIG_PATH")
	if got := ResolveConfigPath(); got != DefaultConfigPath {
		t.Errorf("ResolveConfigPath default = %q, want %q", got, DefaultConfigPath)
	}

	t.Setenv("SCOUT_CONFIG_PATH", "/etc/satellite-scout/config.json")
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
