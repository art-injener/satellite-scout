package sstv

import (
	"image"
	"image/color"
	"math"
	"testing"
)

const testAudioRate = 48_000

// toneAt estimates the frequency of the audio between t0 and t1 seconds by zero crossings.
func toneAt(audio []float32, rate, t0, t1 float64) float64 {
	from, to := int(t0*rate), min(int(t1*rate), len(audio))
	first, last, n := -1.0, -1.0, 0
	for i := from + 1; i < to; i++ {
		if audio[i-1] < 0 && audio[i] >= 0 {
			t := float64(i-1) + float64(-audio[i-1]/(audio[i]-audio[i-1]))
			if first < 0 {
				first = t
			}
			last = t
			n++
		}
	}
	if n < 2 {
		return 0
	}
	return float64(n-1) * rate / (last - first)
}

func encodeAll(t *testing.T, img image.Image, m Mode) []float32 {
	t.Helper()
	e := NewEncoder(img, m, testAudioRate)
	var out []float32
	buf := make([]float32, 4096)
	for {
		n := e.Read(buf)
		out = append(out, buf[:n]...)
		if n < len(buf) {
			return out
		}
	}
}

func uniform(c color.Color, w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestModes_DurationsAndVIS(t *testing.T) {
	for _, tc := range []struct {
		mode    Mode
		vis     byte
		w, h    int
		seconds float64
	}{
		// VIS header 910 ms plus the scan lines.
		{Robot36, 8, 320, 240, 0.910 + 240*0.150},
		{PD120, 95, 640, 496, 0.910 + 248*0.50848},
		{PD180, 96, 640, 496, 0.910 + 248*0.75424},
	} {
		m := tc.mode
		if m.VIS != tc.vis || m.Width != tc.w || m.Height != tc.h {
			t.Errorf("%s: VIS %d %dx%d, want %d %dx%d", m.Name, m.VIS, m.Width, m.Height, tc.vis, tc.w, tc.h)
		}
		if d := m.Duration().Seconds(); math.Abs(d-tc.seconds) > 1e-6 {
			t.Errorf("%s: duration %.6f s, want %.6f", m.Name, d, tc.seconds)
		}
	}
}

func TestModeByVIS(t *testing.T) {
	for _, m := range Modes {
		got, ok := ModeByVIS(m.VIS)
		if !ok || got.Name != m.Name {
			t.Errorf("VIS %d: %v %v, want %s", m.VIS, got.Name, ok, m.Name)
		}
	}
	if _, ok := ModeByVIS(0); ok {
		t.Error("VIS 0 found, want unknown")
	}
}

func TestEncoder_VISHeader(t *testing.T) {
	audio := encodeAll(t, uniform(color.Gray{128}, 320, 240), Robot36)
	want := []struct {
		t0, t1, hz float64
	}{
		{0.010, 0.290, 1900}, // leader
		{0.301, 0.309, 1200}, // break
		{0.320, 0.600, 1900}, // leader
		{0.612, 0.638, 1200}, // start bit
	}
	// Robot 36 is VIS 8: bits LSB first 0001000, even parity 1, then the stop bit.
	for i, bit := range []int{0, 0, 0, 1, 0, 0, 0, 1} {
		hz := 1300.0
		if bit == 1 {
			hz = 1100
		}
		t0 := 0.640 + 0.030*float64(i)
		want = append(want, struct{ t0, t1, hz float64 }{t0 + 0.003, t0 + 0.027, hz})
	}
	want = append(want, struct{ t0, t1, hz float64 }{0.883, 0.907, 1200})
	for _, w := range want {
		if got := toneAt(audio, testAudioRate, w.t0, w.t1); math.Abs(got-w.hz) > 15 {
			t.Errorf("%.3f–%.3f s: %.0f Hz, want %.0f", w.t0, w.t1, got, w.hz)
		}
	}
}

func TestEncoder_Robot36LineStructure(t *testing.T) {
	audio := encodeAll(t, uniform(color.Gray{128}, 320, 240), Robot36)
	if want := Robot36.Duration().Seconds() * testAudioRate; math.Abs(float64(len(audio))-want) > 2 {
		t.Fatalf("samples=%d, want %.0f", len(audio), want)
	}
	gray := PixelHz(YCbCrFromRGB(128, 128, 128).Y)
	for line, sep := range []float64{1500, 2300} {
		l := 0.910 + 0.150*float64(line)
		checks := []struct {
			name   string
			t0, t1 float64
			hz     float64
		}{
			{"sync", l + 0.001, l + 0.008, SyncHz},
			{"Y", l + 0.014, l + 0.098, gray},
			{"separator", l + 0.1005, l + 0.1035, sep},
			{"chroma", l + 0.107, l + 0.148, PixelHz(128)},
		}
		for _, c := range checks {
			if got := toneAt(audio, testAudioRate, c.t0, c.t1); math.Abs(got-c.hz) > 20 {
				t.Errorf("line %d %s: %.0f Hz, want %.0f", line, c.name, got, c.hz)
			}
		}
	}
}

func TestEncoder_PDScansYCrCbY(t *testing.T) {
	// Top line red, second line blue: Y differs per line, chroma is their average.
	img := image.NewRGBA(image.Rect(0, 0, 640, 496))
	for x := range 640 {
		img.Set(x, 0, color.RGBA{255, 0, 0, 255})
		img.Set(x, 1, color.RGBA{0, 0, 255, 255})
	}
	audio := encodeAll(t, img, PD120)
	red, blue := YCbCrFromRGB(255, 0, 0), YCbCrFromRGB(0, 0, 255)
	scan := 0.1216
	start := 0.910 + 0.020 + 0.00208
	for i, hz := range []float64{
		PixelHz(red.Y),
		PixelHz(uint8((int(red.Cr) + int(blue.Cr) + 1) / 2)),
		PixelHz(uint8((int(red.Cb) + int(blue.Cb) + 1) / 2)),
		PixelHz(blue.Y),
	} {
		t0 := start + scan*float64(i)
		if got := toneAt(audio, testAudioRate, t0+0.005, t0+scan-0.005); math.Abs(got-hz) > 10 {
			t.Errorf("scan %d: %.0f Hz, want %.0f", i, got, hz)
		}
	}
}

func TestPixelHzAndColorRoundTrip(t *testing.T) {
	if PixelHz(0) != BlackHz || PixelHz(255) != WhiteHz {
		t.Errorf("pixel 0/255: %.0f/%.0f Hz, want %d/%d", PixelHz(0), PixelHz(255), BlackHz, WhiteHz)
	}
	for _, c := range [][3]uint8{{0, 0, 0}, {255, 255, 255}, {255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {90, 160, 30}} {
		r, g, b := YCbCrFromRGB(c[0], c[1], c[2]).RGB()
		for i, v := range []uint8{r, g, b} {
			if d := int(v) - int(c[i]); d < -2 || d > 2 {
				t.Errorf("rgb %v: back %v", c, [3]uint8{r, g, b})
				break
			}
		}
	}
}

func TestEncoder_ScalesImageToMode(t *testing.T) {
	e := NewEncoder(uniform(color.White, 100, 50), Robot36, testAudioRate)
	if len(e.y) != 320*240 {
		t.Fatalf("Y plane %d pixels, want 320x240", len(e.y))
	}
}

func TestModeByID(t *testing.T) {
	for _, id := range []string{"robot36", "pd120", "pd180"} {
		if m, ok := ModeByID(id); !ok || m.ID != id {
			t.Errorf("%s: %+v %v", id, m.ID, ok)
		}
	}
	if _, ok := ModeByID("martin1"); ok {
		t.Error("martin1 found, want unknown")
	}
}

func TestTestPattern_HasBarsAndGradient(t *testing.T) {
	img := TestPattern(320, 240)
	if b := img.Bounds(); b.Dx() != 320 || b.Dy() != 240 {
		t.Fatalf("size %v, want 320x240", b)
	}
	// Top-left bar is white, the last bar is blue; the bottom row goes from black to white.
	if r, g, b, _ := img.At(5, 5).RGBA(); r>>8 != 255 || g>>8 != 255 || b>>8 != 255 {
		t.Errorf("first bar %d,%d,%d, want white", r>>8, g>>8, b>>8)
	}
	if r, g, b, _ := img.At(315, 5).RGBA(); r>>8 != 0 || g>>8 != 0 || b>>8 != 255 {
		t.Errorf("last bar %d,%d,%d, want blue", r>>8, g>>8, b>>8)
	}
	l, _, _, _ := img.At(0, 235).RGBA()
	r, _, _, _ := img.At(319, 235).RGBA()
	if l>>8 > 5 || r>>8 < 250 {
		t.Errorf("gradient ends %d and %d, want black and white", l>>8, r>>8)
	}
}
