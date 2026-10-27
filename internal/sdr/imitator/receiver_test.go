package imitator

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/cmplx"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

// replayMem — Seek + Close для тестов циклического replay.
type replayMem struct{ *bytes.Reader }

func (replayMem) Close() error { return nil }

func TestReceiver_Name(t *testing.T) {
	r := New(Options{})
	if got := r.Name(); got != "imitator" {
		t.Fatalf("Name() = %q, want %q", got, "imitator")
	}
}

func TestReceiver_Capabilities(t *testing.T) {
	r := New(Options{})
	caps := r.Capabilities()
	if caps.MinFreqHz == 0 || caps.MaxFreqHz == 0 {
		t.Fatal("capabilities must have non-zero freq range")
	}
	if caps.MaxFreqHz <= caps.MinFreqHz {
		t.Fatalf("MaxFreqHz (%d) must be > MinFreqHz (%d)", caps.MaxFreqHz, caps.MinFreqHz)
	}
}

func TestReceiver_Tune(t *testing.T) {
	r := New(Options{})
	caps := r.Capabilities()

	if err := r.Tune(caps.MinFreqHz); err != nil {
		t.Fatalf("Tune(MinFreqHz) error: %v", err)
	}
	if err := r.Tune(caps.MaxFreqHz); err != nil {
		t.Fatalf("Tune(MaxFreqHz) error: %v", err)
	}
	if err := r.Tune(0); err == nil {
		t.Fatal("Tune(0) should return error")
	}
	if err := r.Tune(caps.MaxFreqHz + 1); err == nil {
		t.Fatal("Tune(above max) should return error")
	}
}

