package config

import "time"

// CurrentVersion — текущая версия схемы файла конфигурации.
// Используется для миграций: при чтении файла сравниваем с этим значением и при
// расхождении применяем правила миграции в Migrate().
const CurrentVersion = 1

// Config — корневая структура единого файла настроек приложения.
//
// Единый источник правды (`data/config.json`):
//   - server (порт),
//   - ui (тема и прочая визуальная настройка),
//   - tle (источники, кеш, обновление),
//   - satnogs (интеграция с базой передатчиков),
//   - exclude_norad_file (путь к файлу со списком исключённых NORAD ID),
//   - station (наблюдатель + радиотракты по ADR-004).
//
// DevMode не сериализуется в JSON — это режим запуска из переменной окружения
// (горячая перезагрузка шаблонов / embed.FS).
type Config struct {
	Version int `json:"_version"`

	Server  ServerConfig  `json:"server"`
	UI      UIConfig      `json:"ui"`
	TLE     TLEConfig     `json:"tle"`
	SatNOGS SatNOGSConfig `json:"satnogs"`

	// ExcludeNoradFile — путь к текстовому файлу со списком исключённых NORAD ID
	// (один ID на строку, "#" — комментарий). Хранится отдельным файлом, потому
	// что записи частые (ПКМ "Скрыть"), и переписывать весь config.json на каждый
	// клик нерационально.
	ExcludeNoradFile string `json:"exclude_norad_file"`

	Station StationConfig `json:"station"`

	// DevMode — режим разработки: шаблоны и статика читаются с диска. В production
	// (Docker) — embed.FS. Не сериализуется, читается только из env DEV_MODE.
	DevMode bool `json:"-"`
}

// ServerConfig — настройки HTTP-сервера.
type ServerConfig struct {
	// Port — порт, на котором слушает HTTP-сервер ("8080").
	Port string `json:"port"`
}

// UIConfig — визуальные настройки пользовательского интерфейса.
type UIConfig struct {
	// Theme — стартовая цветовая тема (default, classic, light, breeze, ...).
	// На клиенте может быть переопределена через cookie ss-theme / localStorage.
	Theme string `json:"theme"`

	// ShowAllTracksOnStart — стартовое состояние master-toggle «глазика»
	// в шапке таблицы плана сеансов. true — при загрузке страницы видны
	// трассы всех КА группы; false (по умолчанию) — только selected/tracking,
	// чтобы избежать визуального шума при больших группах. Юзер может
	// переключить через глазик в течение сессии — это runtime-only состояние,
	// в config обратно не пишется.
	ShowAllTracksOnStart bool `json:"show_all_tracks_on_start"`
}

// TLEConfig — настройки загрузки и кеширования TLE.
//
// Зеркалит tracker.TLEStoreConfig, но живёт в пакете config, чтобы не тащить
// tracker в JSON-схему верхнего уровня. Значения переносятся в TLEStoreConfig
// при создании TLEStore в cmd/server/main.go.
type TLEConfig struct {
	// CacheDir — каталог для файлового кеша TLE.
	CacheDir string `json:"cache_dir"`

	// Groups — группы спутников для загрузки с Celestrak (stations, amateur, ...).
	Groups []string `json:"groups"`

	// UpdateInterval — интервал автообновления TLE.
	UpdateInterval time.Duration `json:"update_interval"`

	// MaxTLEAgeDays — TLE старше этого значения считаются устаревшими.
	MaxTLEAgeDays float64 `json:"max_tle_age_days"`
}

// SatNOGSConfig — настройки интеграции с SatNOGS DB.
type SatNOGSConfig struct {
	// Enabled — включена ли интеграция. При false поля freq_mhz/modulation
	// в SSE-событиях пустые.
	Enabled bool `json:"enabled"`

	// CacheTTL — время жизни записи в кеше передатчиков SatNOGS.
	CacheTTL time.Duration `json:"cache_ttl"`
}

// StationConfig — описание наземной станции по ADR-004 §2.2.
//
// Содержит координаты наблюдателя и список радиотрактов (антенна + приёмник +
// опционально поворотка). При отсутствии оборудования генерируется один
// дефолтный виртуальный тракт SimulatedSDR.
type StationConfig struct {
	// Name — отображаемое имя станции ("Станция Ростов-на-Дону").
	Name string `json:"name"`

	// Type — тип станции: "auto", "observation", "tracking", "hybrid".
	// При "auto" определяется автоматически по составу RadioPaths.
	Type string `json:"type"`

	Observer ObserverConfig `json:"observer"`

	// RadioPaths — список радиотрактов станции. Минимум один тракт.
	RadioPaths []RadioPath `json:"radio_paths"`
}

// ObserverConfig — географические координаты точки наблюдения.
type ObserverConfig struct {
	// Name — отображаемое имя точки ("Ростов-на-Дону") для футера и подписи на карте.
	Name string `json:"name"`

	// Lat — широта в градусах [-90; 90].
	Lat float64 `json:"lat"`

	// Lon — долгота в градусах [-180; 180].
	Lon float64 `json:"lon"`

	// AltM — высота над уровнем моря в метрах [0; 8000].
	AltM float64 `json:"alt_m"`
}

// RadioPath — единица оборудования: антенна + приёмник + (опционально) поворотка.
type RadioPath struct {
	ID       int            `json:"id"`
	Name     string         `json:"name"`
	Antenna  AntennaConfig  `json:"antenna"`
	Receiver ReceiverConfig `json:"receiver"`
	Rotator  *RotatorConfig `json:"rotator,omitempty"`
}

// AntennaConfig — параметры антенны радиотракта.
type AntennaConfig struct {
	// Type — "omnidirectional" или "directional".
	Type string `json:"type"`

	// Model — модель антенны для отображения в UI ("QFH 145 MHz", "Yagi 437 MHz").
	Model string `json:"model"`

	// Band — диапазон ("VHF", "UHF", "L", ...).
	Band string `json:"band"`

	// FreqRangeMHz — рабочий диапазон антенны [min, max] в МГц.
	FreqRangeMHz [2]float64 `json:"freq_range_mhz"`
}

// ReceiverConfig — параметры SDR-приёмника радиотракта.
type ReceiverConfig struct {
	// Driver — драйвер: "rtlsdr", "airspy", "hackrf", "simulated".
	Driver string `json:"driver"`

	// Serial — серийный номер устройства (пусто для simulated и единственного приёмника).
	Serial string `json:"serial"`

	Defaults ReceiverDefaults `json:"defaults"`
}

// ReceiverDefaults — настройки приёмника по умолчанию при первом запуске радиотракта.
type ReceiverDefaults struct {
	CenterFreqHz uint64 `json:"center_freq_hz"`
	GainDB       int    `json:"gain_db"`
	BandwidthHz  uint64 `json:"bandwidth_hz"`
	SampleRateHz uint64 `json:"sample_rate_hz"`
}

// RotatorConfig — параметры поворотной платформы радиотракта.
// Если у тракта нет поворотки — поле RadioPath.Rotator == nil.
type RotatorConfig struct {
	// Driver — "rotctld" (Hamlib) или другой будущий драйвер.
	Driver string `json:"driver"`

	Host string `json:"host"`
	Port int    `json:"port"`

	// AzRange — допустимый диапазон азимута в градусах [min, max].
	AzRange [2]float64 `json:"az_range"`

	// ElRange — допустимый диапазон угла места в градусах [min, max].
	ElRange [2]float64 `json:"el_range"`

	// StepDeg — минимальный шаг поворота в градусах.
	StepDeg float64 `json:"step_deg"`
}
