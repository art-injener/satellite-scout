package config

import (
	"errors"
	"testing"
	"time"
)

func TestValidate_DefaultConfigPasses(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig must be valid, got: %v", err)
	}
}

func TestValidate_ObserverLatOutOfRange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Station.Observer.Lat = 91.0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	verrs := mustValidationErrors(t, err)
	if !hasField(verrs, "station.observer.lat") {
		t.Errorf("expected error on station.observer.lat, got %v", verrs)
	}
}

func TestValidate_ObserverLonOutOfRange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Station.Observer.Lon = -181.0

	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "station.observer.lon") {
		t.Errorf("expected error on station.observer.lon, got %v", verrs)
	}
}

func TestValidate_ObserverAltOutOfRange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Station.Observer.AltM = 9000

	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "station.observer.alt_m") {
		t.Errorf("expected error on station.observer.alt_m, got %v", verrs)
	}
}

func TestValidate_DuplicateRadioPathID(t *testing.T) {
	cfg := DefaultConfig()
	rp := sampleRadioPath(1)
	dup := rp
	dup.Name = "Duplicate"
	cfg.Station.RadioPaths = []RadioPath{rp, dup}

	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "station.radio_paths[1].id") {
		t.Errorf("expected duplicate id error, got %v", verrs)
	}
}

func TestValidate_InvalidFreqRange(t *testing.T) {
	cfg := DefaultConfig()
	rp := sampleRadioPath(1)
	rp.Antenna.FreqRangeMHz = [2]float64{500, 100}
	cfg.Station.RadioPaths = []RadioPath{rp}

	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "station.radio_paths[0].antenna.freq_range_mhz") {
		t.Errorf("expected freq_range error, got %v", verrs)
	}
}

// TestValidate_EmptyRadioPathsAllowed — дефолтная конфигурация ("basic"):
// пустой список радиотрактов считается валидным, потому что пользователь без
// SDR-оборудования должен иметь возможность работать только с трекером.
func TestValidate_EmptyRadioPathsAllowed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Station.RadioPaths = []RadioPath{}

	if err := cfg.Validate(); err != nil {
		t.Errorf("empty RadioPaths must be valid for basic station, got: %v", err)
	}
}

// sampleRadioPath — фабрика валидного радиотракта для тестов, в которых нужно
// сконструировать минимальный тракт с заданным id (после ухода от обязательного
// дефолтного радиотракта в DefaultConfig).
func sampleRadioPath(id int) RadioPath {
	return RadioPath{
		ID:   id,
		Name: "Test Radio Path",
		Antenna: AntennaConfig{
			Type:         "omnidirectional",
			Model:        "QFH 145 MHz",
			Band:         "VHF",
			FreqRangeMHz: [2]float64{144.0, 148.0},
		},
		Receiver: ReceiverConfig{
			Driver: "simulated",
			Defaults: ReceiverDefaults{
				CenterFreqHz: 145_900_000,
				GainDB:       42,
				BandwidthHz:  2_400_000,
				SampleRateHz: 2_400_000,
			},
		},
	}
}

func TestValidate_AggregatesAllErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Station.Observer.Lat = 91.0
	cfg.Station.Observer.Lon = -181.0
	cfg.Server.Port = ""

	verrs := mustValidationErrors(t, cfg.Validate())
	if len(verrs) < 3 {
		t.Errorf("expected at least 3 errors aggregated, got %d: %v", len(verrs), verrs)
	}
}

func TestRestartRequiredFields_ServerPort(t *testing.T) {
	old := DefaultConfig()
	updated := DefaultConfig()
	updated.Server.Port = "9000"

	got := RestartRequiredFields(old, updated)
	if !hasString(got, "server.port") {
		t.Errorf("expected server.port in restart fields, got %v", got)
	}
}

func TestRestartRequiredFields_ObserverDoesNotRequireRestart(t *testing.T) {
	old := DefaultConfig()
	updated := DefaultConfig()
	updated.Station.Observer.Lat = 1.0
	updated.Station.Observer.Lon = 2.0
	updated.UI.Theme = "classic"

	got := RestartRequiredFields(old, updated)
	if len(got) != 0 {
		t.Errorf("observer/theme changes must be hot-reload, got restart fields %v", got)
	}
}

func TestRestartRequiredFields_TLEGroupsChanged(t *testing.T) {
	old := DefaultConfig()
	updated := DefaultConfig()
	updated.TLE.Groups = []string{"weather"}

	got := RestartRequiredFields(old, updated)
	if !hasString(got, "tle.groups") {
		t.Errorf("expected tle.groups, got %v", got)
	}
}

func TestRestartRequiredFields_NilSafe(t *testing.T) {
	if RestartRequiredFields(nil, DefaultConfig()) != nil {
		t.Error("expected nil for nil old")
	}
	if RestartRequiredFields(DefaultConfig(), nil) != nil {
		t.Error("expected nil for nil new")
	}
}

func TestValidate_TLEUpdateIntervalZero(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TLE.UpdateInterval = 0

	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "tle.update_interval") {
		t.Errorf("expected error on tle.update_interval, got %v", verrs)
	}
}

func TestValidate_SatNOGSCacheTTLZero(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SatNOGS.CacheTTL = 0

	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "satnogs.cache_ttl") {
		t.Errorf("expected error on satnogs.cache_ttl, got %v", verrs)
	}
}

func TestValidate_SatNOGSValidTTL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SatNOGS.CacheTTL = time.Minute

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with 1m cache TTL, got %v", err)
	}
}

func TestValidate_SatNOGSNegativeParams(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SatNOGS.Timeout = -1
	cfg.SatNOGS.MaxRetries = -1
	cfg.SatNOGS.Workers = -1

	verrs := mustValidationErrors(t, cfg.Validate())
	for _, field := range []string{"satnogs.timeout", "satnogs.max_retries", "satnogs.workers"} {
		if !hasField(verrs, field) {
			t.Errorf("expected error on %s, got %v", field, verrs)
		}
	}
}

func TestValidate_SatNOGSZeroParamsAllowed(t *testing.T) {
	cfg := DefaultConfig()
	// Ноль = «использовать дефолт», поэтому валидация должна пройти.
	cfg.SatNOGS.Timeout = 0
	cfg.SatNOGS.MaxRetries = 0
	cfg.SatNOGS.Workers = 0

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with zero satnogs params, got %v", err)
	}
}

func mustValidationErrors(t *testing.T, err error) ValidationErrors {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var verrs ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	return verrs
}

func hasField(errs ValidationErrors, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func hasString(arr []string, s string) bool {
	for _, v := range arr {
		if v == s {
			return true
		}
	}
	return false
}
