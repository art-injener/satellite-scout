package signalclass

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/wav"
)

// Off-air PD120 wav (data/recordings/sstv): mid-scan must not be a bare carrier/unknown.
func TestClassify_PD120RecordingFile(t *testing.T) {
	path := filepath.Join("..", "..", "data", "recordings", "sstv", "pd120_ariss20_image1.wav")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("no recording: %v", err)
	}
	defer f.Close()
	audio, err := wav.Read(bufio.NewReader(f))
	if err != nil {
		t.Fatal(err)
	}
	const rate = 48_000.0
	srcRate := float64(audio.SampleRate)
	start := int(10 * srcRate)
	n := int(1.2 * rate)
	step := srcRate / rate
	if start+int(1.2*srcRate) > len(audio.Samples) {
		t.Fatal("recording shorter than 11.2 s")
	}
	a := make([]float32, n)
	var peak float32
	for i := range a {
		a[i] = audio.Samples[start+int(float64(i)*step)]
		peak = max(peak, a[i], -a[i])
	}
	if peak == 0 {
		t.Fatal("silent slice")
	}
	for i := range a {
		a[i] /= peak
	}
	iq := modem.NewFM(modem.FMConfig{SampleRate: rate, DeviationHz: 5000}).Modulate(nil, a)
	// As after Extract at ~25 kS/s: same audio, lower rate.
	res := Classify(iq, rate)
	if res.Service != sstvProbable && res.Service != sstvService {
		t.Fatalf("48 kS/s: %+v, want SSTV service", res)
	}
	iq25 := modem.NewFM(modem.FMConfig{SampleRate: 25_000, DeviationHz: 5000}).Modulate(nil, resample(a, rate, 25_000))
	res25 := Classify(iq25, 25_000)
	if res25.Service != sstvProbable && res25.Service != sstvService {
		t.Fatalf("25 kS/s (extract-like): %+v, want SSTV service", res25)
	}
}

func resample(x []float32, from, to float64) []float32 {
	n := int(float64(len(x)) * to / from)
	out := make([]float32, n)
	step := from / to
	for i := range out {
		out[i] = x[min(int(float64(i)*step), len(x)-1)]
	}
	return out
}
