package config

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
	// (Docker) — embed.FS. Не сериализуется, читается только из env SCOUT_DEV_MODE.
	DevMode bool `json:"-"`
}

// ServerConfig — настройки HTTP-сервера.
type ServerConfig struct {
	// Port — порт, на котором слушает HTTP-сервер ("8080").
	Port string `json:"port"`

	// MasterPassword — пароль мастера для удалённого управления приёмником и настройками.
	// Пустой — сервер генерирует при старте и пишет в файл конфига, с которым запущен.
	// Через API не отдаётся и не меняется.
	MasterPassword string `json:"master_password,omitempty"`
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

	// UpdateInterval — интервал автообновления TLE ("6h", "30m").
	UpdateInterval Duration `json:"update_interval"`

	// MaxTLEAgeDays — TLE старше этого значения считаются устаревшими.
	MaxTLEAgeDays float64 `json:"max_tle_age_days"`
}

// SatNOGSConfig — настройки интеграции с SatNOGS DB.
type SatNOGSConfig struct {
	// Enabled — включена ли интеграция. При false поля freq_mhz/modulation
	// в SSE-событиях пустые.
	Enabled bool `json:"enabled"`

	// CacheTTL — время жизни записи в кеше передатчиков SatNOGS ("24h").
	CacheTTL Duration `json:"cache_ttl"`

	// Timeout — таймаут одного HTTP-запроса к SatNOGS DB ("12s").
	// 0 — использовать значение по умолчанию клиента.
	Timeout Duration `json:"timeout"`

	// MaxRetries — число повторов запроса при быстрых ошибках (5xx/429).
	// Таймауты не повторяются на уровне клиента. 0 — значение по умолчанию.
	MaxRetries int `json:"max_retries"`

	// Workers — число параллельных воркеров-загрузчиков передатчиков.
	// 0 — использовать значение по умолчанию.
	Workers int `json:"workers"`
}

// StationConfig — описание наземной станции по ADR-004 §2.2.
//
// Содержит координаты наблюдателя и список радиотрактов (антенна + приёмник +
// опционально поворотка). Тип станции явно не задаётся пользователем — он
// вычисляется методом StationType() по составу RadioPaths. При первом запуске
// без оборудования RadioPaths пустой — это конфигурация "basic" (только
// отслеживание спутников на карте, без работы с радиотрактами).
type StationConfig struct {
	// Name — отображаемое имя станции ("Станция Ростов-на-Дону").
	Name string `json:"name"`

	Observer ObserverConfig `json:"observer"`

	// RadioPaths — список радиотрактов станции. Может быть пустым:
	// конфигурация "basic" — пользователь только смотрит карту и план сеансов,
	// без SDR-приёмников и поворотных платформ.
	RadioPaths []RadioPath `json:"radio_paths"`
}

// Возможные значения, возвращаемые StationType().
const (
	// StationTypeBasic — станция без радиотрактов: только трекер на карте.
	// Mode-bar в UI скрыт, режимы Обзор/Ручной/Имитация недоступны.
	StationTypeBasic = "basic"

	// StationTypeObservation — все тракты без поворотной платформы.
	// Работа в режимах Обзор и Имитация; кнопка "Сопровождать" заблокирована.
	StationTypeObservation = "observation"

	// StationTypeTracking — все тракты с поворотной платформой.
	// Полный набор режимов, включая сопровождение по азимуту/углу места.
	StationTypeTracking = "tracking"

	// StationTypeHybrid — часть трактов с повороткой, часть без.
	// Возможности зависят от выбранного радиотракта.
	StationTypeHybrid = "hybrid"
)

