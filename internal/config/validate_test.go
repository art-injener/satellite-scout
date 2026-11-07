package config

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
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
			Type: AntennaTypeStationary,
			Kind: AntennaKindQFH,
			Name: "QFH 145",
		},
		Receiver: ReceiverConfig{
			Driver: "simulated",
		},
	}
}

func TestValidate_AntennaWithoutRotatorRequiresKind(t *testing.T) {
	cfg := DefaultConfig()
	rp := sampleRadioPath(1)
	rp.Antenna.Kind = ""
	cfg.Station.RadioPaths = []RadioPath{rp}

	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "station.radio_paths[0].antenna.kind") {
		t.Errorf("expected error on antenna.kind, got %v", verrs)
	}
}

func TestValidate_UnknownAntennaKindRejected(t *testing.T) {
	cfg := DefaultConfig()
	rp := sampleRadioPath(1)
	rp.Antenna.Kind = "helix"
	cfg.Station.RadioPaths = []RadioPath{rp}

	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "station.radio_paths[0].antenna.kind") {
		t.Errorf("expected error on antenna.kind, got %v", verrs)
	}
}

func TestValidate_UnknownAntennaKindRejectedWithRotator(t *testing.T) {
	cfg := DefaultConfig()
	rp := sampleRadioPath(1)
	rp.Antenna.Type = AntennaTypeRotatable
	rp.Antenna.Kind = "helix"
	rp.Rotator = &RotatorConfig{Driver: "rotctld", Port: 4533}
	cfg.Station.RadioPaths = []RadioPath{rp}

	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "station.radio_paths[0].antenna.kind") {
		t.Errorf("expected error on antenna.kind, got %v", verrs)
	}
}

func TestValidate_AntennaKindOptionalWithRotator(t *testing.T) {
	cfg := DefaultConfig()
	rp := sampleRadioPath(1)
	rp.Antenna.Type = AntennaTypeRotatable
	rp.Antenna.Kind = ""
	rp.Rotator = &RotatorConfig{Driver: "rotctld", Port: 4533}
	cfg.Station.RadioPaths = []RadioPath{rp}

	if err := cfg.Validate(); err != nil {
		t.Errorf("kind must be optional on a rotator, got %v", err)
	}
}

func TestValidate_AllAntennaKindsAccepted(t *testing.T) {
	for _, kind := range AntennaKinds {
		cfg := DefaultConfig()
		rp := sampleRadioPath(1)
		rp.Antenna.Kind = kind
		cfg.Station.RadioPaths = []RadioPath{rp}
		if err := cfg.Validate(); err != nil {
			t.Errorf("kind %q must be valid, got %v", kind, err)
		}
	}
}

