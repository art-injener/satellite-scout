package products

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func picture(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func decodePNG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return img
}

func TestStore_PictureLifecycle(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.now = fixedClock(time.Date(2026, 10, 27, 15, 30, 12, 0, time.UTC))
	src := picture(4, 2, color.RGBA{10, 20, 30, 255})

	p := s.Begin(Meta{RadioPath: "radio_path_0", Kind: KindSSTV, Mode: "robot36", ModeName: "Robot 36",
		FrequencyHz: 436_200_000}, src)
	if p.ID != "20261027T153012Z-robot36" || p.Done || p.Width != 4 || p.Height != 2 || p.Lines != 0 {
		t.Fatalf("begin=%+v", p)
	}

	// The decoder keeps drawing into its buffer: the store serves its own copy.
	src.Pix[0] = 200
	p = s.Update(p.ID, 1, src)
	src.Pix[0] = 99
	if p.Lines != 1 {
		t.Errorf("lines=%d, want 1", p.Lines)
	}
	b, ok := s.PNG(p.ID)
	if !ok {
		t.Fatal("no png while receiving")
	}
	if r, _, _, _ := decodePNG(t, b).At(0, 0).RGBA(); r>>8 != 200 {
		t.Errorf("served red %d, want the copy taken on update (200)", r>>8)
	}

	p, err = s.Finish(p.ID, src)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Done || p.Lines != 2 {
		t.Errorf("finish=%+v, want done with all lines", p)
	}
	file, err := os.ReadFile(filepath.Join(dir, p.ID+".png"))
	if err != nil {
		t.Fatalf("png on disk: %v", err)
	}
	if r, _, _, _ := decodePNG(t, file).At(0, 0).RGBA(); r>>8 != 99 {
		t.Errorf("saved red %d, want the final picture (99)", r>>8)
	}
}

func TestStore_ListsNewestFirstAndLoadsSaved(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	for i, mode := range []string{"pd120", "robot36"} {
		s.now = fixedClock(at.Add(time.Duration(i) * time.Minute))
		p := s.Begin(Meta{Kind: KindSSTV, Mode: mode}, picture(2, 2, color.RGBA{A: 255}))
		if _, err := s.Finish(p.ID, picture(2, 2, color.RGBA{A: 255})); err != nil {
			t.Fatal(err)
		}
	}
	list := s.List()
	if len(list) != 2 || list[0].Mode != "robot36" || list[1].Mode != "pd120" {
		t.Fatalf("list=%+v, want robot36 then pd120", list)
	}

	// After a restart the saved pictures are back, without the radio path and frequency.
	again, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	list = again.List()
	if len(list) != 2 || list[0].ID != "20261027T100100Z-robot36" || !list[0].Done || list[0].Width != 2 {
		t.Fatalf("reloaded=%+v", list)
	}
	if list[0].StartedMs != uint64(at.Add(time.Minute).UnixMilli()) {
		t.Errorf("started=%d, want from the file name", list[0].StartedMs)
	}
	if _, ok := again.PNG(list[1].ID); !ok {
		t.Error("saved png is not served after restart")
	}
}

func TestStore_KeepsRecentInMemory(t *testing.T) {
	s, _ := NewStore(t.TempDir())
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	for i := range maxProducts + 3 {
		s.now = fixedClock(at.Add(time.Duration(i) * time.Second))
		s.Begin(Meta{Kind: KindSSTV, Mode: "robot36"}, picture(1, 1, color.RGBA{}))
	}
	if n := len(s.List()); n != maxProducts {
		t.Errorf("kept %d, want %d", n, maxProducts)
	}
}

func TestStore_UnknownID(t *testing.T) {
	s, _ := NewStore(t.TempDir())
	if _, ok := s.PNG("../etc/passwd"); ok {
		t.Error("served an unknown id")
	}
	if _, err := s.Finish("nope", picture(1, 1, color.RGBA{})); err == nil {
		t.Error("finish of an unknown id did not fail")
	}
}

func TestStore_FinishCutShortKeepsReceivedLines(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	p := s.Begin(Meta{Kind: KindSSTV, Mode: "pd120"}, picture(2, 4, color.RGBA{A: 255}))
	s.Update(p.ID, 2, picture(2, 4, color.RGBA{R: 50, A: 255}))
	p, err := s.Finish(p.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Done || p.Lines != 2 {
		t.Errorf("finish=%+v, want done with the 2 received lines", p)
	}
	b, _ := os.ReadFile(filepath.Join(dir, p.ID+".png"))
	if r, _, _, _ := decodePNG(t, b).At(0, 0).RGBA(); r>>8 != 50 {
		t.Errorf("saved red %d, want the last update (50)", r>>8)
	}
	if _, err := s.Finish(p.ID, nil); err == nil {
		t.Error("second finish did not fail")
	}
}
