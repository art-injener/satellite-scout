package main

import (
	"time"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/sdr/imitator"
)

// imitatorOptions собирает эфир имитатора из секции конфига; без секции — эфир по умолчанию.
func imitatorOptions(im *config.ImitatorConfig) imitator.Options {
	if im == nil {
		return imitator.DefaultOptions()
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
			sig.Frame = imitator.DefaultBeaconFrame()
			sig.Period = seconds(s.PeriodS)
			sig.Modem = imitator.BeaconModem(s.Modem)
		}
		opts.Signals = append(opts.Signals, sig)
	}
	return opts
}

// pathImitatorOptions — эфир имитатора тракта: шум задан при частоте дискретизации из конфига
// (0 — рекомендуемая), при смене «Полосы» он пересчитывается по плотности.
func pathImitatorOptions(im *config.ImitatorConfig, sampleRateHz uint32) imitator.Options {
	opts := imitatorOptions(im)
	opts.NoiseRefRateHz = imitator.RecommendedSampleRate
	if sampleRateHz != 0 {
		opts.NoiseRefRateHz = float64(sampleRateHz)
	}
	return opts
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}
