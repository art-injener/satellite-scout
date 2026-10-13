package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readServerFromFile(t *testing.T, path string) ServerConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg Config
	if err = json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cfg.Server
}

func TestEnsureMasterPassword_GeneratesIntoStartupConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "station-pi.json")
	store, err := Bootstrap(path)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	generated, err := EnsureMasterPassword(store)
	if err != nil {
		t.Fatalf("EnsureMasterPassword: %v", err)
	}
	if !generated {
		t.Fatal("empty password must be generated")
	}
	pw := store.Get().Server.MasterPassword
	if len(pw) < 16 {
		t.Errorf("generated password %q too short", pw)
	}
	if got := readServerFromFile(t, path).MasterPassword; got != pw {
		t.Errorf("startup config file has %q, want generated %q", got, pw)
	}
}

func TestEnsureMasterPassword_KeepsConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := NewStore(path)
	cfg := DefaultConfig()
	cfg.Server.MasterPassword = "from-file"
	store.Set(cfg)

	generated, err := EnsureMasterPassword(store)
	if err != nil {
		t.Fatalf("EnsureMasterPassword: %v", err)
	}
	if generated {
		t.Error("configured password must not be regenerated")
	}
	if got := store.Get().Server.MasterPassword; got != "from-file" {
		t.Errorf("password=%q, want from-file", got)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Error("nothing to save, file must stay untouched")
	}
}

func TestEnsureMasterPassword_SaveFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	store := NewStore(path)
	store.Set(DefaultConfig())
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	if _, err := EnsureMasterPassword(store); err == nil {
		t.Fatal("must fail when the startup config cannot be written")
	}
	if got := store.Get().Server.MasterPassword; got != "" {
		t.Errorf("password %q kept in memory although not saved", got)
	}
}
