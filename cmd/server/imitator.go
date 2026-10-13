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
		}
		opts.Signals = append(opts.Signals, sig)
	}
	return opts
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}
