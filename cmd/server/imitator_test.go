package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sdr/imitator"
)

func mustOpts(t *testing.T, im *config.ImitatorConfig) imitator.Options {
	t.Helper()
	opts, err := imitatorOptions(im)
	if err != nil {
		t.Fatal(err)
	}
	return opts
}

func TestImitatorOptions_NilSectionUsesDefault(t *testing.T) {
	got := mustOpts(t, nil)
	if want := imitator.DefaultOptions(); !reflect.DeepEqual(got, want) {
		t.Errorf("options=%+v, want default %+v", got, want)
	}
}

// Без секции imitator в эфире должен быть маяк каждого модема, который знает конфиг:
// новый формат проверяется в браузере без правки конфига.
func TestImitatorOptions_DefaultHasBeaconOfEveryConfigModem(t *testing.T) {
	have := map[string]bool{}
	for _, s := range mustOpts(t, nil).Signals {
		if s.Kind == imitator.SignalBeacon {
			have[string(s.Modem)] = true
		}
	}
	for _, m := range config.ImitatorBeaconModems {
		if !have[m] {
			t.Errorf("default imitator has no beacon for modem %q", m)
		}
	}
}

func TestImitatorOptions_MapsSignals(t *testing.T) {
	got := mustOpts(t, &config.ImitatorConfig{
		NoiseDBFS: -55,
		Signals: []config.ImitatorSignal{
			{Kind: config.ImitatorAFSKPRBS, FreqHz: 437_000_000, LevelDBFS: -45, DopplerHz: -1500, DopplerRateHzS: 20},
			{Kind: config.ImitatorCarrier, FreqHz: 436_500_000, LevelDBFS: -40, SweepHz: 500_000, SweepPeriodS: 30,
				PassSwingDB: 50, PassPeriodS: 45},
			{Kind: config.ImitatorBeacon, FreqHz: 436_400_000, LevelDBFS: -50, PeriodS: 5},
			{Kind: config.ImitatorBeacon, FreqHz: 436_300_000, LevelDBFS: -50, PeriodS: 5, Modem: config.ImitatorFSK9600G3RUH},
		},
	})
	want := imitator.Options{
		NoiseDBFS: -55,
		Seed:      1,
		Signals: []imitator.Signal{
			{Kind: imitator.SignalAFSKPRBS, FrequencyHz: 437_000_000, LevelDBFS: -45, DopplerHz: -1500, DopplerRateHzS: 20},
			{Kind: imitator.SignalCarrier, FrequencyHz: 436_500_000, LevelDBFS: -40, SweepHz: 500_000, SweepPeriod: 30 * time.Second,
				PassSwingDB: 50, PassPeriod: 45 * time.Second},
			{Kind: imitator.SignalBeacon, FrequencyHz: 436_400_000, LevelDBFS: -50,
				Frame: imitator.DefaultBeaconFrame(""), Period: 5 * time.Second},
			{Kind: imitator.SignalBeacon, FrequencyHz: 436_300_000, LevelDBFS: -50,
				Frame:  imitator.DefaultBeaconFrame(imitator.BeaconFSK9600G3RUH),
				Period: 5 * time.Second, Modem: imitator.BeaconFSK9600G3RUH},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("options=%+v\nwant %+v", got, want)
	}
}

func TestImitatorOptions_NoiseReferencedToPathRate(t *testing.T) {
	opts, rate, err := pathImitatorOptions(nil, 1_024_000)
	if err != nil {
		t.Fatal(err)
	}
	if opts.NoiseRefRateHz != 1_024_000 || rate != 1_024_000 {
		t.Errorf("NoiseRefRateHz=%v rate=%d, want path rate", opts.NoiseRefRateHz, rate)
	}
	opts, rate, err = pathImitatorOptions(&config.ImitatorConfig{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if opts.NoiseRefRateHz != imitator.RecommendedSampleRate || rate != 0 {
		t.Errorf("NoiseRefRateHz=%v rate=%d, want recommended / 0", opts.NoiseRefRateHz, rate)
	}
}

func TestImitatorOptions_ZeroNoiseUsesDefault(t *testing.T) {
	got := mustOpts(t, &config.ImitatorConfig{})
	if got.NoiseDBFS != imitator.DefaultNoiseDBFS {
		t.Errorf("NoiseDBFS=%.0f, want default %.0f", got.NoiseDBFS, imitator.DefaultNoiseDBFS)
	}
}

func TestImitatorOptions_ReplayOpensSigMF(t *testing.T) {
	meta := filepath.Join("..", "..", "internal", "sigmf", "testdata", "tone_cf32.sigmf-meta")
	opts, rate, err := pathImitatorOptions(&config.ImitatorConfig{Replay: meta}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Source == nil || opts.SourceCloser == nil || opts.SourceOpen == nil {
		t.Fatal("replay must set Source, SourceCloser and SourceOpen")
	}
	defer opts.SourceCloser.Close()
	if rate != 32000 || opts.ReplaySampleRate != 32000 {
		t.Errorf("rate=%d ReplaySampleRate=%d, want 32000", rate, opts.ReplaySampleRate)
	}
	if opts.ReplayCenterHz != 436_666_000 {
		t.Errorf("center=%d", opts.ReplayCenterHz)
	}
	r := imitator.New(opts)
	if caps := r.Capabilities(); caps.RecommendedSampleRate != 32000 {
		t.Errorf("RecommendedSampleRate=%d", caps.RecommendedSampleRate)
	}
	// Фикстура — 8 отсчётов; с Seek запись крутится, за 200 мс кадров больше одного прохода.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{SampleRate: 32000, BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for frame := range ch {
		total += len(frame.Samples)
	}
	if total <= 8 {
		t.Fatalf("looping samples=%d, want > 8", total)
	}
}

// Каждый вид сигнала и модем маяка, которые пропускает проверка конфига, должны запускаться в имитаторе.
func TestImitatorOptions_EveryConfigKindStarts(t *testing.T) {
	signals := make([]config.ImitatorSignal, 0, len(config.ImitatorSignalKinds)+len(config.ImitatorBeaconModems))
	for _, kind := range config.ImitatorSignalKinds {
		signals = append(signals, config.ImitatorSignal{Kind: kind, FreqHz: 436_500_000, LevelDBFS: -40, PeriodS: 10})
	}
	for _, m := range config.ImitatorBeaconModems {
		signals = append(signals, config.ImitatorSignal{Kind: config.ImitatorBeacon, FreqHz: 436_500_000, LevelDBFS: -40,
			PeriodS: 10, Modem: m})
	}
	for _, s := range signals {
		r := imitator.New(mustOpts(t, &config.ImitatorConfig{Signals: []config.ImitatorSignal{s}}))
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		ch, err := r.StartStream(ctx, sdr.StreamConfig{SampleRate: 48_000, BlockSize: 1024})
		if err != nil {
			t.Errorf("kind %q modem %q: StartStream: %v", s.Kind, s.Modem, err)
			cancel()
			continue
		}
		cancel()
		for range ch {
		}
		_ = r.Stop()
	}
}
