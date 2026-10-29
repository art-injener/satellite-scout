package sstv

import (
	"bufio"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/art-injener/satellite-scout/internal/wav"
)

// Off-air recordings with pictures decoded by another program (data/recordings/SOURCES.txt).
// They are not in git: the test is skipped without them. The other program's Robot 36
// picture is slanted and torn, so that one is only checked to decode in full.
var recordings = []struct {
	wav, expected, mode string
	minPSNR             float64
}{
	{"robot36_pt7apm.wav", "", "robot36", 0},
	{"pd120_ariss20_image1.wav", "pd120_ariss20_image1.expected.png", "pd120", 22},
	{"pd180_gagarin_image5.wav", "pd180_gagarin_image5.expected.jpg", "pd180", 18},
}

const recordingsDir = "../../data/recordings/sstv"

func readImage(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestDecoder_RealRecordings(t *testing.T) {
	for _, rec := range recordings {
		t.Run(rec.mode, func(t *testing.T) {
			path := filepath.Join(recordingsDir, rec.wav)
			f, err := os.Open(path)
			if err != nil {
				t.Skipf("no recording: %v", err)
			}
			defer f.Close()
			audio, err := wav.Read(bufio.NewReader(f))
			if err != nil {
				t.Fatal(err)
			}
			d := NewDecoder(float64(audio.SampleRate))
			var events []Event
			for i := 0; i < len(audio.Samples); i += 4096 {
				events = append(events, d.Push(audio.Samples[i:min(i+4096, len(audio.Samples))])...)
			}
			done := doneImages(events)
			if len(done) != 1 {
				t.Fatalf("pictures=%d, want 1", len(done))
			}
			if done[0].Mode.ID != rec.mode {
				t.Errorf("mode %q, want %q", done[0].Mode.ID, rec.mode)
			}
			if rec.expected == "" {
				return
			}
			p := psnr(done[0].Image, readImage(t, filepath.Join(recordingsDir, rec.expected)))
			t.Logf("%s: PSNR %.1f dB to the other decoder", rec.wav, p)
			if p < rec.minPSNR {
				t.Errorf("PSNR %.1f dB, want ≥ %.0f", p, rec.minPSNR)
			}
		})
	}
}
