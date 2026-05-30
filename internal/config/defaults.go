package config

import (
	"path/filepath"
	"time"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// Дефолтные значения единого конфига.
const (
	defaultPort         = "8080"
	defaultTheme        = "default"
	defaultStationName  = "Станция Ростов-на-Дону"
	defaultObserverName = "Ростов-на-Дону"

	// Координаты Ростова-на-Дону по умолчанию (точка станции).
	defaultObserverLat float64 = 47.315813
	defaultObserverLon float64 = 39.788243
	defaultObserverAlt float64 = 70.0

	// Имя файла исключений по умолчанию (внутри каталога кеша TLE).
	defaultExcludeNoradFilename = "exclude_norad.txt"

	defaultSatNOGSEnabled                 = true
	defaultSatNOGSCacheTTL                = 24 * time.Hour
	defaultRadioPathID                    = 1
	defaultRadioPathName                  = "Имитатор SDR"
	defaultRadioPathAntennaType           = "omnidirectional"
	defaultRadioPathAntennaModel          = "QFH 145 MHz"
	defaultRadioPathAntennaBand           = "VHF"
	defaultRadioPathReceiverDriver        = "simulated"
	defaultReceiverCenterFreqHz    uint64 = 145_900_000
	defaultReceiverGainDB                 = 42
	defaultReceiverBandwidthHz     uint64 = 2_400_000
	defaultReceiverSampleRateHz    uint64 = 2_400_000
)

// defaultRadioPathFreqRange — рабочий диапазон антенны дефолтного радиотракта (МГц).
var defaultRadioPathFreqRange = [2]float64{144.0, 148.0}

// DefaultConfig возвращает конфигурацию приложения со всеми значениями по умолчанию.
//
// Используется при первом запуске (когда файла config.json ещё нет) и в тестах.
// Параметры TLE собираются из tracker.DefaultTLEStoreConfig() для единого источника
// дефолтов (DRY между слоями).
func DefaultConfig() *Config {
	tleDefaults := tracker.DefaultTLEStoreConfig()

	cfg := &Config{
		Version: CurrentVersion,
		Server:  ServerConfig{Port: defaultPort},
		UI: UIConfig{
			Theme:                defaultTheme,
			ShowAllTracksOnStart: false,
		},
		TLE: TLEConfig{
			CacheDir:       tleDefaults.CacheDir,
			Groups:         append([]string(nil), tleDefaults.Groups...),
			UpdateInterval: tleDefaults.UpdateInterval,
			MaxTLEAgeDays:  tleDefaults.MaxTLEAgeDays,
		},
		SatNOGS: SatNOGSConfig{
			Enabled:  defaultSatNOGSEnabled,
			CacheTTL: defaultSatNOGSCacheTTL,
		},
		Station: StationConfig{
			Name: defaultStationName,
			Type: "auto",
			Observer: ObserverConfig{
				Name: defaultObserverName,
				Lat:  defaultObserverLat,
				Lon:  defaultObserverLon,
				AltM: defaultObserverAlt,
			},
			RadioPaths: []RadioPath{defaultRadioPath()},
		},
	}

	// Путь к файлу исключений живёт внутри каталога кеша TLE — единое место хранения
	// пользовательских данных при первом запуске.
	cfg.ExcludeNoradFile = filepath.Join(cfg.TLE.CacheDir, defaultExcludeNoradFilename)

	return cfg
}

// defaultRadioPath возвращает один виртуальный радиотракт-имитатор. Используется
// при первом запуске, когда у пользователя ещё нет реального оборудования.
func defaultRadioPath() RadioPath {
	return RadioPath{
		ID:   defaultRadioPathID,
		Name: defaultRadioPathName,
		Antenna: AntennaConfig{
			Type:         defaultRadioPathAntennaType,
			Model:        defaultRadioPathAntennaModel,
			Band:         defaultRadioPathAntennaBand,
			FreqRangeMHz: defaultRadioPathFreqRange,
		},
		Receiver: ReceiverConfig{
			Driver: defaultRadioPathReceiverDriver,
			Serial: "",
			Defaults: ReceiverDefaults{
				CenterFreqHz: defaultReceiverCenterFreqHz,
				GainDB:       defaultReceiverGainDB,
				BandwidthHz:  defaultReceiverBandwidthHz,
				SampleRateHz: defaultReceiverSampleRateHz,
			},
		},
		Rotator: nil,
	}
}

// TLEStoreConfig переносит настройки TLE из единого конфига в форму, понятную
// пакету tracker. Не зависит от файла на диске — преобразование структур.
func (c *Config) TLEStoreConfig() *tracker.TLEStoreConfig {
	return &tracker.TLEStoreConfig{
		Groups:         append([]string(nil), c.TLE.Groups...),
		UpdateInterval: c.TLE.UpdateInterval,
		CacheDir:       c.TLE.CacheDir,
		MaxTLEAgeDays:  c.TLE.MaxTLEAgeDays,
	}
}

// Addr возвращает адрес HTTP-сервера в формате ":port" — для http.Server.Addr.
func (c *Config) Addr() string {
	return ":" + c.Server.Port
}
