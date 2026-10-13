package imitator

import (
	"context"
	"errors"
	"math"
	"math/cmplx"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	beaconTestRate   = 48000.0
	beaconTestCenter = 435_000_000
)

func testBeaconConfig() BeaconConfig {
	return BeaconConfig{
		FrequencyHz: beaconTestCenter + 10_000,
		Frame: ax25.Frame{
			Dest: ax25.Address{Call: "CQ"},
			Src:  ax25.Address{Call: "RS0SIM"},
			Info: []byte("test beacon"),
		},
		Period: time.Second,
	}
}

func newTestBeacon(t *testing.T, cfg BeaconConfig) *beaconSource {
	t.Helper()
	b, err := newBeaconSource(cfg, beaconTestRate)
	if err != nil {
		t.Fatalf("newBeaconSource: %v", err)
	}
	return b
}

// meanFreq — средняя мгновенная частота (Гц): для FM с AFSK равна сдвигу несущей.
func meanFreq(x []complex64) float64 {
	sum := 0.0
	for i := 1; i < len(x); i++ {
		sum += cmplx.Phase(complex128(x[i]) * cmplx.Conj(complex128(x[i-1])))
	}
	return sum / float64(len(x)-1) * beaconTestRate / (2 * math.Pi)
}

func isSilent(x []complex64) bool {
	for _, v := range x {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestBeacon_BurstThenSilenceThenBurst(t *testing.T) {
	b := newTestBeacon(t, testBeaconConfig())
	burst := b.burstSamples()
	if burst <= 0 || burst >= int(beaconTestRate) {
		t.Fatalf("burst=%d samples, want within (0, period)", burst)
	}

	x := make([]complex64, int(beaconTestRate)+burst/2)
	b.fill(x, beaconTestCenter)

	for i, v := range x[:burst] {
		if mag := cmplx.Abs(complex128(v)); math.Abs(mag-1) > 1e-4 {
			t.Fatalf("burst sample %d: |x|=%g, want 1", i, mag)
		}
	}
	if !isSilent(x[burst:int(beaconTestRate)]) {
		t.Error("expected silence between bursts")
	}
	if isSilent(x[int(beaconTestRate):]) {
		t.Error("expected next burst after period")
	}
}

func TestBeacon_SignalAtOffsetFromCenter(t *testing.T) {
	b := newTestBeacon(t, testBeaconConfig())
	x := make([]complex64, 4800)
	b.fill(x, beaconTestCenter)
	if f := meanFreq(x); math.Abs(f-10_000) > 50 {
		t.Errorf("mean freq=%.1f Hz, want 10000", f)
	}
}

func TestBeacon_RetuneMovesSignal(t *testing.T) {
	b := newTestBeacon(t, testBeaconConfig())
	x := make([]complex64, 4800)
	b.fill(x, beaconTestCenter)
	b.fill(x, beaconTestCenter+5000)
	if f := meanFreq(x); math.Abs(f-5000) > 50 {
		t.Errorf("mean freq after retune=%.1f Hz, want 5000", f)
	}
}

func TestBeacon_DopplerAddsToOffset(t *testing.T) {
	cfg := testBeaconConfig()
	cfg.DopplerHz = func(time.Duration) float64 { return -3000 }
	b := newTestBeacon(t, cfg)
	x := make([]complex64, 4800)
	b.fill(x, beaconTestCenter)
	if f := meanFreq(x); math.Abs(f-7000) > 50 {
		t.Errorf("mean freq=%.1f Hz, want 7000 (10 kHz offset - 3 kHz Doppler)", f)
	}
}

func TestBeacon_OutsideBandIsSilent(t *testing.T) {
	cfg := testBeaconConfig()
	cfg.FrequencyHz = beaconTestCenter + 30_000 // дальше половины полосы 48 кГц
	b := newTestBeacon(t, cfg)
	x := make([]complex64, 4800)
	b.fill(x, beaconTestCenter)
	if !isSilent(x) {
		t.Error("signal outside receiver band must not appear")
	}
}

func TestBeacon_NoiseFillsSilence(t *testing.T) {
	cfg := testBeaconConfig()
	cfg.SNRdB = 10
	cfg.NoiseBandwidthHz = 12_500
	b := newTestBeacon(t, cfg)
	x := make([]complex64, int(beaconTestRate))
	b.fill(x, beaconTestCenter)
	if isSilent(x[b.burstSamples():]) {
		t.Error("expected noise between bursts")
	}
}

func meanPower(x []complex64) float64 {
	sum := 0.0
	for _, v := range x {
		re, im := float64(real(v)), float64(imag(v))
		sum += re*re + im*im
	}
	return sum / float64(len(x))
}

func TestBeacon_LevelScalesSignal(t *testing.T) {
	cfg := testBeaconConfig()
	cfg.LevelDBFS = -20
	b := newTestBeacon(t, cfg)
	x := make([]complex64, b.burstSamples())
	b.fill(x, beaconTestCenter)
	for i, v := range x {
		if mag := cmplx.Abs(complex128(v)); math.Abs(mag-0.1) > 1e-5 {
			t.Fatalf("burst sample %d: |x|=%g, want 0.1 at -20 dBFS", i, mag)
		}
	}
}

func TestBeacon_LevelKeepsSNR(t *testing.T) {
	// signal and noise power in the burst and in the silence after it
	noiseAt := func(level float64) (float64, float64) {
		cfg := testBeaconConfig()
		cfg.SNRdB = 10
		cfg.NoiseBandwidthHz = 12_500
		cfg.Seed = 7
		cfg.LevelDBFS = level
		b := newTestBeacon(t, cfg)
		x := make([]complex64, int(beaconTestRate))
		b.fill(x, beaconTestCenter)
		burst := b.burstSamples()
		return meanPower(x[:burst]), meanPower(x[burst:])
	}
	sig0, noise0 := noiseAt(0)
	sig40, noise40 := noiseAt(-40)
	if r := 10 * math.Log10(noise40/noise0); math.Abs(r+40) > 0.01 {
		t.Errorf("noise moved by %.2f dB, want -40", r)
	}
	if snr0, snr40 := sig0/noise0, sig40/noise40; math.Abs(snr40/snr0-1) > 1e-6 {
		t.Errorf("SNR changed: %.4f vs %.4f", snr40, snr0)
	}
}

func TestDefaultBeaconOptions_NoiseWellBelowFullScale(t *testing.T) {
	opts := DefaultBeaconOptions()
	rate := float64(New(opts).Capabilities().RecommendedSampleRate)
	b, err := newBeaconSource(*opts.Beacon, rate)
	if err != nil {
		t.Fatalf("newBeaconSource: %v", err)
	}
	x := make([]complex64, b.burstSamples()+65536)
	b.fill(x, DefaultBeaconHz)
	if p := 10 * math.Log10(meanPower(x[b.burstSamples():])); p > -20 {
		t.Errorf("total noise %.1f dBFS, want below -20 dBFS like a real receiver", p)
	}
}

func TestDefaultBeaconOptions_Valid(t *testing.T) {
	opts := DefaultBeaconOptions()
	if opts.Beacon == nil {
		t.Fatal("Beacon is nil")
	}
	rate := float64(New(opts).Capabilities().RecommendedSampleRate)
	b, err := newBeaconSource(*opts.Beacon, rate)
	if err != nil {
		t.Fatalf("newBeaconSource: %v", err)
	}
	if burst := float64(b.burstSamples()) / rate; burst >= opts.Beacon.Period.Seconds() {
		t.Errorf("burst %.2f s does not fit into period %v", burst, opts.Beacon.Period)
	}
}

// Одна секунда сигнала при рекомендуемой частоте должна генерироваться заметно быстрее секунды.
func BenchmarkBeacon_FillOneSecond(b *testing.B) {
	opts := DefaultBeaconOptions()
	rate := New(opts).Capabilities().RecommendedSampleRate
	src, err := newBeaconSource(*opts.Beacon, float64(rate))
	if err != nil {
		b.Fatal(err)
	}
	block := make([]complex64, 16384)
	blocks := int(rate) / len(block)
	for b.Loop() {
		for range blocks {
			src.fill(block, DefaultBeaconHz)
		}
	}
}

func TestBeacon_InvalidFrame(t *testing.T) {
	cfg := testBeaconConfig()
	cfg.Frame.Src.Call = ""
	if _, err := newBeaconSource(cfg, beaconTestRate); !errors.Is(err, ax25.ErrInvalidCallsign) {
		t.Errorf("err=%v, want ErrInvalidCallsign", err)
	}
}

func TestReceiver_StartStream_Beacon(t *testing.T) {
	cfg := testBeaconConfig()
	r := New(Options{Beacon: &cfg})
	if err := r.Tune(beaconTestCenter); err != nil {
		t.Fatalf("Tune: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{SampleRate: beaconTestRate, BlockSize: 4800})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	defer func() { _ = r.Stop() }()

	frame := <-ch
	if f := meanFreq(frame.Samples); math.Abs(f-10_000) > 50 {
		t.Errorf("mean freq=%.1f Hz, want 10000", f)
	}
}

func TestReceiver_StartStream_InvalidBeacon(t *testing.T) {
	cfg := testBeaconConfig()
	cfg.Frame.Dest.SSID = 16
	r := New(Options{Beacon: &cfg})
	_, err := r.StartStream(context.Background(), sdr.StreamConfig{SampleRate: beaconTestRate})
	if !errors.Is(err, ax25.ErrInvalidSSID) {
		t.Fatalf("err=%v, want ErrInvalidSSID", err)
	}
	if r.Status().Running {
		t.Error("receiver must not be running after failed start")
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
	r := New(DefaultBeaconOptions())
	if err := r.Tune(DefaultBeaconHz); err != nil {
		t.Fatalf("Tune: %v", err)
	}
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