// StationType вычисляет тип станции из состава RadioPaths. В JSON-схеме
// конфигурации этого поля нет: тип — производное от оборудования, и пересчёт
// автоматический при добавлении/удалении трактов через UI.
func (sc *StationConfig) StationType() string {
	if len(sc.RadioPaths) == 0 {
		return StationTypeBasic
	}
	hasRotator := false
	hasNoRotator := false
	for _, rp := range sc.RadioPaths {
		if rp.Rotator != nil {
			hasRotator = true
		} else {
			hasNoRotator = true
		}
	}
	switch {
	case hasRotator && hasNoRotator:
		return StationTypeHybrid
	case hasRotator:
		return StationTypeTracking
	default:
		return StationTypeObservation
	}
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

// Типы антенны радиотракта (операционная классификация для UI и валидации).
const (
	// AntennaTypeStationary — стационарная антенна без поворотной платформы.
	AntennaTypeStationary = "stationary"
	// AntennaTypeRotatable — антенна на поворотной платформе (управление rotator).
	AntennaTypeRotatable = "rotatable"
)

// Конструкция антенны: по ней UI рисует диаграмму направленности антенны без
// поворотки. Неподвижные яги и диполь считаются смотрящими в зенит.
const (
	AntennaKindQFH    = "qfh"
	AntennaKindYagi   = "yagi"
	AntennaKindDipole = "dipole"
	AntennaKindWhip   = "whip"
)

// AntennaKinds — все допустимые значения AntennaConfig.Kind.
var AntennaKinds = []string{AntennaKindQFH, AntennaKindYagi, AntennaKindDipole, AntennaKindWhip}

// AntennaConfig — параметры антенны радиотракта.
// Диапазон частот и название носят справочный характер — в расчётах не участвуют.
type AntennaConfig struct {
	// Name — отображаемое название ("QFH 145", "3 м парабола").
	Name string `json:"name,omitempty"`

	// Type — AntennaTypeStationary или AntennaTypeRotatable.
	Type string `json:"type"`

	// Kind — конструкция (AntennaKinds). Обязательна для тракта без поворотки.
	Kind string `json:"kind,omitempty"`

	// FreqMinMHz — нижняя граница рабочего диапазона, МГц (необязательно).
	FreqMinMHz *float64 `json:"freq_min_mhz,omitempty"`

	// FreqMaxMHz — верхняя граница рабочего диапазона, МГц (необязательно).
	FreqMaxMHz *float64 `json:"freq_max_mhz,omitempty"`
}

// ReceiverConfig — привязка SDR-приёмника к радиотракту.
// Частота и gain задаются в Ручном режиме; здесь только выбор железа.
type ReceiverConfig struct {
	// Driver — "rtlsdr", "airspy", "hackrf", "simulated" или пусто (не выбран).
	Driver string `json:"driver,omitempty"`

	// Serial — серийный номер USB-устройства (стабильный идентификатор).
	Serial string `json:"serial,omitempty"`

	// DevicePath — путь к устройству (/dev/bus/usb/...), может меняться при переподключении.
	DevicePath string `json:"device_path,omitempty"`

	// Label — кэшированное имя модели для отображения в UI.
	Label string `json:"label,omitempty"`

	// SampleRateHz — частота дискретизации тракта, Гц. 0 — рекомендуемая драйвером.
	// Проверяется по возможностям драйвера при открытии устройства, а не здесь:
	// железо при разборе конфига может быть не подключено.
	SampleRateHz uint32 `json:"sample_rate_hz,omitempty"`

	// CalibrationDB — уровень на входе антенны в dBm, который даёт 0 dBFS при усилении 0 дБ.
	// UI показывает dBm = dBFS + CalibrationDB − усиление. Берётся из калибровки генератором
	// известного уровня или ориентировочно для модели приёмника. nil — калибровки нет, UI в dBFS.
	CalibrationDB *float64 `json:"calibration_db,omitempty"`

	// Imitator — эфир имитатора, только для DriverSimulated. nil — эфир по умолчанию:
	// маяк каждого модема около 436,5 МГц поверх шума.
	Imitator *ImitatorConfig `json:"imitator,omitempty"`
}

// Допустимый диапазон ReceiverConfig.CalibrationDB, dBm: с запасом шире любого SDR.
const (
	CalibrationMinDB = -100.0
	CalibrationMaxDB = 30.0
)

// DriverSimulated — драйвер приёмника-имитатора.
const DriverSimulated = "simulated"

// Виды сигналов имитатора.
const (
	ImitatorCarrier  = "carrier"   // немодулированная несущая
	ImitatorAFSKPRBS = "afsk_prbs" // непрерывный AFSK 1200 на PRBS-9, эталон для BER
	ImitatorBeacon   = "beacon"    // кадр AX.25 раз в period_s, модуляция по modem
	ImitatorSSTV     = "sstv"      // картинка SSTV в режиме sstv_mode поверх ЧМ раз в period_s
)

// ImitatorSignalKinds — все допустимые значения ImitatorSignal.Kind.
var ImitatorSignalKinds = []string{ImitatorCarrier, ImitatorAFSKPRBS, ImitatorBeacon, ImitatorSSTV}

// ImitatorSSTVModes — допустимые значения ImitatorSignal.SSTVMode; пусто — robot36.
var ImitatorSSTVModes = []string{"robot36", "pd120", "pd180"}

// Модемы маяка имитатора; названия совпадают с демодуляторами канала.
const (
	ImitatorAFSK1200       = "afsk1200"        // AFSK 1200 поверх FM
	ImitatorFSK9600G3RUH   = "fsk9600_g3ruh"   // GFSK 9600 со скремблером G3RUH
	ImitatorFSK9600GEOSCAN = "fsk9600_geoscan" // GFSK 9600, кадр GEOSCAN (CC1125)
)

// ImitatorBeaconModems — все допустимые значения ImitatorSignal.Modem для маяка.
var ImitatorBeaconModems = []string{ImitatorAFSK1200, ImitatorFSK9600G3RUH, ImitatorFSK9600GEOSCAN}

// ImitatorConfig — эфир имитатора: общий шум приёмника и сигналы.
// Если задан Replay — воспроизводится запись SigMF (.sigmf-meta), сигналы и шум не используются.
type ImitatorConfig struct {
	// NoiseDBFS — мощность шума во всей полосе дискретизации, dBFS. 0 — шум по умолчанию.
	NoiseDBFS float64          `json:"noise_dbfs,omitempty"`
	Signals   []ImitatorSignal `json:"signals,omitempty"`
	// Replay — путь к .sigmf-meta (рядом должен лежать .sigmf-data). Пусто — синтетический эфир.
	Replay string `json:"replay,omitempty"`
}

// ImitatorSignal — сигнал имитатора. Сдвиг от центра приёмника в момент t:
// freq_hz − центр + doppler_hz + doppler_rate_hz_s·t + ход(t).
type ImitatorSignal struct {
	Kind      string  `json:"kind"`
	FreqHz    uint64  `json:"freq_hz"`
	LevelDBFS float64 `json:"level_dbfs"` // 0 — полная шкала

	DopplerHz      float64 `json:"doppler_hz,omitempty"`
	DopplerRateHzS float64 `json:"doppler_rate_hz_s,omitempty"`

	// SweepHz — размах хода туда-обратно от freq_hz за SweepPeriodS; 0 — без хода.
	SweepHz      float64 `json:"sweep_hz,omitempty"`
	SweepPeriodS float64 `json:"sweep_period_s,omitempty"`

	// PassSwingDB — профиль пролёта: level_dbfs в середине, на PassSwingDB ниже у горизонта,
	// пролёт длится PassPeriodS и повторяется; 0 — уровень постоянный.
	PassSwingDB float64 `json:"pass_swing_db,omitempty"`
	PassPeriodS float64 `json:"pass_period_s,omitempty"`

	// PeriodS — интервал между кадрами маяка, с.
	PeriodS float64 `json:"period_s,omitempty"`
	// Modem — модуляция маяка; пусто — afsk1200.
	Modem string `json:"modem,omitempty"`
	// SSTVMode — режим SSTV; пусто — robot36.
	SSTVMode string `json:"sstv_mode,omitempty"`
	// Image — картинка SSTV (PNG или JPEG); пусто — испытательная таблица.
	Image string `json:"image,omitempty"`
	// Audio — запись звука SSTV (WAV) вместо картинки: передаётся в ЧМ как есть,
	// режим определяет декодер по заголовку VIS.
	Audio string `json:"audio,omitempty"`
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
