package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
)

// DefaultConfigPath — путь к файлу конфигурации по умолчанию.
const DefaultConfigPath = "data/config.json"

// ResolveConfigPath возвращает путь к файлу конфигурации. Источник —
// единственная переменная окружения SS_CONFIG; при её отсутствии используется
// путь по умолчанию (data/config.json внутри рабочей директории).
func ResolveConfigPath() string {
	if p := os.Getenv(envConfigPath); p != "" {
		return p
	}
	return DefaultConfigPath
}

// Bootstrap инициализирует ConfigStore при старте приложения.
//
// Алгоритм:
//  1. Открываем существующий файл config.json (если он есть) — основной путь.
//  2. Если файла нет — собираем конфиг из устаревших переменных окружения
//     (PORT, OBSERVER_*, THEME, …) поверх дефолтов и сохраняем в файл.
//     Это однократная миграция: повторный запуск уже найдёт файл.
//  3. Поле DevMode читается из env DEV_MODE на каждом старте — это режим
//     запуска, а не настройка приложения, и в файл не сохраняется.
//
// Возвращает Store, готовый к использованию (Get / Update / Subscribe).
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
		cfg := loadFromLegacyEnv()
		store.Set(cfg)
		if err := store.Save(); err != nil {
			return nil, fmt.Errorf("save bootstrapped config: %w", err)
		}
		applyRuntimeEnv(store)
		slog.Info("configuration bootstrapped from env (file did not exist)",
			slog.String("path", path))
		return store, nil

	default:
		return nil, fmt.Errorf("bootstrap config: %w", err)
	}
}

// applyRuntimeEnv проставляет в загруженный конфиг runtime-поля, которые не
// сериализуются в файл, — сейчас это только DevMode.
func applyRuntimeEnv(store *Store) {
	devMode := getEnvBool(envDevMode, true)
	cfg := store.Get()
	if cfg == nil {
		return
	}
	cfg.DevMode = devMode
	store.Set(cfg)
}
