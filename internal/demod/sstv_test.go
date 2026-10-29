package demod

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/products"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sstv"
)

// SSTV runs at a low receiver rate: a whole picture at 1 MHz would not fit the test memory.
const sstvRate = 128_000

// sstvAir — one picture in mode m over FM, 20 kHz from the tuning, with noise at snr dB in 15 kHz.
func sstvAir(t *testing.T, img image.Image, m sstv.Mode, snr float64) []complex64 {
	return sstvAirAt(t, img, m, snr, 20_000)
}

func sstvAirAt(t *testing.T, img image.Image, m sstv.Mode, snr, offsetHz float64) []complex64 {
	t.Helper()
	enc := sstv.NewEncoder(img, m, sstvRate)
	fm := modem.NewFM(modem.FMConfig{SampleRate: sstvRate, DeviationHz: 5000})
	lead := make([]float32, sstvRate) // a second before and after the picture
	clean := fm.Modulate(nil, lead)
	buf := make([]float32, 8192)
	for {
		n := enc.Read(buf)
		clean = fm.Modulate(clean, buf[:n])
		if n < len(buf) {
			break
		}
	}
	clean = fm.Modulate(clean, lead)
	ch := modem.NewChannel(modem.ChannelConfig{
		SampleRate:       sstvRate,
		OffsetHz:         func(time.Duration) float64 { return offsetHz },
		SNRdB:            snr,
		NoiseBandwidthHz: 15_000,
		Seed:             11,
	})
	return ch.Process(nil, clean)
}

// pictureEvents feeds the frames and also returns the most lines the status reported mid-picture.
func pictureEvents(c *Chain, frames []sdr.IQFrame, s sdr.ChannelSettings) ([]sstv.Event, int) {
	var out []sstv.Event
	most := 0
	for _, f := range frames {
		c.Process(f, s)
		out = append(out, c.Pictures()...)
		if st := c.Status(); st.Picture != nil {
			most = max(most, st.Picture.Lines)
		}
	}
	return out, most
}

func rgbPSNR(a, b image.Image) float64 {
	var se float64
	n := 0
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			r1, g1, b1, _ := a.At(x, y).RGBA()
			r2, g2, b2, _ := b.At(x, y).RGBA()
			for _, d := range []float64{float64(r1>>8) - float64(r2>>8), float64(g1>>8) - float64(g2>>8),
				float64(b1>>8) - float64(b2>>8)} {
				se += d * d
				n++
			}
		}
	}
	return 10 * math.Log10(255*255/(se/float64(n)))
}

func TestChain_ReceivesSSTVPicture(t *testing.T) {
	src := sstv.TestPattern(320, 240)
	frames := toFrames(sstvAir(t, src, sstv.Robot36, 20), sstvRate, 8192, 0)
	c := NewChain()
	// The squelch sits above the signal: SSTV is timed by the clock and must not lose audio.
	s := sdr.ChannelSettings{OffsetHz: 20_000, BandwidthHz: 15_000, SquelchDBFS: 0, Demod: DemodSSTV}
	events, most := pictureEvents(c, frames, s)

	var start, done *sstv.Event
	for i := range events {
		switch events[i].Kind {
		case sstv.EventStart:
			start = &events[i]
		case sstv.EventDone:
			done = &events[i]
		}
	}
	if start == nil || start.Mode.ID != "robot36" || done == nil {
		t.Fatalf("start=%v done=%v, want a Robot 36 picture", start != nil, done != nil)
	}
	// The test pattern has sharp edges the audio band blurs; noise adds the rest.
	if p := rgbPSNR(done.Image, src); p < 18 {
		t.Errorf("PSNR %.1f dB at 20 dB SNR, want ≥ 18", p)
	}
	if st := c.Status(); !st.Supported || st.Good != 1 || st.Picture != nil {
		t.Errorf("status=%+v, want supported, one picture and none in progress", st)
	}
	if most < 200 {
		t.Errorf("status showed at most %d lines in progress, want the picture growing", most)
	}
}

func TestService_PublishesSSTVProduct(t *testing.T) {
	dir := t.TempDir()
	store, err := products.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sink := &sinkStub{}
	s := sdr.ChannelSettings{OffsetHz: 20_000, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodSSTV}
	svc := NewService(&channelsStub{s: s}, sink.emit).WithProducts(store)
	ch := make(chan sdr.IQFrame, 16)
	go func() {
		for _, f := range toFrames(sstvAir(t, sstv.TestPattern(320, 240), sstv.Robot36, 30), sstvRate, 8192, 0) {
			f.CenterHz = 436_000_000
			ch <- f
		}
		close(ch)
	}()
	svc.Run("radio_path_0", ch)

	evs := sink.ofType(EventProduct)
	if len(evs) < 3 {
		t.Fatalf("product events=%d, want start, progress and done", len(evs))
	}
	first, last := evs[0], evs[len(evs)-1]
	if first["done"] != false || first["lines"] != 0.0 || first["mode"] != "robot36" ||
		first["radio_path"] != "radio_path_0" || first["frequency_hz"] != 436_020_000.0 {
		t.Errorf("first=%v, want a fresh Robot 36 picture on the channel frequency", first)
	}
	if last["done"] != true || last["lines"] != 240.0 || last["id"] != first["id"] {
		t.Errorf("last=%v, want the same picture done", last)
	}
	// Progress is throttled: a 37 s picture gives far fewer events than its 120 line groups.
	if len(evs) > 60 {
		t.Errorf("product events=%d, want throttled progress", len(evs))
	}
	if _, err := os.Stat(filepath.Join(dir, first["id"].(string)+".png")); err != nil {
		t.Errorf("picture not saved: %v", err)
	}
}

