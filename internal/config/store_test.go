package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStore_LoadReturnsErrorWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))

	err := store.Load()
	if !errors.Is(err, ErrConfigFileNotFound) {
		t.Fatalf("Load() error = %v, want ErrConfigFileNotFound", err)
	}
}

func TestStore_SaveAtomicCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	store := NewStore(path)

	store.Set(DefaultConfig())

	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Version != CurrentVersion {
		t.Errorf("Version = %d, want %d", got.Version, CurrentVersion)
	}
	if got.Station.Observer.Lat != defaultObserverLat {
		t.Errorf("Observer.Lat round-trip = %f, want %f", got.Station.Observer.Lat, defaultObserverLat)
	}
}

func TestStore_LoadAfterSaveRestoresSameValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := DefaultConfig()
	original.Station.Observer.Lat = 12.345
	original.Station.Observer.Lon = -98.765
	original.UI.Theme = "classic"

	store := NewStore(path)
	store.Set(original)
	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	store2 := NewStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := store2.Get()
	if got.Station.Observer.Lat != 12.345 {
		t.Errorf("Lat = %f, want 12.345", got.Station.Observer.Lat)
	}
	if got.Station.Observer.Lon != -98.765 {
		t.Errorf("Lon = %f, want -98.765", got.Station.Observer.Lon)
	}
	if got.UI.Theme != "classic" {
		t.Errorf("Theme = %q, want %q", got.UI.Theme, "classic")
	}
}

func TestStore_GetReturnsDeepCopy(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))
	seed := DefaultConfig()
	// Подсаживаем один радиотракт, чтобы проверить глубокую копию RadioPaths и Rotator.
	seed.Station.RadioPaths = []RadioPath{{
		ID:   1,
		Name: "Test",
		Antenna: AntennaConfig{
			Type: AntennaTypeStationary, Name: "QFH",
		},
		Receiver: ReceiverConfig{Driver: "simulated"},
	}}
	store.Set(seed)

	first := store.Get()
	first.Station.Observer.Lat = 999.0
	first.TLE.Groups[0] = "tampered"
	first.Station.RadioPaths[0].Rotator = &RotatorConfig{Driver: "tampered"}

	second := store.Get()
	if second.Station.Observer.Lat == 999.0 {
		t.Error("Get() returned shared Config — Observer mutation leaked")
	}
	if second.TLE.Groups[0] == "tampered" {
		t.Error("Get() returned shared TLE.Groups slice")
	}
	if second.Station.RadioPaths[0].Rotator != nil {
		t.Error("Get() returned shared RadioPath — Rotator mutation leaked")
	}
}

func TestStore_UpdateAppliesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	store := NewStore(path)
	store.Set(DefaultConfig())
	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	err := store.Update(func(c *Config) error {
		c.Station.Observer.Lat = 11.11
		c.UI.Theme = "breeze"
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got := store.Get()
	if got.Station.Observer.Lat != 11.11 {
		t.Errorf("in-memory Lat = %f, want 11.11", got.Station.Observer.Lat)
	}

	store2 := NewStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if store2.Get().UI.Theme != "breeze" {
		t.Errorf("on-disk theme = %q, want %q", store2.Get().UI.Theme, "breeze")
	}
}

func TestStore_UpdateRollbackOnValidationError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	store := NewStore(path)
	store.Set(DefaultConfig())
	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	wantErr := errors.New("invalid latitude")
	err := store.Update(func(c *Config) error {
		c.Station.Observer.Lat = 200.0
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Update() error = %v, want %v", err, wantErr)
	}

	got := store.Get()
	if got.Station.Observer.Lat != defaultObserverLat {
		t.Errorf("Lat after rollback = %f, want default %f",
			got.Station.Observer.Lat, defaultObserverLat)
	}

	store2 := NewStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if store2.Get().Station.Observer.Lat == 200.0 {
		t.Error("invalid value was persisted to disk despite validation failure")
	}
}

func TestStore_UpdateNotifiesSubscribers(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))
	store.Set(DefaultConfig())
	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var (
		mu       sync.Mutex
		gotOld   *Config
		gotNew   *Config
		callsCnt int
	)
	store.Subscribe(func(old, n *Config) {
		mu.Lock()
		gotOld, gotNew = old, n
		callsCnt++
		mu.Unlock()
	})

	err := store.Update(func(c *Config) error {
		c.UI.Theme = "light"
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if callsCnt != 1 {
		t.Fatalf("subscriber calls = %d, want 1", callsCnt)
	}
	if gotOld == nil || gotOld.UI.Theme != defaultTheme {
		t.Errorf("old theme = %v, want %q", gotOld, defaultTheme)
	}
	if gotNew == nil || gotNew.UI.Theme != "light" {
		t.Errorf("new theme = %v, want %q", gotNew, "light")
	}
}

func TestStore_UpdateOnEmptyStoreFails(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "config.json"))

	err := store.Update(func(c *Config) error { return nil })
	if err == nil {
		t.Fatal("Update on empty store: expected error, got nil")
	}
}

func TestStore_LoadRejectsHigherVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := DefaultConfig()
	cfg.Version = CurrentVersion + 5
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	store := NewStore(path)
	err := store.Load()
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Load() error = %v, want ErrUnsupportedVersion", err)
	}
}

func TestStore_LoadMigratesLowerVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := DefaultConfig()
	cfg.Version = 0
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := store.Get().Version; got != CurrentVersion {
		t.Errorf("Version after migration = %d, want %d", got, CurrentVersion)
	}
}

func TestStore_AtomicityNoPartialOnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	store := NewStore(path)
	store.Set(DefaultConfig())
	if err := store.Save(); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// Снимок успешно записанного файла — должен остаться валидным даже если
	// последующая запись провалится в середине (контракт writeConfigAtomic:
	// рабочий файл атомарно заменяется только финальным rename).
	beforeData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	var beforeCfg Config
	if err := json.Unmarshal(beforeData, &beforeCfg); err != nil {
		t.Fatalf("unmarshal before: %v", err)
	}

	if err := store.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	afterData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	var afterCfg Config
	if err := json.Unmarshal(afterData, &afterCfg); err != nil {
		t.Fatalf("unmarshal after: %v", err)
	}
	if afterCfg.Station.Observer.Lat != beforeCfg.Station.Observer.Lat {
		t.Errorf("file mutated unexpectedly: before %f, after %f",
			beforeCfg.Station.Observer.Lat, afterCfg.Station.Observer.Lat)
	}

	// Не должно остаться tmp-файлов рядом.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("leftover file in config dir: %s", e.Name())
		}
	}
}
