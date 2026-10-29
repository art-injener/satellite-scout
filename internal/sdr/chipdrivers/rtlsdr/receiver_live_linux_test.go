//go:build linux

package rtlsdr

import (
	"context"
	"math"
	"os"
	"slices"
	"testing"
	"time"

	"gonum.org/v1/gonum/stat"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv("SCOUT_USB_LIVE") != "1" {
		t.Skip("set SCOUT_USB_LIVE=1 with an RTL-SDR plugged in")
	}
}

// averageSpectrumDB tunes to centerHz and returns the averaged power spectrum in dB, DC in the middle.
func averageSpectrumDB(t *testing.T, r *Receiver, centerHz uint64, rate uint32, size int) []float64 {
	t.Helper()
	if err := r.Tune(centerHz); err != nil {
		t.Fatal(err)
	}
	ch, err := r.StartStream(context.Background(), sdr.StreamConfig{SampleRate: rate, BlockSize: size})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Stop() }()
	spec := dsp.NewSpectrum(size)
	acc := make([]float64, size)
	var blocks int
	start := time.Now()
	for time.Since(start) < 600*time.Millisecond {
		f := <-ch
		if time.Since(start) < 100*time.Millisecond || len(f.Samples) < size {
			continue
		}
		for i, p := range spec.PowerSpectrum(f.Samples[:size]) {
			acc[i] += p
		}
		blocks++
	}
	for i := range acc {
		acc[i] = 10 * math.Log10(acc[i]/float64(blocks)+1e-20)
	}
	return acc
}

// TestLive_SpectrumIsNotMirrored tunes two centers 300 kHz apart in the FM broadcast band: the
// stations must line up in absolute frequency, not as a mirror image.
func TestLive_SpectrumIsNotMirrored(t *testing.T) {
	skipUnlessLive(t)
	const (
		rate  = 2_048_000
		size  = 4096
		fa    = 100_000_000
		shift = 300_000
	)
	r := New(Options{Serial: os.Getenv("SCOUT_RTL_SERIAL")})
	a := averageSpectrumDB(t, r, fa, rate, size)
	b := averageSpectrumDB(t, r, fa+shift, rate, size)

	sorted := append([]float64(nil), a...)
	slices.Sort(sorted)
	if peak := sorted[len(sorted)-1] - sorted[len(sorted)/2]; peak < 15 {
		t.Skipf("no strong FM station (peak %.1f dB over median): connect an antenna", peak)
	}

	df := float64(rate) / size
	corr := func(sign float64) float64 {
		var xs, ys []float64
		for i := size / 8; i < size*7/8; i++ {
			off := sign * float64(i-size/2) * df
			abs := fa + off
			j := size/2 + int(math.Round(sign*(abs-fa-shift)/df))
			if j < size/8 || j >= size*7/8 || math.Abs(off) < 30e3 || math.Abs(abs-fa-shift) < 30e3 {
				continue
			}
			xs, ys = append(xs, a[i]), append(ys, b[j])
		}
		return stat.Correlation(xs, ys, nil)
	}
	straight, mirrored := corr(1), corr(-1)
	t.Logf("correlation straight %.2f, mirrored %.2f", straight, mirrored)
	if straight <= mirrored || straight < 0.5 {
		t.Errorf("spectrum looks mirrored: straight %.2f, mirrored %.2f", straight, mirrored)
	}
}

// Live check on a plugged-in RTL-SDR: SCOUT_USB_LIVE=1 go test -run Live ./internal/sdr/chipdrivers/rtlsdr/
func TestLive_StreamDeliversSampleRate(t *testing.T) {
	skipUnlessLive(t)
	r := New(Options{Serial: os.Getenv("SCOUT_RTL_SERIAL")})
	const rate = 2_048_000
	for _, freq := range []uint64{436_500_000, 100_000_000} {
		if err := r.Tune(freq); err != nil {
			t.Fatal(err)
		}
		ch, err := r.StartStream(context.Background(), sdr.StreamConfig{SampleRate: rate})
		if err != nil {
			t.Fatal(err)
		}
		// The first buffers carry the FIFO left from before the reset.
		var n int
		var sum complex128
		var power float64
		var lo, hi float32 = 1, -1
		start := time.Now()
		for time.Since(start) < time.Second {
			f := <-ch
			if time.Since(start) < 100*time.Millisecond {
				continue
			}
			for _, s := range f.Samples {
				sum += complex128(s)
				power += float64(real(s)*real(s) + imag(s)*imag(s))
				lo, hi = min(lo, real(s)), max(hi, real(s))
			}
			n += len(f.Samples)
		}
		elapsed := time.Since(start) - 100*time.Millisecond
		if err := r.Stop(); err != nil {
			t.Fatal(err)
		}
		got := float64(n) / elapsed.Seconds()
		mean := sum / complex(float64(n), 0)
		t.Logf("%d Hz: %.0f S/s, I range %.3f..%.3f, mean %.4f, power %.1f dBFS",
			freq, got, lo, hi, mean, 10*math.Log10(power/float64(n)))
		if got < 0.9*rate || got > 1.1*rate {
			t.Errorf("%d Hz: %.0f S/s, want about %d", freq, got, rate)
		}
		if hi-lo < 0.02 {
			t.Errorf("%d Hz: samples barely move (%.3f..%.3f): no signal from the ADC", freq, lo, hi)
		}
	}
}
