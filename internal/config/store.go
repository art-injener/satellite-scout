package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ErrConfigFileNotFound возвращается из Store.Load(), когда файла конфигурации
// нет на диске. Используется уровнем выше (Bootstrap), чтобы запустить миграцию
// со старых ENV или просто записать дефолтный конфиг.
var ErrConfigFileNotFound = errors.New("config file not found")

// ErrUnsupportedVersion — версия в файле выше, чем CurrentVersion. Файл создан
// более новой версией приложения; даунгрейд опасен и явно отклоняется.
var ErrUnsupportedVersion = errors.New("unsupported config version")

var (
	ErrConfigUpdateFnNil    = errors.New("config update: fn is nil")
	ErrConfigStoreEmpty     = errors.New("config update: store is empty (call Load or Set first)")
	ErrConfigSaveStoreEmpty = errors.New("config save: store is empty")
)

// Subscriber вызывается после успешной записи нового конфига на диск.
// Получает копии старой и новой конфигурации — подписчик сам определяет, какие
// поля изменились (например, observer.lat → пересчёт пролётов; ui.theme → SSE
// рассылка темы). Подписчики вызываются последовательно, под отдельной
// горутиной — Store не блокируется на их выполнении.
type Subscriber func(old, updated *Config)

// Store — потокобезопасный держатель конфигурации с атомарной записью на диск.
//
// Контракт:
//   - Load() читает файл; ErrConfigFileNotFound если файла нет.
//   - Get() возвращает глубокую копию текущего конфига (внешние мутации не
//     затрагивают состояние Store).
//   - Update(fn) под write-lock: применяет fn к копии конфига, атомарно пишет на
//     диск, обновляет состояние, нотифицирует подписчиков.
//   - SaveAs(path) пишет текущее состояние в произвольный путь (для миграции).
//
// Атомарность записи реализуется через временный файл в той же директории и
// os.Rename — это гарантирует POSIX-атомарность (на Windows семантика немного
// слабее, но приемлема для нашего сценария).
type Store struct {
	mu          sync.RWMutex
	path        string
	cfg         *Config
	subscribers []Subscriber
}

// NewStore создаёт пустое хранилище, привязанное к пути файла. Конфигурацию
// нужно явно загрузить через Load() или установить через Set().
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Path возвращает путь к файлу конфигурации.
func (s *Store) Path() string {
	return s.path
}

// Load читает конфиг из файла, проверяет версию, при необходимости мигрирует и
// сохраняет в памяти. Возвращает ErrConfigFileNotFound если файла нет.
func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrConfigFileNotFound
		}
		return fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if unmarshalErr := json.Unmarshal(data, &cfg); unmarshalErr != nil {
		return fmt.Errorf("parse config: %w", unmarshalErr)
	}
	if cfg.Version > CurrentVersion {
		return fmt.Errorf("%w: file %d > supported %d", ErrUnsupportedVersion, cfg.Version, CurrentVersion)
	}
	if cfg.Version < CurrentVersion {
		// Миграция версий: на текущем этапе любая версия ниже текущей просто
		// поднимается до CurrentVersion без преобразований. Реальные правила
		// миграции добавляются по мере роста схемы.
		cfg.Version = CurrentVersion
	}

	s.mu.Lock()
	s.cfg = &cfg
	s.mu.Unlock()
	return nil
}

// Set устанавливает конфигурацию в памяти без записи на диск. Используется в
// миграции (Bootstrap), когда мы хотим сначала собрать конфиг, потом записать.
// Подписчики не уведомляются.
func (s *Store) Set(cfg *Config) {
	s.mu.Lock()
	s.cfg = cloneConfig(cfg)
	s.mu.Unlock()
}

// Get возвращает глубокую копию текущей конфигурации. Если конфиг не загружен,
// возвращается nil.
func (s *Store) Get() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg == nil {
		return nil
	}
	return cloneConfig(s.cfg)
}

// Subscribe добавляет подписчика на изменения. Подписки нельзя отменить — это
// допустимо, так как подписчики живут весь жизненный цикл сервера.
func (s *Store) Subscribe(sub Subscriber) {
	if sub == nil {
		return
	}
	s.mu.Lock()
	s.subscribers = append(s.subscribers, sub)
	s.mu.Unlock()
}

// Update атомарно применяет функцию-мутатор к копии конфига, пишет результат на
// диск и нотифицирует подписчиков. Если fn вернёт ошибку, состояние не меняется.
//
// Контракт fn:
//   - получает рабочую копию (мутации в ней допустимы),
//   - может вернуть ошибку валидации — Store не запишет файл и не нотифицирует.
func (s *Store) Update(fn func(*Config) error) error {
	if fn == nil {
		return ErrConfigUpdateFnNil
	}

	s.mu.Lock()
	if s.cfg == nil {
		s.mu.Unlock()
		return ErrConfigStoreEmpty
	}
	old := cloneConfig(s.cfg)
	working := cloneConfig(s.cfg)
	if err := fn(working); err != nil {
		s.mu.Unlock()
		return err
	}
	working.Version = CurrentVersion
	if err := writeConfigAtomic(s.path, working); err != nil {
		s.mu.Unlock()
		return err
	}
	s.cfg = working

	subs := make([]Subscriber, len(s.subscribers))
	copy(subs, s.subscribers)
	s.mu.Unlock()

	for _, sub := range subs {
		sub(old, cloneConfig(working))
	}
	return nil
}

// Save принудительно записывает текущее состояние на диск. Полезно для миграции
// после Set — когда конфиг собран в памяти и нужно зафиксировать его на диске,
// не вызывая подписчиков.
func (s *Store) Save() error {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	if cfg == nil {
		return ErrConfigSaveStoreEmpty
	}
	return writeConfigAtomic(s.path, cfg)
}

// writeConfigAtomic пишет конфиг во временный файл и переименовывает его в
// целевой. Атомарно гарантирует, что на диске либо старая версия (при ошибке
// записи или сбое питания между write и rename), либо полностью новая —
// состояний "наполовину записан JSON" не возникает.
func writeConfigAtomic(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if mkdirErr := os.MkdirAll(dir, 0o750); mkdirErr != nil {
		return fmt.Errorf("mkdir config dir: %w", mkdirErr)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp config: %w", writeErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp config: %w", syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return fmt.Errorf("close temp config: %w", closeErr)
	}
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		cleanup()
		return fmt.Errorf("rename temp config: %w", renameErr)
	}
	return nil
}

// cloneConfig делает глубокую копию структуры конфигурации. Используется в Get,
// Update и Set, чтобы внешний код не мог случайно изменить состояние Store
// через ссылочные поля (slices, maps, pointers).
func cloneConfig(c *Config) *Config {
	if c == nil {
		return nil
	}
	dup := *c

	if c.TLE.Groups != nil {
		dup.TLE.Groups = append([]string(nil), c.TLE.Groups...)
	}
	if c.Station.RadioPaths != nil {
		dup.Station.RadioPaths = make([]RadioPath, len(c.Station.RadioPaths))
		for i, rp := range c.Station.RadioPaths {
			dup.Station.RadioPaths[i] = cloneRadioPath(rp)
		}
	}
	return &dup
}

// cloneRadioPath копирует радиотракт с нюансом по nullable Rotator.
func cloneRadioPath(rp RadioPath) RadioPath {
	out := rp
	if rp.Rotator != nil {
		rot := *rp.Rotator
		out.Rotator = &rot
	}
	return out
}