func TestAntennaKinds_ListsAllKinds(t *testing.T) {
	want := []string{AntennaKindQFH, AntennaKindYagi, AntennaKindDipole, AntennaKindWhip}
	if !slices.Equal(AntennaKinds, want) {
		t.Errorf("AntennaKinds = %v, want %v", AntennaKinds, want)
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

func TestReceiverConfig_SampleRateFromJSON(t *testing.T) {
	var rc ReceiverConfig
	if err := json.Unmarshal([]byte(`{"driver":"simulated","sample_rate_hz":2048000}`), &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc.SampleRateHz != 2_048_000 {
		t.Errorf("SampleRateHz=%d, want 2048000", rc.SampleRateHz)
	}
}

func TestRestartRequiredFields_ReceiverSampleRateChanged(t *testing.T) {
	old := DefaultConfig()
	old.Station.RadioPaths = []RadioPath{sampleRadioPath(1)}
	updated := DefaultConfig()
	rp := sampleRadioPath(1)
	rp.Receiver.SampleRateHz = 1_024_000
	updated.Station.RadioPaths = []RadioPath{rp}

	got := RestartRequiredFields(old, updated)
	if !hasString(got, "station.radio_paths") {
		t.Errorf("sample rate is applied when the device opens, expected restart, got %v", got)
	}
}

func sampleImitator() *ImitatorConfig {
	return &ImitatorConfig{
		NoiseDBFS: -60,
		Signals: []ImitatorSignal{
			{Kind: ImitatorCarrier, FreqHz: 436_500_000, LevelDBFS: -40, SweepHz: 1_000_000, SweepPeriodS: 60},
			{Kind: ImitatorAFSKPRBS, FreqHz: 436_510_000, LevelDBFS: -50, DopplerHz: -2_000, DopplerRateHzS: 15,
				PassSwingDB: 30, PassPeriodS: 600},
			{Kind: ImitatorBeacon, FreqHz: 436_490_000, LevelDBFS: -45, PeriodS: 10},
			{Kind: ImitatorBeacon, FreqHz: 436_470_000, LevelDBFS: -45, PeriodS: 10, Modem: ImitatorFSK9600G3RUH},
			{Kind: ImitatorSSTV, FreqHz: 436_450_000, LevelDBFS: -45, PeriodS: 120, SSTVMode: "pd120"},
		},
	}
}

func imitatorPath(imitator *ImitatorConfig) RadioPath {
	rp := sampleRadioPath(1)
	rp.Receiver.Imitator = imitator
	return rp
}

func TestValidate_ImitatorSectionPasses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Station.RadioPaths = []RadioPath{imitatorPath(sampleImitator())}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid imitator section rejected: %v", err)
	}
}

func TestValidate_ImitatorSignalErrors(t *testing.T) {
	const prefix = "station.radio_paths[0].receiver.imitator"
	cases := []struct {
		name  string
		edit  func(im *ImitatorConfig)
		field string
	}{
		{"unknown kind", func(im *ImitatorConfig) { im.Signals[0].Kind = "noise" }, ".signals[0].kind"},
		{"zero frequency", func(im *ImitatorConfig) { im.Signals[1].FreqHz = 0 }, ".signals[1].freq_hz"},
		{"level above full scale", func(im *ImitatorConfig) { im.Signals[2].LevelDBFS = 3 }, ".signals[2].level_dbfs"},
		{"beacon without period", func(im *ImitatorConfig) { im.Signals[2].PeriodS = 0 }, ".signals[2].period_s"},
		{"sweep without period", func(im *ImitatorConfig) { im.Signals[0].SweepPeriodS = 0 }, ".signals[0].sweep_period_s"},
		{"negative sweep", func(im *ImitatorConfig) { im.Signals[0].SweepHz = -1 }, ".signals[0].sweep_hz"},
		{"pass without period", func(im *ImitatorConfig) { im.Signals[1].PassPeriodS = 0 }, ".signals[1].pass_period_s"},
		{"negative pass swing", func(im *ImitatorConfig) { im.Signals[1].PassSwingDB = -5 }, ".signals[1].pass_swing_db"},
		{"noise above full scale", func(im *ImitatorConfig) { im.NoiseDBFS = 1 }, ".noise_dbfs"},
		{"unknown beacon modem", func(im *ImitatorConfig) { im.Signals[3].Modem = "gmsk9600" }, ".signals[3].modem"},
		{"modem on non-beacon", func(im *ImitatorConfig) { im.Signals[1].Modem = ImitatorAFSK1200 }, ".signals[1].modem"},
		{"sstv without period", func(im *ImitatorConfig) { im.Signals[4].PeriodS = 0 }, ".signals[4].period_s"},
		{"unknown sstv mode", func(im *ImitatorConfig) { im.Signals[4].SSTVMode = "martin1" }, ".signals[4].sstv_mode"},
		{"sstv mode on beacon", func(im *ImitatorConfig) { im.Signals[2].SSTVMode = "robot36" }, ".signals[2].sstv_mode"},
		{"sstv image on carrier", func(im *ImitatorConfig) { im.Signals[0].Image = "a.png" }, ".signals[0].sstv_mode"},
		{"sstv audio on carrier", func(im *ImitatorConfig) { im.Signals[0].Audio = "a.wav" }, ".signals[0].audio"},
		{"sstv audio with image", func(im *ImitatorConfig) {
			im.Signals[4].Audio, im.Signals[4].Image = "a.wav", "a.png"
		}, ".signals[4].audio"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			im := sampleImitator()
			c.edit(im)
			cfg := DefaultConfig()
			cfg.Station.RadioPaths = []RadioPath{imitatorPath(im)}
			verrs := mustValidationErrors(t, cfg.Validate())
			if !hasField(verrs, prefix+c.field) {
				t.Errorf("expected error on %s, got %v", prefix+c.field, verrs)
			}
		})
	}
}

