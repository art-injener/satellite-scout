package main

import (
	"fmt"
	"io"
	"time"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/sdr/imitator"
	"github.com/art-injener/satellite-scout/internal/sigmf"
)

// imitatorOptions собирает эфир имитатора из секции конфига; без секции — эфир по умолчанию.
func imitatorOptions(im *config.ImitatorConfig) (imitator.Options, error) {
	if im == nil {
		return imitator.DefaultOptions(), nil
	}
	if im.Replay != "" {
		path := im.Replay
		open := func() (io.ReadCloser, error) { return sigmf.Open(path) }
		// Проверка при старте: meta/data читаются; rate/center — из первого Open.
		ds, err := sigmf.Open(path)
		if err != nil {
			return imitator.Options{}, fmt.Errorf("imitator replay: %w", err)
		}
		return imitator.Options{
			Source:           ds,
			SourceCloser:     ds,
			SourceOpen:       open,
			ReplaySampleRate: ds.SampleRate(),
			ReplayCenterHz:   ds.CenterHz(),
		}, nil
	}
	opts := imitator.Options{NoiseDBFS: im.NoiseDBFS, Seed: 1}
	if opts.NoiseDBFS == 0 {
		opts.NoiseDBFS = imitator.DefaultNoiseDBFS
	}
	for _, s := range im.Signals {
		sig := imitator.Signal{
			Kind:           imitator.SignalKind(s.Kind),
			FrequencyHz:    s.FreqHz,
			LevelDBFS:      s.LevelDBFS,
			DopplerHz:      s.DopplerHz,
			DopplerRateHzS: s.DopplerRateHzS,
			SweepHz:        s.SweepHz,
			SweepPeriod:    seconds(s.SweepPeriodS),
			PassSwingDB:    s.PassSwingDB,
			PassPeriod:     seconds(s.PassPeriodS),
		}
		if sig.Kind == imitator.SignalBeacon {
			sig.Modem = imitator.BeaconModem(s.Modem)
			sig.Frame = imitator.DefaultBeaconFrame(sig.Modem)
			sig.Period = seconds(s.PeriodS)
		}
		opts.Signals = append(opts.Signals, sig)
	}
	return opts, nil
}

// pathImitatorOptions — эфир тракта и частота дискретизации для PathConfig.
// При replay частота берётся из SigMF; иначе из конфига (0 — рекомендуемая).
func pathImitatorOptions(im *config.ImitatorConfig, sampleRateHz uint32) (imitator.Options, uint32, error) {
	opts, err := imitatorOptions(im)
	if err != nil {
		return imitator.Options{}, 0, err
	}
	if opts.ReplaySampleRate != 0 {
		return opts, opts.ReplaySampleRate, nil
	}
	opts.NoiseRefRateHz = imitator.RecommendedSampleRate
	if sampleRateHz != 0 {
		opts.NoiseRefRateHz = float64(sampleRateHz)
	}
	return opts, sampleRateHz, nil
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}
