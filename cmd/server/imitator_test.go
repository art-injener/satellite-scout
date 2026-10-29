package main

import (
	"context"
	"image"
	"image/png"
	"os"
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
	for _, m := range config.ImitatorSSTVModes {
		signals = append(signals, config.ImitatorSignal{Kind: config.ImitatorSSTV, FreqHz: 436_500_000, LevelDBFS: -40,
			PeriodS: 10, SSTVMode: m})
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

func TestImitatorOptions_MapsSSTV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	f.Close()

	opts := mustOpts(t, &config.ImitatorConfig{Signals: []config.ImitatorSignal{
		{Kind: config.ImitatorSSTV, FreqHz: 436_500_000, PeriodS: 200, SSTVMode: "pd180", Image: path},
		{Kind: config.ImitatorSSTV, FreqHz: 436_500_000, PeriodS: 60},
	}})
	pd, robot := opts.Signals[0], opts.Signals[1]
	if pd.SSTVMode.ID != "pd180" || pd.Period != 200*time.Second || pd.Image == nil || pd.Image.Bounds().Dx() != 8 {
		t.Errorf("pd180 signal: mode %q period %v image %v", pd.SSTVMode.ID, pd.Period, pd.Image != nil)
	}
	if robot.SSTVMode.ID != "robot36" || robot.Image != nil {
		t.Errorf("default signal: mode %q image %v, want robot36 and the test pattern", robot.SSTVMode.ID, robot.Image != nil)
	}

	_, err = imitatorOptions(&config.ImitatorConfig{Signals: []config.ImitatorSignal{
		{Kind: config.ImitatorSSTV, FreqHz: 436_500_000, PeriodS: 60, Image: filepath.Join(dir, "missing.png")},
	}})
	if err == nil {
		t.Error("missing image accepted")
	}
}

func TestImitatorOptions_MapsSSTVRecording(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rec.wav")
	// 16-bit mono PCM at 8 kHz, three samples.
	wavFile := []byte("RIFF\x2a\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x40\x1f\x00\x00" +
		"\x80\x3e\x00\x00\x02\x00\x10\x00data\x06\x00\x00\x00\x00\x00\x00\x40\x00\xc0")
	if err := os.WriteFile(path, wavFile, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := mustOpts(t, &config.ImitatorConfig{Signals: []config.ImitatorSignal{
		{Kind: config.ImitatorSSTV, FreqHz: 436_500_000, PeriodS: 60, Audio: path},
	}})
	sig := opts.Signals[0]
	if sig.AudioRate != 8000 || len(sig.Audio) != 3 || sig.Audio[1] != 0.5 || sig.Audio[2] != -0.5 {
		t.Errorf("recording: rate %v samples %v", sig.AudioRate, sig.Audio)
	}

	_, err := imitatorOptions(&config.ImitatorConfig{Signals: []config.ImitatorSignal{
		{Kind: config.ImitatorSSTV, FreqHz: 436_500_000, PeriodS: 60, Audio: filepath.Join(dir, "missing.wav")},
	}})
	if err == nil {
		t.Error("missing recording accepted")
	}
}