func TestValidate_ImitatorOnlyForSimulatedDriver(t *testing.T) {
	rp := imitatorPath(sampleImitator())
	rp.Receiver.Driver = "rtlsdr"
	cfg := DefaultConfig()
	cfg.Station.RadioPaths = []RadioPath{rp}
	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "station.radio_paths[0].receiver.imitator") {
		t.Errorf("expected error on receiver.imitator for rtlsdr, got %v", verrs)
	}
}

func TestValidate_ImitatorReplayPath(t *testing.T) {
	im := &ImitatorConfig{Replay: "data/rec/x.sigmf-meta"}
	cfg := DefaultConfig()
	cfg.Station.RadioPaths = []RadioPath{imitatorPath(im)}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid replay rejected: %v", err)
	}
	im.Replay = "data/rec/x.cfile"
	verrs := mustValidationErrors(t, cfg.Validate())
	if !hasField(verrs, "station.radio_paths[0].receiver.imitator.replay") {
		t.Errorf("expected replay suffix error, got %v", verrs)
	}
}

func TestValidate_CalibrationRange(t *testing.T) {
	const field = "station.radio_paths[0].receiver.calibration_db"
	cases := []struct {
		db      float64
		wantErr bool
	}{
		{db: -40, wantErr: false},
		{db: CalibrationMinDB, wantErr: false},
		{db: CalibrationMaxDB, wantErr: false},
		{db: CalibrationMinDB - 0.1, wantErr: true},
		{db: CalibrationMaxDB + 0.1, wantErr: true},
	}
	for _, c := range cases {
		rp := sampleRadioPath(1)
		db := c.db
		rp.Receiver.CalibrationDB = &db
		cfg := DefaultConfig()
		cfg.Station.RadioPaths = []RadioPath{rp}
		err := cfg.Validate()
		if !c.wantErr {
			if err != nil {
				t.Errorf("calibration %.1f rejected: %v", c.db, err)
			}
			continue
		}
		if verrs := mustValidationErrors(t, err); !hasField(verrs, field) {
			t.Errorf("calibration %.1f: expected error on %s, got %v", c.db, field, verrs)
		}
	}
}

func TestValidate_FreqCorrectionRange(t *testing.T) {
	const field = "station.radio_paths[0].receiver.freq_correction_ppm"
	cases := []struct {
		ppm     int
		wantErr bool
	}{
		{ppm: 0, wantErr: false},
		{ppm: -37, wantErr: false},
		{ppm: FreqCorrectionMaxPPM, wantErr: false},
		{ppm: -FreqCorrectionMaxPPM, wantErr: false},
		{ppm: FreqCorrectionMaxPPM + 1, wantErr: true},
		{ppm: -FreqCorrectionMaxPPM - 1, wantErr: true},
	}
	for _, c := range cases {
		rp := sampleRadioPath(1)
		rp.Receiver.FreqCorrectionPPM = c.ppm
		cfg := DefaultConfig()
		cfg.Station.RadioPaths = []RadioPath{rp}
		err := cfg.Validate()
		if !c.wantErr {
			if err != nil {
				t.Errorf("ppm %d rejected: %v", c.ppm, err)
			}
			continue
		}
		if verrs := mustValidationErrors(t, err); !hasField(verrs, field) {
			t.Errorf("ppm %d: expected error on %s, got %v", c.ppm, field, verrs)
		}
	}
}

