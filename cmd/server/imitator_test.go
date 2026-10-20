package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sdr/imitator"
)

func TestImitatorOptions_NilSectionUsesDefault(t *testing.T) {
	if got, want := imitatorOptions(nil), imitator.DefaultOptions(); !reflect.DeepEqual(got, want) {
		t.Errorf("options=%+v, want default %+v", got, want)
	}
}

func TestImitatorOptions_MapsSignals(t *testing.T) {
	got := imitatorOptions(&config.ImitatorConfig{
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
				Frame: imitator.DefaultBeaconFrame(), Period: 5 * time.Second},
			{Kind: imitator.SignalBeacon, FrequencyHz: 436_300_000, LevelDBFS: -50,
				Frame: imitator.DefaultBeaconFrame(), Period: 5 * time.Second, Modem: imitator.BeaconFSK9600G3RUH},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("options=%+v\nwant %+v", got, want)
	}
}

func TestImitatorOptions_NoiseReferencedToPathRate(t *testing.T) {
	if got := pathImitatorOptions(nil, 1_024_000).NoiseRefRateHz; got != 1_024_000 {
		t.Errorf("NoiseRefRateHz=%v, want the configured path rate", got)
	}
	if got := pathImitatorOptions(&config.ImitatorConfig{}, 0).NoiseRefRateHz; got != imitator.RecommendedSampleRate {
		t.Errorf("NoiseRefRateHz=%v without a configured rate, want recommended %v", got, imitator.RecommendedSampleRate)
	}
}

func TestImitatorOptions_ZeroNoiseUsesDefault(t *testing.T) {
	got := imitatorOptions(&config.ImitatorConfig{})
	if got.NoiseDBFS != imitator.DefaultNoiseDBFS {
		t.Errorf("NoiseDBFS=%.0f, want default %.0f", got.NoiseDBFS, imitator.DefaultNoiseDBFS)
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
		r := imitator.New(imitatorOptions(&config.ImitatorConfig{Signals: []config.ImitatorSignal{s}}))
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		ch, err := r.StartStream(ctx, sdr.StreamConfig{SampleRate: 48_000, BlockSize: 1024})
		if err != nil {
			t.Errorf("kind %q modem %q: StartStream: %v", s.Kind, s.Modem, err)
			cancel()
			continue
		}
		<-ch
		_ = r.Stop()
		cancel()
	}
}
