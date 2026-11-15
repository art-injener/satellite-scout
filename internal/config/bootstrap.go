package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Имена переменных окружения для пути к файлу конфигурации и режима разработки
const (
	envConfigPath = "SCOUT_CONFIG_PATH"
	envDevMode    = "SCOUT_DEV_MODE"

	// Учётка Space-Track: из окружения или .env, в config.json не пишется
	envSpaceTrackIdentity = "SPACETRACK_IDENTITY"
	envSpaceTrackPassword = "SPACETRACK_PASSWORD"
)

// dotEnvPath — файл с секретами в рабочем каталоге сервера (лежит в .gitignore).
const dotEnvPath = ".env"

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
		// Те же правила, что при сохранении из настроек: с невалидным файлом не стартуем
		if verr := store.Get().Validate(); verr != nil {
			return nil, fmt.Errorf("invalid config %s: %w", path, verr)
		}
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
	cfg.DevMode = envBool(envDevMode, true)
	cfg.SpaceTrack = loadSpaceTrackCredentials(dotEnvPath)
	store.Set(cfg)
}

// loadSpaceTrackCredentials берёт учётку из окружения, недостающее — из файла envFile.
// Неполная пара даёт пустую учётку: источник Space-Track тогда пропускается.
func loadSpaceTrackCredentials(envFile string) SpaceTrackCredentials {
	identity := os.Getenv(envSpaceTrackIdentity)
	password := os.Getenv(envSpaceTrackPassword)
	if identity == "" || password == "" {
		fromFile := readDotEnv(envFile, envSpaceTrackIdentity, envSpaceTrackPassword)
		if identity == "" {
			identity = fromFile[envSpaceTrackIdentity]
		}
		if password == "" {
			password = fromFile[envSpaceTrackPassword]
		}
	}
	creds := SpaceTrackCredentials{Identity: identity, Password: password}
	if !creds.Enabled() {
		return SpaceTrackCredentials{}
	}
	return creds
}

// readDotEnv читает из файла строки KEY=VALUE, только перечисленные ключи.
// Остальное содержимое файла не разбирается и никуда не попадает.
func readDotEnv(path string, keys ...string) map[string]string {
	values := make(map[string]string, len(keys))
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("env file is not readable", slog.String("path", path), slog.Any("error", err))
		}
		return values
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		key = strings.TrimSpace(key)
		if !ok || !slices.Contains(keys, key) {
			continue
		}
		values[key] = unquoteEnvValue(strings.TrimSpace(value))
	}
	return values
}

// unquoteEnvValue снимает парные одинарные или двойные кавычки вокруг значения.
func unquoteEnvValue(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
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