func TestReceiverConfig_FreqCorrectionJSON(t *testing.T) {
	var rc ReceiverConfig
	if err := json.Unmarshal([]byte(`{"driver":"rtlsdr","freq_correction_ppm":-12}`), &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc.FreqCorrectionPPM != -12 {
		t.Errorf("freq_correction_ppm=%d, want -12", rc.FreqCorrectionPPM)
	}
	out, err := json.Marshal(ReceiverConfig{Driver: "rtlsdr"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "freq_correction_ppm") {
		t.Errorf("zero correction must not be written: %s", out)
	}
}

func TestReceiverConfig_CalibrationJSON(t *testing.T) {
	var rc ReceiverConfig
	if err := json.Unmarshal([]byte(`{"driver":"rtlsdr","calibration_db":-23.5}`), &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc.CalibrationDB == nil || *rc.CalibrationDB != -23.5 {
		t.Errorf("calibration_db=%v, want -23.5", rc.CalibrationDB)
	}
	out, err := json.Marshal(ReceiverConfig{Driver: "rtlsdr"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "calibration_db") {
		t.Errorf("absent calibration must not be written: %s", out)
	}
}

func TestClone_CalibrationIsDeepCopied(t *testing.T) {
	cfg := DefaultConfig()
	rp := sampleRadioPath(1)
	db := -40.0
	rp.Receiver.CalibrationDB = &db
	cfg.Station.RadioPaths = []RadioPath{rp}

	dup := cloneConfig(cfg)
	*dup.Station.RadioPaths[0].Receiver.CalibrationDB = -10
	if db != -40 {
		t.Errorf("clone shares calibration with the original: original became %.1f", db)
	}
}

func TestReceiverConfig_ImitatorJSON(t *testing.T) {
	raw := `{"driver":"simulated","imitator":{"noise_dbfs":-55,"signals":[
		{"kind":"afsk_prbs","freq_hz":437000000,"level_dbfs":-45,"doppler_hz":-1500,"doppler_rate_hz_s":20},
		{"kind":"carrier","freq_hz":436500000,"level_dbfs":-40,"sweep_hz":500000,"sweep_period_s":30,
		 "pass_swing_db":50,"pass_period_s":45},
		{"kind":"beacon","freq_hz":436400000,"level_dbfs":-50,"period_s":5,"modem":"fsk9600_g3ruh"}]}}`
	var rc ReceiverConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := &ImitatorConfig{NoiseDBFS: -55, Signals: []ImitatorSignal{
		{Kind: ImitatorAFSKPRBS, FreqHz: 437_000_000, LevelDBFS: -45, DopplerHz: -1500, DopplerRateHzS: 20},
		{Kind: ImitatorCarrier, FreqHz: 436_500_000, LevelDBFS: -40, SweepHz: 500_000, SweepPeriodS: 30,
			PassSwingDB: 50, PassPeriodS: 45},
		{Kind: ImitatorBeacon, FreqHz: 436_400_000, LevelDBFS: -50, PeriodS: 5, Modem: ImitatorFSK9600G3RUH},
	}}
	if !reflect.DeepEqual(rc.Imitator, want) {
		t.Errorf("imitator=%+v, want %+v", rc.Imitator, want)
	}
}

func TestRestartRequiredFields_ImitatorSameContentNoRestart(t *testing.T) {
	old := DefaultConfig()
	old.Station.RadioPaths = []RadioPath{imitatorPath(sampleImitator())}
	updated := DefaultConfig()
	updated.Station.RadioPaths = []RadioPath{imitatorPath(sampleImitator())}
	if got := RestartRequiredFields(old, updated); hasString(got, "station.radio_paths") {
		t.Errorf("equal imitator sections must not require restart, got %v", got)
	}
}

func TestRestartRequiredFields_ImitatorChanged(t *testing.T) {
	old := DefaultConfig()
	old.Station.RadioPaths = []RadioPath{imitatorPath(sampleImitator())}
	im := sampleImitator()
	im.Signals[1].LevelDBFS = -30
	updated := DefaultConfig()
	updated.Station.RadioPaths = []RadioPath{imitatorPath(im)}
	if got := RestartRequiredFields(old, updated); !hasString(got, "station.radio_paths") {
		t.Errorf("imitator is built when the device opens, expected restart, got %v", got)
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
	cfg.SatNOGS.CacheTTL = Duration(time.Minute)

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with 1m cache TTL, got %v", err)
	}
}

func TestValidate_SatNOGSNegativeParams(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SatNOGS.Timeout = Duration(-1)
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
	return slices.Contains(arr, s)
}
