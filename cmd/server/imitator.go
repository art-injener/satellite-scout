package main

import (
	"bufio"
	"fmt"
	"image"
	_ "image/jpeg" // картинки SSTV имитатора
	_ "image/png"
	"io"
	"os"
	"time"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/sdr/imitator"
	"github.com/art-injener/satellite-scout/internal/sigmf"
	"github.com/art-injener/satellite-scout/internal/sstv"
	"github.com/art-injener/satellite-scout/internal/wav"
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
		if sig.Kind == imitator.SignalSSTV {
			if err := applySSTV(&sig, s); err != nil {
				return imitator.Options{}, err
			}
		}
		opts.Signals = append(opts.Signals, sig)
	}
	return opts, nil
}

// applySSTV задаёт режим, картинку и период передачи SSTV из конфига.
func applySSTV(sig *imitator.Signal, s config.ImitatorSignal) error {
	id := s.SSTVMode
	if id == "" {
		id = sstv.Robot36.ID
	}
	mode, ok := sstv.ModeByID(id)
	if !ok {
		return fmt.Errorf("imitator sstv: unknown mode %q", id)
	}
	sig.SSTVMode = mode
	sig.Period = seconds(s.PeriodS)
	if s.Audio != "" {
		return applySSTVRecording(sig, s.Audio)
	}
	if s.Image == "" {
		return nil
	}
	f, err := os.Open(s.Image)
	if err != nil {
		return fmt.Errorf("imitator sstv image: %w", err)
	}
	defer f.Close()
	if sig.Image, _, err = image.Decode(f); err != nil {
		return fmt.Errorf("imitator sstv image %s: %w", s.Image, err)
	}
	return nil
}

// applySSTVRecording читает запись звука SSTV из WAV.
func applySSTVRecording(sig *imitator.Signal, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("imitator sstv audio: %w", err)
	}
	defer f.Close()
	rec, err := wav.Read(bufio.NewReader(f))
	if err != nil {
		return fmt.Errorf("imitator sstv audio %s: %w", path, err)
	}
	sig.Audio, sig.AudioRate = rec.Samples, float64(rec.SampleRate)
	return nil
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
