package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

// Имена переменных окружения для пути к файлу конфигурации и режима разработки
const (
	envConfigPath = "SCOUT_CONFIG_PATH"
	envDevMode    = "SCOUT_DEV_MODE"
)

// DefaultConfigPath — путь к файлу конфигурации по умолчанию.
const DefaultConfigPath = "data/config.json"

// ResolveConfigPath возвращает путь к файлу конфигурации из переменной SCOUT_CONFIG_PATH,
// либо путь по умолчанию (data/config.json), если переменная не задана.
func ResolveConfigPath() string {
	if p := os.Getenv(envConfigPath); p != "" {
		return p
	}
	return DefaultConfigPath
}

// Bootstrap загружает конфиг при старте: читает файл по указанному пути,
// а если файла нет — создаёт его с дефолтами. Поле DevMode добавляется из переменной окружения SCOUT_DEV_MODE
func Bootstrap(path string) (*Store, error) {
	if path == "" {
		path = ResolveConfigPath()
	}

	store := NewStore(path)
	err := store.Load()
	switch {
	case err == nil:
		applyRuntimeEnv(store)
		slog.Info("configuration loaded from file", slog.String("path", path))
		return store, nil

	case errors.Is(err, ErrConfigFileNotFound):
		store.Set(DefaultConfig())
		if saveErr := store.Save(); saveErr != nil {
			return nil, fmt.Errorf("save default config: %w", saveErr)
		}
		applyRuntimeEnv(store)
		slog.Info("configuration initialized with defaults (file did not exist)",
			slog.String("path", path))
		return store, nil

	default:
		return nil, fmt.Errorf("bootstrap config: %w", err)
	}
}

// applyRuntimeEnv добавляет в конфиг значения из env, которые не записываются в файл
func applyRuntimeEnv(store *Store) {
	cfg := store.Get()
	if cfg == nil {
		return
	}
	cfg.DevMode = envBool(envDevMode, true) // пока что только DevMode
	store.Set(cfg)
}

// envBool возвращает значение env-переменной, или defaultVal
func envBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}
