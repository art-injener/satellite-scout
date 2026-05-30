package config

// Имена runtime-переменных окружения, признанных архитектурой долгосрочно.
// Все остальные ENV считаются устаревшими и обслуживаются только миграцией
// (см. migrate.go и Bootstrap).
const (
	envConfigPath = "SS_CONFIG"
	envDevMode    = "DEV_MODE"
)

// Load — устаревшая точка входа: возвращает Config, собранный исключительно из
// переменных окружения, без чтения файла. Сохраняется для обратной совместимости
// с тестами, которые опираются на env-поведение, и для отдельных тулов, где
// полноценный Bootstrap не нужен. В основном цикле приложения используйте
// Bootstrap().
func Load() *Config {
	cfg := loadFromLegacyEnv()
	cfg.DevMode = getEnvBool(envDevMode, true)
	return cfg
}