func TestReceiver_StartStream_SyntheticTone(t *testing.T) {
	r := New(Options{ToneOffsetHz: 1000})

	cfg := sdr.StreamConfig{
		CenterHz:   145_900_000,
		SampleRate: 48000,
		BlockSize:  1024,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := r.StartStream(ctx, cfg)
	if err != nil {
		t.Fatalf("StartStream error: %v", err)
	}

	const wantFrames = 3
	received := 0
	for frame := range ch {
		if len(frame.Samples) != cfg.BlockSize {
			t.Fatalf("frame %d: len(Samples)=%d, want %d", received, len(frame.Samples), cfg.BlockSize)
		}
		if frame.CenterHz != cfg.CenterHz {
			t.Fatalf("frame %d: CenterHz=%d, want %d", received, frame.CenterHz, cfg.CenterHz)
		}
		if frame.SampleRate != cfg.SampleRate {
			t.Fatalf("frame %d: SampleRate=%d, want %d", received, frame.SampleRate, cfg.SampleRate)
		}
		if frame.TimestampMs == 0 {
			t.Fatalf("frame %d: zero timestamp", received)
		}
		allZero := true
		for _, s := range frame.Samples {
			if s != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Fatalf("frame %d: all samples are zero", received)
		}
		received++
		if received >= wantFrames {
			cancel()
		}
	}
	if received < wantFrames {
		t.Fatalf("received %d frames, want >= %d", received, wantFrames)
	}
}

func TestReplayBlockSize(t *testing.T) {
	if got := replayBlockSize(32_000, 16_384); got != 1600 {
		t.Fatalf("32k: got %d, want 1600 (50 ms)", got)
	}
	if got := replayBlockSize(2_048_000, 16_384); got != 16_384 {
		t.Fatalf("2M: got %d, want unchanged 16384", got)
	}
	if got := replayBlockSize(1_000, 16_384); got != 512 {
		t.Fatalf("1k: got %d, want floor 512", got)
	}
}

func TestReceiver_StartStream_ReplayReader(t *testing.T) {
	const numSamples = 2048
	buf := &bytes.Buffer{}
	for i := range numSamples {
		phase := 2 * math.Pi * float64(i) * 1000 / 48000
		re := float32(math.Cos(phase))
		im := float32(math.Sin(phase))
		binary.Write(buf, binary.LittleEndian, re)
		binary.Write(buf, binary.LittleEndian, im)
	}

	r := New(Options{Source: buf})

	cfg := sdr.StreamConfig{
		CenterHz:   145_900_000,
		SampleRate: 48000,
		BlockSize:  512,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := r.StartStream(ctx, cfg)
	if err != nil {
		t.Fatalf("StartStream error: %v", err)
	}

	totalSamples := 0
	for frame := range ch {
		if len(frame.Samples) != cfg.BlockSize {
			t.Fatalf("frame len=%d, want %d", len(frame.Samples), cfg.BlockSize)
		}
		totalSamples += len(frame.Samples)
	}

	if totalSamples != numSamples {
		t.Fatalf("total samples=%d, want %d", totalSamples, numSamples)
	}
}

// Как хаб: Stop закрывает файл; следующий StartStream открывает запись снова, при EOF крутит её.
func TestReceiver_StartStream_ReplayReopensAfterStopAndLoops(t *testing.T) {
	const numSamples = 512
	raw := make([]byte, numSamples*8)
	for i := range numSamples {
		binary.LittleEndian.PutUint32(raw[i*8:], math.Float32bits(float32(i)))
		binary.LittleEndian.PutUint32(raw[i*8+4:], math.Float32bits(0))
	}
	open := func() (io.ReadCloser, error) {
		return &replayMem{Reader: bytes.NewReader(append([]byte(nil), raw...))}, nil
	}
	src, err := open()
	if err != nil {
		t.Fatal(err)
	}
	r := New(Options{Source: src, SourceCloser: src, SourceOpen: open})
	cfg := sdr.StreamConfig{SampleRate: 48000, BlockSize: 128}

	ctx1, cancel1 := context.WithCancel(context.Background())
	ch1, err := r.StartStream(ctx1, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch1; !ok {
		t.Fatal("first stream empty")
	}
	cancel1()
	for range ch1 {
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	ch2, err := r.StartStream(ctx2, cfg)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for frame := range ch2 {
		total += len(frame.Samples)
	}
	if total <= numSamples {
		t.Fatalf("looping replay: got %d samples, want > %d (file length)", total, numSamples)
	}
}

func TestReceiver_Stop(t *testing.T) {
	r := New(Options{ToneOffsetHz: 1000})

	ctx := context.Background()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{
		CenterHz:   145_900_000,
		SampleRate: 48000,
		BlockSize:  256,
	})
	if err != nil {
		t.Fatalf("StartStream error: %v", err)
	}

	<-ch
	if stopErr := r.Stop(); stopErr != nil {
		t.Fatalf("Stop() error: %v", stopErr)
	}

	select {
	case _, ok := <-ch:
		if ok {
			for range ch {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after Stop()")
	}
}

func TestReceiver_New_TunedToFirstSignal(t *testing.T) {
	r := New(Options{Signals: []Signal{testCarrier(10_000), testCarrier(-5_000)}})
	if got := r.Status().CenterHz; got != testCenter+10_000 {
		t.Errorf("CenterHz=%d, want first signal %d", got, testCenter+10_000)
	}
}

func TestReceiver_New_SignalOutsideRangeLeavesUntuned(t *testing.T) {
	r := New(Options{Signals: []Signal{{Kind: SignalCarrier, FrequencyHz: 10_000_000}}})
	if got := r.Status().CenterHz; got != 0 {
		t.Errorf("CenterHz=%d, want 0 for frequency outside receiver range", got)
	}
}

func TestReceiver_StartStream_Signals(t *testing.T) {
	r := New(Options{Signals: []Signal{testCarrier(10_000)}})
	if err := r.Tune(testCenter); err != nil {
		t.Fatalf("Tune: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{SampleRate: testRate, BlockSize: 4800})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	defer func() { _ = r.Stop() }()

	frame := <-ch
	if f := meanFreq(frame.Samples); math.Abs(f-10_000) > 1 {
		t.Errorf("mean freq=%.1f Hz, want 10000", f)
	}
}

// Retuning by Δ mid-stream moves the signal by −Δ in the baseband, and the
// frames carry the new center from the first block generated after Tune.
func TestReceiver_TuneMidStreamShiftsSignal(t *testing.T) {
	const deltaHz = 4_000
	r := New(Options{Signals: []Signal{testCarrier(10_000)}})
	if err := r.Tune(testCenter); err != nil {
		t.Fatalf("Tune: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{SampleRate: testRate, BlockSize: 480})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	defer func() { _ = r.Stop() }()

	if f := meanFreq((<-ch).Samples); math.Abs(f-10_000) > 1 {
		t.Fatalf("before tune: freq=%.1f Hz, want 10000", f)
	}
	if err := r.Tune(testCenter + deltaHz); err != nil {
		t.Fatalf("Tune: %v", err)
	}
	for frame := range ch {
		if frame.CenterHz != testCenter+deltaHz {
			continue
		}
		if f := meanFreq(frame.Samples); math.Abs(f-(10_000-deltaHz)) > 1 {
			t.Errorf("after tune by %d Hz: freq=%.1f Hz, want %d", deltaHz, f, 10_000-deltaHz)
		}
		return
	}
	t.Fatal("no frame with the new center before the stream ended")
}

// streamRate collects blocks from streamGenerated for the given time and returns
// samples per second of wall-clock time from block number skip to the last block.
func streamRate(t *testing.T, window time.Duration, skip int, generate func(dst []complex64, centerHz uint64)) float64 {
	t.Helper()
	const blockSize = 480 // 10 ms at testRate
	r := New(Options{})
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()
	ch := make(chan sdr.IQFrame, 4)
	go func() {
		defer close(ch)
		r.streamGenerated(ctx, sdr.StreamConfig{SampleRate: testRate}, blockSize, ch, generate)
	}()

	var first, last time.Time
	samples, n := 0, 0
	for frame := range ch {
		now := time.Now()
		if n++; n <= skip {
			continue
		}
		if first.IsZero() {
			first = now
			continue
		}
		last = now
		samples += len(frame.Samples)
	}
	if samples == 0 {
		t.Fatal("stream produced less than two blocks")
	}
	return float64(samples) / last.Sub(first).Seconds()
}

// Generation time must not stretch the stream: blocks follow a schedule from
// the stream start, so the sample rate in real time stays fs.
func TestStreamGenerated_SlowGeneratorKeepsSampleRate(t *testing.T) {
	slow := func(dst []complex64, _ uint64) { time.Sleep(3 * time.Millisecond) }
	rate := streamRate(t, 600*time.Millisecond, 0, slow)
	if ratio := rate / testRate; ratio < 0.95 || ratio > 1.05 {
		t.Errorf("stream rate %.0f samples/s = %.2f of fs, want 1.00 ± 0.05", rate, ratio)
	}
}

// After a long stall the stream resumes at fs instead of bursting the missed
// blocks: a consumer would otherwise see time run fast right after the stall.
// The rate is measured from the stalled block on.
func TestStreamGenerated_NoBurstAfterStall(t *testing.T) {
	const stalled = 3
	calls := 0
	stallOnce := func(dst []complex64, _ uint64) {
		calls++
		if calls == stalled {
			time.Sleep(200 * time.Millisecond)
		}
	}
	rate := streamRate(t, 600*time.Millisecond, stalled-1, stallOnce)
	if ratio := rate / testRate; ratio > 1.05 {
		t.Errorf("stream rate %.0f samples/s = %.2f of fs: missed blocks came in a burst", rate, ratio)
	}
}

func TestReceiver_StartStream_NoiseOnly(t *testing.T) {
	r := New(Options{NoiseDBFS: -60, Seed: 2})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{CenterHz: testCenter, SampleRate: testRate, BlockSize: 48000})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	defer func() { _ = r.Stop() }()

	frame := <-ch
	if p := 10 * math.Log10(meanPower(frame.Samples)); math.Abs(p+60) > 0.1 {
		t.Errorf("power %.2f dBFS, want noise only at -60", p)
	}
}

// Gain scales the whole air, signals and noise alike: levels in the config are
// at 0 dB gain, so the level at the antenna stays put while dBFS follows the gain.
// Negative gain means AGC and leaves the air as configured.
func TestReceiver_GainScalesAir(t *testing.T) {
	cases := []struct {
		gainDB float32
		wantDB float64
	}{
		{gainDB: 0, wantDB: -60},
		{gainDB: 10, wantDB: -50},
		{gainDB: 25.5, wantDB: -34.5},
		{gainDB: -1, wantDB: -60},
	}
	for _, tc := range cases {
		r := New(Options{NoiseDBFS: -60, Seed: 2})
		if err := r.SetGain(tc.gainDB); err != nil {
			t.Fatalf("SetGain: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ch, err := r.StartStream(ctx, sdr.StreamConfig{CenterHz: testCenter, SampleRate: testRate, BlockSize: 48000})
		if err != nil {
			cancel()
			t.Fatalf("StartStream: %v", err)
		}
		frame := <-ch
		_ = r.Stop()
		cancel()
		if p := 10 * math.Log10(meanPower(frame.Samples)); math.Abs(p-tc.wantDB) > 0.1 {
			t.Errorf("gain %.1f dB: power %.2f dBFS, want %.1f", tc.gainDB, p, tc.wantDB)
		}
	}
}

// Changing gain mid-stream takes effect on the next generated block.
func TestReceiver_GainChangeMidStream(t *testing.T) {
	r := New(Options{NoiseDBFS: -60, Seed: 2})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{CenterHz: testCenter, SampleRate: testRate, BlockSize: 4800})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	defer func() { _ = r.Stop() }()

	<-ch
	if err := r.SetGain(20); err != nil {
		t.Fatalf("SetGain: %v", err)
	}
	for frame := range ch {
		if p := 10 * math.Log10(meanPower(frame.Samples)); math.Abs(p+40) < 0.5 {
			return
		}
	}
	t.Fatal("no block at -40 dBFS after raising gain by 20 dB")
}

func TestReceiver_StartStream_InvalidSignal(t *testing.T) {
	r := New(Options{Signals: []Signal{{Kind: "noise", FrequencyHz: testCenter}}})
	_, err := r.StartStream(context.Background(), sdr.StreamConfig{SampleRate: testRate})
	if !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("err=%v, want ErrInvalidSignal", err)
	}
	if r.Status().Running {
		t.Error("receiver must not be running after failed start")
	}
}

// The default channel (offset 0, AFSK 1200) receives the first beacon without any setup.
func TestDefaultOptions_AFSKBeaconAtCenter(t *testing.T) {
	opts := DefaultOptions()
	if len(opts.Signals) == 0 {
		t.Fatal("no signals, want the AFSK 1200 beacon first")
	}
	sig := opts.Signals[0]
	if sig.Kind != SignalBeacon || sig.Modem != BeaconAFSK1200 || sig.FrequencyHz != DefaultFrequencyHz {
		t.Errorf("first signal %s %s at %d Hz, want AFSK 1200 beacon at %d",
			sig.Kind, sig.Modem, sig.FrequencyHz, uint64(DefaultFrequencyHz))
	}
	if opts.NoiseDBFS >= sig.LevelDBFS || opts.NoiseDBFS > -20 {
		t.Errorf("noise %.0f dBFS, beacon %.0f dBFS: want noise well below full scale and the beacon",
			opts.NoiseDBFS, sig.LevelDBFS)
	}
	if got := New(opts).Status().CenterHz; got != DefaultFrequencyHz {
		t.Errorf("CenterHz=%d, want %d", got, uint64(DefaultFrequencyHz))
	}
}

func TestDefaultOptions_BeaconsOfEveryFormat(t *testing.T) {
	opts := DefaultOptions()
	// The narrowest receiver band is a quarter of the recommended rate on each side of the center.
	const halfNarrowBand = RecommendedSampleRate / 8 / 2
	// Channel noise at the recommended rate in 30 kHz, the widest channel of 9600 Bd.
	noise := opts.NoiseDBFS + 10*math.Log10(30_000.0/RecommendedSampleRate)

	modems := make(map[BeaconModem]bool)
	var freqs []uint64
	for _, s := range opts.Signals {
		if s.Kind != SignalBeacon {
			t.Errorf("signal %s at %d Hz, want only beacons", s.Kind, s.FrequencyHz)
			continue
		}
		modems[s.Modem] = true
		if d := math.Abs(float64(s.FrequencyHz) - DefaultFrequencyHz); d > halfNarrowBand-15_000 {
			t.Errorf("%s at %d Hz: %.0f Hz from the center, not inside the narrowest band", s.Modem, s.FrequencyHz, d)
		}
		if s.LevelDBFS-noise < 20 {
			t.Errorf("%s: SNR %.0f dB in 30 kHz, want at least 20", s.Modem, s.LevelDBFS-noise)
		}
		if s.Period != DefaultBeaconPeriod || s.Frame.Src.Call == "" {
			t.Errorf("%s: period %v frame %+v, want the default beacon", s.Modem, s.Period, s.Frame)
		}
		for _, f := range freqs {
			if math.Abs(float64(s.FrequencyHz)-float64(f)) < 30_000 {
				t.Errorf("%s at %d Hz overlaps a beacon at %d Hz", s.Modem, s.FrequencyHz, f)
			}
		}
		freqs = append(freqs, s.FrequencyHz)
	}
	for _, m := range []BeaconModem{BeaconAFSK1200, BeaconFSK9600G3RUH, BeaconFSK9600GEOSCAN} {
		if !modems[m] {
			t.Errorf("no default beacon for %s", m)
		}
	}
}

func TestReceiver_StartStream_ZeroSampleRateRejected(t *testing.T) {
	r := New(DefaultOptions())
	_, err := r.StartStream(context.Background(), sdr.StreamConfig{BlockSize: 1024})
	if !errors.Is(err, sdr.ErrSampleRateRequired) {
		t.Fatalf("err=%v, want ErrSampleRateRequired", err)
	}
	if r.Status().Running {
		t.Error("receiver must not be running after failed start")
	}
}

func TestReceiver_Capabilities_RecommendedRateSupported(t *testing.T) {
	caps := New(DefaultOptions()).Capabilities()
	if caps.RecommendedSampleRate == 0 {
		t.Fatal("RecommendedSampleRate is zero")
	}
	if _, err := sdr.ResolveSampleRate(caps, caps.RecommendedSampleRate); err != nil {
		t.Errorf("recommended rate outside own ranges: %v", err)
	}
}

func TestReceiver_StartStream_RecommendedRate(t *testing.T) {
	r := New(DefaultOptions())
	rate := r.Capabilities().RecommendedSampleRate
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{SampleRate: rate, BlockSize: 1024})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	defer func() { _ = r.Stop() }()

	frame := <-ch
	if frame.SampleRate != rate {
		t.Errorf("SampleRate=%d, want %d", frame.SampleRate, rate)
	}
	for i, v := range frame.Samples {
		if cmplx.IsNaN(complex128(v)) || cmplx.IsInf(complex128(v)) {
			t.Fatalf("sample %d is not finite: %v", i, v)
		}
	}
}

func TestReceiver_ContextCancel(t *testing.T) {
	r := New(Options{ToneOffsetHz: 1000})

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := r.StartStream(ctx, sdr.StreamConfig{
		CenterHz:   145_900_000,
		SampleRate: 48000,
		BlockSize:  256,
	})
	if err != nil {
		t.Fatalf("StartStream error: %v", err)
	}

	<-ch
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			for range ch {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after context cancel")
	}
}