// Doppler leaves the carrier off the channel center: the discriminator turns that into
// a constant in the audio, which must not reach the tone estimate.
func TestChain_SSTVToleratesMistunedCarrier(t *testing.T) {
	src := sstv.TestPattern(320, 240)
	frames := toFrames(sstvAirAt(t, src, sstv.Robot36, 30, 22_000), sstvRate, 8192, 0)
	s := sdr.ChannelSettings{OffsetHz: 20_000, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodSSTV}
	events, _ := pictureEvents(NewChain(), frames, s)
	var done *sstv.Event
	for i := range events {
		if events[i].Kind == sstv.EventDone {
			done = &events[i]
		}
	}
	if done == nil {
		t.Fatal("no picture with the carrier 2 kHz off")
	}
	if p := rgbPSNR(done.Image, src); p < 18 {
		t.Errorf("PSNR %.1f dB with the carrier 2 kHz off, want ≥ 18", p)
	}
}

// Retuning mid-picture means another transmitter: the picture is dropped and VIS is searched again.
func TestChain_RetuneDropsThePicture(t *testing.T) {
	frames := toFrames(sstvAir(t, sstv.TestPattern(320, 240), sstv.Robot36, 30), sstvRate, 8192, 0)
	s := sdr.ChannelSettings{OffsetHz: 20_000, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodSSTV}
	for name, retune := range map[string]func(f *sdr.IQFrame, s *sdr.ChannelSettings){
		"channel offset": func(_ *sdr.IQFrame, s *sdr.ChannelSettings) { s.OffsetHz = 60_000 },
		"receiver tuning": func(f *sdr.IQFrame, _ *sdr.ChannelSettings) {
			f.CenterHz += 40_000
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := NewChain()
			half := len(frames) * 6 / 10
			for _, f := range frames[:half] {
				c.Process(f, s)
			}
			if c.Status().Picture == nil {
				t.Fatal("no picture in progress before the retune")
			}
			s2 := s
			for i, f := range frames[half:] {
				retune(&f, &s2)
				c.Process(f, s2)
				if i == 0 && c.Status().Picture != nil {
					t.Fatal("picture still in progress after the retune")
				}
				for _, e := range c.Pictures() {
					if e.Kind != sstv.EventStart {
						t.Fatalf("event %v of the dropped picture after the retune", e.Kind)
					}
				}
			}
		})
	}
}

func TestService_DemodOffSavesThePictureAndDropsIt(t *testing.T) {
	store, err := products.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &sinkStub{}
	frames := toFrames(sstvAir(t, sstv.TestPattern(320, 240), sstv.Robot36, 30), sstvRate, 8192, 0)
	ch := &channelsStub{s: sdr.ChannelSettings{OffsetHz: 20_000, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodSSTV}}
	svc := NewService(ch, sink.emit).WithProducts(store)
	in := make(chan sdr.IQFrame)
	done := make(chan struct{})
	go func() {
		svc.Run("radio_path_0", in)
		close(done)
	}()
	half := len(frames) * 6 / 10
	for i, f := range frames {
		ch.mu.Lock()
		// Выключено на середине и снова включено: хвост не должен дорисовать ту же картинку.
		ch.s.DemodOff = i >= half && i < half+8
		ch.mu.Unlock()
		in <- f
	}
	close(in)
	<-done

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("products=%d, want the cut picture only", len(list))
	}
	if p := list[0]; !p.Done || p.Lines == 0 || p.Lines >= p.Height {
		t.Errorf("product=%+v, want saved cut short", p)
	}
}

func TestService_RetuneSavesThePictureCutShort(t *testing.T) {
	store, err := products.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &sinkStub{}
	frames := toFrames(sstvAir(t, sstv.TestPattern(320, 240), sstv.Robot36, 30), sstvRate, 8192, 0)
	ch := &channelsStub{s: sdr.ChannelSettings{OffsetHz: 20_000, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: DemodSSTV}}
	svc := NewService(ch, sink.emit).WithProducts(store)
	in := make(chan sdr.IQFrame)
	done := make(chan struct{})
	go func() {
		svc.Run("radio_path_0", in)
		close(done)
	}()
	half := len(frames) * 6 / 10
	for i, f := range frames {
		if i == half {
			ch.mu.Lock()
			ch.s.OffsetHz = 60_000
			ch.mu.Unlock()
		}
		in <- f
	}
	close(in)
	<-done

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("products=%d, want 1", len(list))
	}
	if p := list[0]; !p.Done || p.Lines == 0 || p.Lines >= p.Height {
		t.Errorf("product=%+v, want saved cut short", p)
	}
}
