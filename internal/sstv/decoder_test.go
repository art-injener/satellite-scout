package sstv

import (
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"
)

const decodeRate = 12_000

// smoothImage — slow colour waves: no edges sharper than the SSTV audio band can carry.
func smoothImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(128 + 100*math.Sin(2*math.Pi*fx)),
				G: uint8(128 + 100*math.Sin(2*math.Pi*fy)),
				B: uint8(128 + 100*math.Cos(2*math.Pi*(fx+fy)/2)),
				A: 255,
			})
		}
	}
	return img
}

// psnr between two images of the same size over R, G and B.
func psnr(a, b image.Image) float64 {
	var se float64
	n := 0
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r1, g1, b1, _ := a.At(x, y).RGBA()
			r2, g2, b2, _ := b.At(x, y).RGBA()
			for _, d := range []float64{
				float64(r1>>8) - float64(r2>>8), float64(g1>>8) - float64(g2>>8), float64(b1>>8) - float64(b2>>8),
			} {
				se += d * d
				n++
			}
		}
	}
	if se == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(se/float64(n)))
}

// sstvAudio — the image in mode m at the given rate, with a second of silence around it.
func sstvAudio(img image.Image, m Mode, rate float64) []float32 {
	pad := make([]float32, int(rate))
	out := append([]float32(nil), pad...)
	e := NewEncoder(img, m, rate)
	buf := make([]float32, 4096)
	for {
		n := e.Read(buf)
		out = append(out, buf[:n]...)
		if n < len(buf) {
			break
		}
	}
	return append(out, pad...)
}

// decodeAll pushes audio in uneven blocks and returns the events.
func decodeAll(audio []float32) []Event {
	d := NewDecoder(decodeRate)
	var events []Event
	for i, step := 0, 1000; i < len(audio); i, step = i+step, 700+(step*7)%900 {
		events = append(events, d.Push(audio[i:min(i+step, len(audio))])...)
	}
	return events
}

func doneImages(events []Event) []Event {
	var out []Event
	for _, e := range events {
		if e.Kind == EventDone {
			out = append(out, e)
		}
	}
	return out
}

// reference — the source image at the mode size, as the encoder sees it.
func reference(img image.Image, m Mode) *image.RGBA {
	y, cb, cr := planes(img, m.Width, m.Height)
	out := image.NewRGBA(image.Rect(0, 0, m.Width, m.Height))
	for i := range y {
		r, g, b := YCbCr{Y: y[i], Cb: cb[i], Cr: cr[i]}.RGB()
		out.SetRGBA(i%m.Width, i/m.Width, color.RGBA{r, g, b, 255})
	}
	return out
}

func TestDecoder_RoundTripEveryMode(t *testing.T) {
	for _, m := range Modes {
		t.Run(m.ID, func(t *testing.T) {
			src := smoothImage(m.Width, m.Height)
			events := decodeAll(sstvAudio(src, m, decodeRate))
			if len(events) == 0 || events[0].Kind != EventStart || events[0].Mode.ID != m.ID {
				t.Fatalf("first event %+v, want start of %s", events, m.Name)
			}
			done := doneImages(events)
			if len(done) != 1 {
				t.Fatalf("done events=%d, want 1", len(done))
			}
			// The audio band blurs a couple of pixels at the ends of each scan, the rest is within ±2.
			if p := psnr(done[0].Image, reference(src, m)); p < 27 {
				t.Errorf("PSNR %.1f dB, want ≥ 27", p)
			}
		})
	}
}

func TestDecoder_ReportsLinesAsTheyArrive(t *testing.T) {
	events := decodeAll(sstvAudio(smoothImage(320, 240), Robot36, decodeRate))
	last := 0
	for _, e := range events {
		if e.Kind != EventLines {
			continue
		}
		if e.Lines <= last {
			t.Fatalf("lines went %d to %d", last, e.Lines)
		}
		last = e.Lines
	}
	if last < 200 {
		t.Errorf("last progress at %d lines, want most of 240", last)
	}
}

func TestDecoder_CorrectsSlantFromClockSkew(t *testing.T) {
	// The transmitter clock runs 0.3 % fast: 0.4 s drift over a PD120 picture.
	src := smoothImage(640, 496)
	events := decodeAll(sstvAudio(src, PD120, decodeRate*1.003))
	done := doneImages(events)
	if len(done) != 1 {
		t.Fatalf("done events=%d, want 1", len(done))
	}
	if p := psnr(done[0].Image, reference(src, PD120)); p < 30 {
		t.Errorf("PSNR %.1f dB with clock skew, want ≥ 30", p)
	}
}

func TestDecoder_NoiseGivesNoPicture(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	noise := make([]float32, 30*decodeRate)
	for i := range noise {
		noise[i] = float32(r.NormFloat64())
	}
	for _, e := range decodeAll(noise) {
		if e.Kind == EventStart {
			t.Fatalf("start of %s on noise", e.Mode.Name)
		}
	}
}

func TestDecoder_BackToBackPictures(t *testing.T) {
	a := sstvAudio(smoothImage(320, 240), Robot36, decodeRate)
	b := sstvAudio(TestPattern(320, 240), Robot36, decodeRate)
	done := doneImages(decodeAll(append(a, b...)))
	if len(done) != 2 {
		t.Fatalf("done events=%d, want 2", len(done))
	}
}
