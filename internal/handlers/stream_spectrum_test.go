package handlers

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sdr/imitator"
)

// fakeReceiver — минимальный Receiver для тестов стрима.
type fakeReceiver struct {
	frames    []sdr.IQFrame
	streamCfg sdr.StreamConfig // последний запрошенный поток
	usable    float64          // доля окна без краёв; 0 — всё окно
}

func (f *fakeReceiver) Name() string { return "fake" }
func (f *fakeReceiver) Capabilities() sdr.Capabilities {
	return sdr.Capabilities{
		SampleRateRanges:      []sdr.SampleRateRange{{MinHz: 48_000, MaxHz: 2_400_000}},
		RecommendedSampleRate: 48_000,
		UsableFraction:        f.usable,
	}
}
func (f *fakeReceiver) Tune(uint64) error          { return nil }
func (f *fakeReceiver) SetGain(float32) error      { return nil }
func (f *fakeReceiver) Status() sdr.ReceiverStatus { return sdr.ReceiverStatus{} }
func (f *fakeReceiver) Stop() error                { return nil }

func (f *fakeReceiver) StartStream(_ context.Context, cfg sdr.StreamConfig) (<-chan sdr.IQFrame, error) {
	f.streamCfg = cfg
	ch := make(chan sdr.IQFrame, len(f.frames))
	for _, fr := range f.frames {
		ch <- fr
	}
	close(ch)
	return ch, nil
}

func makeToneFrames(n, blockSize int, freqHz, sampleRate float64) []sdr.IQFrame {
	frames := make([]sdr.IQFrame, n)
	for i := range frames {
		samples := make([]complex64, blockSize)
		for k := range samples {
			t := float64(i*blockSize+k) / sampleRate
			phase := 2.0 * math.Pi * freqHz * t
			samples[k] = complex(float32(math.Cos(phase)), float32(math.Sin(phase)))
		}
		frames[i] = sdr.IQFrame{
			Samples:     samples,
			CenterHz:    uint64(sampleRate / 2),
			SampleRate:  uint32(sampleRate),
			TimestampMs: uint64(time.Now().UnixMilli()),
		}
	}
	return frames
}

func TestSpectrum_MissingRadioPath(t *testing.T) {
	hub := sdr.NewReceiverHub()
	h := NewStreamHandler(hub)

	req := httptest.NewRequest(http.MethodGet, "/api/stream/v1/spectrum", nil)
	rec := httptest.NewRecorder()
	h.Spectrum(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestSpectrum_UnknownRadioPath(t *testing.T) {
	hub := sdr.NewReceiverHub()
	h := NewStreamHandler(hub)

	req := httptest.NewRequest(http.MethodGet, "/api/stream/v1/spectrum?radio_path=nope", nil)
	rec := httptest.NewRecorder()
	h.Spectrum(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestSpectrum_ProducesValidSSTMFrames(t *testing.T) {
	const (
		bins       = 128 // nextPow2(128) = 128 = fftSize
		sampleRate = 48000.0
		toneHz     = 5000.0
		numBlocks  = 5
	)
	fftSize := nextPow2(bins)

	hub := sdr.NewReceiverHub()
	receiver := &fakeReceiver{
		frames: makeToneFrames(numBlocks, fftSize, toneHz, sampleRate),
	}
	hub.Register("test-path", sdr.PathConfig{}, func() (sdr.Receiver, error) {
		return receiver, nil
	})

	h := NewStreamHandler(hub)

	req := httptest.NewRequest(http.MethodGet,
		"/api/stream/v1/spectrum?radio_path=test-path&bins=128&fps=30&mode=avg", nil)
	ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.Spectrum(rec, req)

	body := rec.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("empty response body")
	}

	// Разбираем первый фрейм
	if len(body) < dsp.HeaderSize {
		t.Fatalf("body too short: %d bytes", len(body))
	}

	magic := binary.LittleEndian.Uint32(body[0:4])
	if magic != dsp.Magic {
		t.Errorf("magic=0x%08X, want 0x%08X", magic, dsp.Magic)
	}

	if body[4] != dsp.Version {
		t.Errorf("version=%d, want %d", body[4], dsp.Version)
	}

	if body[5] != byte(dsp.DataTypeFFTDB) {
		t.Errorf("data_type=0x%02X, want 0x01", body[5])
	}

	payloadLen := binary.LittleEndian.Uint32(body[32:36])
	expectedPayload := uint32(fftSize * 4) // float32 — bins = fftSize после nextPow2
	if payloadLen != expectedPayload {
		t.Errorf("payload_len=%d, want %d", payloadLen, expectedPayload)
	}

	seq := binary.LittleEndian.Uint32(body[36:40])
	if seq != 0 {
		t.Errorf("first frame seq=%d, want 0", seq)
	}

	totalFrameSize := dsp.HeaderSize + int(payloadLen)
	if len(body) < totalFrameSize {
		t.Fatalf("body=%d bytes, need at least %d for one frame", len(body), totalFrameSize)
	}
}

// A receiver with unusable window edges gets only the middle of the spectrum: fewer bins,
// a narrower span, and a tone still on its own frequency.
func TestSpectrum_CropsEdgesToUsableBand(t *testing.T) {
	const (
		bins       = 128
		sampleRate = 48000.0
		toneHz     = 6000.0 // 16 bins of 375 Hz right of the center
	)
	hub := sdr.NewReceiverHub()
	receiver := &fakeReceiver{frames: makeToneFrames(5, bins, toneHz, sampleRate), usable: 0.5}
	hub.Register("p", sdr.PathConfig{}, func() (sdr.Receiver, error) { return receiver, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/stream/v1/spectrum?radio_path=p&bins=128&fps=30", nil)
	rec := httptest.NewRecorder()
	NewStreamHandler(hub).Spectrum(rec, req)

	f := readModeFrame(t, bytes.NewReader(rec.Body.Bytes()), dsp.ModeAvg)
	if len(f.dbfs) != bins/2 || f.spanHz != 24_000 {
		t.Fatalf("got %d bins over %d Hz, want %d bins over 24000 Hz", len(f.dbfs), f.spanHz, bins/2)
	}
	if got := peakBin(f.dbfs); got != bins/4+16 {
		t.Errorf("tone at bin %d, want %d", got, bins/4+16)
	}
}

func TestSpectrum_RequestsPathSampleRate(t *testing.T) {
	cases := []struct {
		name       string
		configured uint32
		want       uint32
	}{
		{"driver recommended", 0, 48_000},
		{"configured override", 1_024_000, 1_024_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hub := sdr.NewReceiverHub()
			receiver := &fakeReceiver{}
			hub.Register("p", sdr.PathConfig{SampleRateHz: tc.configured}, func() (sdr.Receiver, error) {
				return receiver, nil
			})

			req := httptest.NewRequest(http.MethodGet, "/api/stream/v1/spectrum?radio_path=p", nil)
			NewStreamHandler(hub).Spectrum(httptest.NewRecorder(), req)

			if receiver.streamCfg.SampleRate != tc.want {
				t.Errorf("stream SampleRate=%d, want %d", receiver.streamCfg.SampleRate, tc.want)
			}
		})
	}
}

func TestSpectrum_UnsupportedSampleRate(t *testing.T) {
	hub := sdr.NewReceiverHub()
	receiver := &fakeReceiver{}
	hub.Register("p", sdr.PathConfig{SampleRateHz: 10_000_000}, func() (sdr.Receiver, error) {
		return receiver, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stream/v1/spectrum?radio_path=p", nil)
	rec := httptest.NewRecorder()
	NewStreamHandler(hub).Spectrum(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", rec.Code)
	}
	if receiver.streamCfg != (sdr.StreamConfig{}) {
		t.Errorf("stream must not start with unsupported rate, got %+v", receiver.streamCfg)
	}
}

// liveReceiver выдаёт тон блоками до остановки и, как железо, не даёт запустить второй поток.
type liveReceiver struct {
	mu      sync.Mutex
	starts  int
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func (l *liveReceiver) Name() string { return "live" }
func (l *liveReceiver) Capabilities() sdr.Capabilities {
	return sdr.Capabilities{
		SampleRateRanges:      []sdr.SampleRateRange{{MinHz: 48_000, MaxHz: 2_400_000}},
		RecommendedSampleRate: 1_024_000,
	}
}
func (l *liveReceiver) Tune(uint64) error          { return nil }
func (l *liveReceiver) SetGain(float32) error      { return nil }
func (l *liveReceiver) Status() sdr.ReceiverStatus { return sdr.ReceiverStatus{} }

func (l *liveReceiver) StartStream(ctx context.Context, cfg sdr.StreamConfig) (<-chan sdr.IQFrame, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running {
		return nil, errors.New("device busy")
	}
	l.starts++
	l.running = true
	ctx, l.cancel = context.WithCancel(ctx)
	l.done = make(chan struct{})
	ch := make(chan sdr.IQFrame, 4)
	go func() {
		defer close(l.done)
		defer close(ch)
		for {
			frames := makeToneFrames(1, cfg.BlockSize, 50_000, float64(cfg.SampleRate))
			select {
			case ch <- frames[0]:
			case <-ctx.Done():
				return
			}
			select {
			case <-time.After(5 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (l *liveReceiver) Stop() error {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return nil
	}
	l.running = false
	l.cancel()
	done := l.done
	l.mu.Unlock()
	<-done
	return nil
}

func (l *liveReceiver) startCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.starts
}

func TestSpectrum_TwoViewersShareDeviceStream(t *testing.T) {
	hub := sdr.NewReceiverHub()
	receiver := &liveReceiver{}
	hub.Register("p", sdr.PathConfig{}, func() (sdr.Receiver, error) { return receiver, nil })
	h := NewStreamHandler(hub)

	view := func(bins string, rec *httptest.ResponseRecorder, wg *sync.WaitGroup) {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodGet, "/api/stream/v1/spectrum?radio_path=p&fps=30&bins="+bins, nil)
		ctx, cancel := context.WithTimeout(req.Context(), 400*time.Millisecond)
		defer cancel()
		h.Spectrum(rec, req.WithContext(ctx))
	}

	recA, recB := httptest.NewRecorder(), httptest.NewRecorder()
	var wg sync.WaitGroup
	wg.Add(1)
	go view("256", recA, &wg)
	deadline := time.Now().Add(time.Second)
	for receiver.startCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	wg.Add(1)
	go view("1024", recB, &wg)
	wg.Wait()

	if n := receiver.startCount(); n != 1 {
		t.Errorf("device started %d times, want 1 shared stream", n)
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{"A": recA, "B": recB} {
		if rec.Code != http.StatusOK {
			t.Errorf("viewer %s: status=%d, body=%q", name, rec.Code, rec.Body.String())
		}
		if rec.Body.Len() < dsp.HeaderSize {
			t.Errorf("viewer %s: got %d bytes, want spectrum frames", name, rec.Body.Len())
		}
	}
	// The last viewer does not stop the receiver at once: the session waits out
	// its idle grace so a spectrum stream can reopen. The stop follows that grace.
	stopBy := time.Now().Add(2 * time.Second)
	for time.Now().Before(stopBy) {
		receiver.mu.Lock()
		running := receiver.running
		receiver.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("device must stop after the idle grace that follows the last viewer")
}

func TestIQChunker_SplitsIntoOwnBlocks(t *testing.T) {
	in := make([]complex64, 300)
	for i := range in {
		in[i] = complex(float32(i), 0)
	}
	c := newIQChunker(128)
	var blocks [][]complex64
	emit := func(b []complex64) {
		blocks = append(blocks, append([]complex64(nil), b...))
		b[0] = -1 // конвейер меняет блок на месте: вход не должен пострадать
	}
	c.feed(in[:100], emit)
	c.feed(in[100:], emit)

	if len(blocks) != 2 {
		t.Fatalf("blocks=%d, want 2 (300 samples / 128)", len(blocks))
	}
	for bi, b := range blocks {
		for i, v := range b {
			if want := float32(bi*128 + i); real(v) != want {
				t.Fatalf("block %d sample %d = %v, want %v", bi, i, real(v), want)
			}
		}
	}
	for i, v := range in {
		if real(v) != float32(i) {
			t.Fatalf("input sample %d modified: %v", i, v)
		}
	}

	c.feed(make([]complex64, 84), emit)
	if len(blocks) != 3 {
		t.Errorf("remainder of 44 samples plus 84 must form a third block, blocks=%d", len(blocks))
	}
}

func TestIQChunker_ResetDropsPartialBlock(t *testing.T) {
	c := newIQChunker(4)
	var blocks [][]complex64
	emit := func(b []complex64) { blocks = append(blocks, append([]complex64(nil), b...)) }

	c.feed([]complex64{1, 2, 3}, emit)
	c.reset()
	c.feed([]complex64{10, 20, 30, 40}, emit)

	if len(blocks) != 1 {
		t.Fatalf("blocks=%d, want 1", len(blocks))
	}
	if blocks[0][0] != 10 {
		t.Errorf("block starts with %v, want 10: samples from before reset leaked", blocks[0][0])
	}
}

// After a retune the first frame must hold only the new tuning: the max-hold
// accumulator would otherwise keep the old tone as well.
func TestSpectrum_RetuneResetsAccumulation(t *testing.T) {
	const (
		fftSize    = 128
		sampleRate = 48000.0
		toneHz     = 6000.0 // 16 bins from the center at 48 kHz / 128
	)
	before := makeToneFrames(1, fftSize, toneHz, sampleRate)[0]
	before.CenterHz = 435_000_000
	after := makeToneFrames(1, fftSize, -toneHz, sampleRate)[0]
	after.CenterHz = 436_000_000

	hub := sdr.NewReceiverHub()
	receiver := &fakeReceiver{frames: []sdr.IQFrame{before, after}}
	hub.Register("p", sdr.PathConfig{}, func() (sdr.Receiver, error) { return receiver, nil })
	h := NewStreamHandler(hub)

	req := httptest.NewRequest(http.MethodGet,
		"/api/stream/v1/spectrum?radio_path=p&bins=128&fps=10&mode=maxhold", nil)
	ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
	defer cancel()
	rec := httptest.NewRecorder()
	h.Spectrum(rec, req.WithContext(ctx))

	body := rec.Body.Bytes()
	frameLen := dsp.HeaderSize + fftSize*4
	if len(body) < frameLen {
		t.Fatalf("body=%d bytes, want at least one frame of %d", len(body), frameLen)
	}
	last := body[len(body)-frameLen:]
	if c := binary.LittleEndian.Uint64(last[16:24]); c != after.CenterHz {
		t.Fatalf("center=%d, want %d", c, after.CenterHz)
	}
	bin := func(i int) float32 {
		off := dsp.HeaderSize + i*4
		return math.Float32frombits(binary.LittleEndian.Uint32(last[off : off+4]))
	}
	newTone, oldTone := bin(fftSize/2-16), bin(fftSize/2+16)
	if newTone-oldTone < 40 {
		t.Errorf("new tone %.1f dB, old tone bin %.1f dB: old tuning leaked into the frame", newTone, oldTone)
	}
}

// sstmFrame — разобранный кадр спектра из HTTP-потока.
type sstmFrame struct {
	mode             dsp.DisplayMode
	centerHz, spanHz uint64
	dbfs             []float32
}

// readSpectrumFrame reads SSTM frames from the stream until one carries FFT data.
func readSpectrumFrame(t *testing.T, r io.Reader) sstmFrame {
	t.Helper()
	hdr := make([]byte, dsp.HeaderSize)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			t.Fatalf("read header: %v", err)
		}
		payload := make([]byte, binary.LittleEndian.Uint32(hdr[32:36]))
		if _, err := io.ReadFull(r, payload); err != nil {
			t.Fatalf("read payload: %v", err)
		}
		if dsp.DataType(hdr[5]) != dsp.DataTypeFFTDB {
			continue
		}
		f := sstmFrame{
			mode:     dsp.DisplayMode(hdr[6]),
			centerHz: binary.LittleEndian.Uint64(hdr[16:24]),
			spanHz:   binary.LittleEndian.Uint64(hdr[24:32]),
			dbfs:     make([]float32, len(payload)/4),
		}
		for i := range f.dbfs {
			f.dbfs[i] = math.Float32frombits(binary.LittleEndian.Uint32(payload[i*4:]))
		}
		return f
	}
}

// readModeFrame reads FFT frames until one of the given display mode arrives.
func readModeFrame(t *testing.T, r io.Reader, mode dsp.DisplayMode) sstmFrame {
	t.Helper()
	for {
		if f := readSpectrumFrame(t, r); f.mode == mode {
			return f
		}
	}
}

func peakBin(dbfs []float32) int {
	idx := 0
	for i, v := range dbfs {
		if v > dbfs[idx] {
			idx = i
		}
	}
	return idx
}

// End to end through the imitator, the device hub and the spectrum stream:
// retuning by Δ moves the carrier peak by −Δ on the frequency axis of the frame.
func TestSpectrum_ImitatorRetuneMovesPeak(t *testing.T) {
	const (
		carrierHz = 436_500_000
		deltaHz   = 100_000
		rate      = 1_024_000
		bins      = 1024 // 1 kHz per bin
	)
	receiver := imitator.New(imitator.Options{Signals: []imitator.Signal{
		{Kind: imitator.SignalCarrier, FrequencyHz: carrierHz, LevelDBFS: -20},
	}})
	hub := sdr.NewReceiverHub()
	hub.Register("p", sdr.PathConfig{SampleRateHz: rate}, func() (sdr.Receiver, error) { return receiver, nil })
	srv := httptest.NewServer(http.HandlerFunc(NewStreamHandler(hub).Spectrum))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/stream/v1/spectrum?radio_path=p&fps=20&mode=current&bins="+strconv.Itoa(bins), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET spectrum: %v", err)
	}
	defer resp.Body.Close()

	// Частота пика по оси кадра: левый край center − span/2, шаг span/bins.
	peakHz := func(f sstmFrame) float64 {
		return float64(f.centerHz) - float64(f.spanHz)/2 + float64(peakBin(f.dbfs))*float64(f.spanHz)/float64(len(f.dbfs))
	}
	binHz := float64(rate) / bins

	before := readSpectrumFrame(t, resp.Body)
	if before.centerHz != carrierHz {
		t.Fatalf("center=%d, want the receiver on the carrier %d", before.centerHz, carrierHz)
	}
	if got := peakHz(before); math.Abs(got-carrierHz) > binHz {
		t.Fatalf("peak at %.0f Hz before tune, want %d ± one bin", got, carrierHz)
	}

	if err := receiver.Tune(carrierHz + deltaHz); err != nil {
		t.Fatalf("Tune: %v", err)
	}
	after := readSpectrumFrame(t, resp.Body)
	for after.centerHz != carrierHz+deltaHz {
		after = readSpectrumFrame(t, resp.Body)
	}
	wantBin := float64(len(after.dbfs))/2 - deltaHz/binHz
	if got := peakBin(after.dbfs); math.Abs(float64(got)-wantBin) > 1 {
		t.Errorf("peak bin %d after tune by %d Hz, want %.0f ± 1 (moved by −Δ)", got, deltaHz, wantBin)
	}
	if got := peakHz(after); math.Abs(got-carrierHz) > binHz {
		t.Errorf("peak at %.0f Hz after tune, want the carrier to stay at %d ± one bin", got, carrierHz)
	}
}

// Narrowing the device band through the hub changes span_hz of the frames, keeps
// the carrier at its frequency and drops the peak hold of the wider band.
func TestSpectrum_SampleRateChangeNarrowsSpanAndResetsHold(t *testing.T) {
	const (
		carrierHz = 436_500_000
		tunedHz   = carrierHz - 50_000
		bins      = 1024
	)
	receiver := imitator.New(imitator.Options{Signals: []imitator.Signal{
		{Kind: imitator.SignalCarrier, FrequencyHz: carrierHz, LevelDBFS: -20},
	}})
	if err := receiver.Tune(tunedHz); err != nil {
		t.Fatalf("Tune: %v", err)
	}
	hub := sdr.NewReceiverHub()
	hub.Register("p", sdr.PathConfig{}, func() (sdr.Receiver, error) { return receiver, nil })
	srv := httptest.NewServer(http.HandlerFunc(NewStreamHandler(hub).Spectrum))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/stream/v1/spectrum?radio_path=p&fps=20&mode=maxhold&bins="+strconv.Itoa(bins), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET spectrum: %v", err)
	}
	defer resp.Body.Close()

	binOf := func(f sstmFrame) int {
		return int(math.Round(float64(bins)/2 + 50_000/(float64(f.spanHz)/bins)))
	}
	wide := readModeFrame(t, resp.Body, dsp.ModeMaxHold)
	if wide.spanHz != 2_048_000 {
		t.Fatalf("span=%d, want the recommended 2048000", wide.spanHz)
	}
	oldBin := binOf(wide)
	if got := peakBin(wide.dbfs); abs(got-oldBin) > 1 {
		t.Fatalf("peak bin %d at 2.048 MHz, want %d ± 1", got, oldBin)
	}

	if err := hub.SetSampleRate("p", 256_000); err != nil {
		t.Fatalf("SetSampleRate: %v", err)
	}
	narrow := readModeFrame(t, resp.Body, dsp.ModeMaxHold)
	for narrow.spanHz != 256_000 {
		narrow = readModeFrame(t, resp.Body, dsp.ModeMaxHold)
	}
	if got, want := peakBin(narrow.dbfs), binOf(narrow); abs(got-want) > 1 {
		t.Errorf("peak bin %d at 256 kHz, want %d ± 1 (carrier stays at its frequency)", got, want)
	}
	if peak := narrow.dbfs[peakBin(narrow.dbfs)]; narrow.dbfs[oldBin] > peak-40 {
		t.Errorf("bin %d holds %.1f dB vs peak %.1f dB: hold of the wider band leaked", oldBin, narrow.dbfs[oldBin], peak)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// openImitatorSpectrum streams the spectrum of an imitator with one sweeping
// carrier; the caller reads frames from the returned body.
func openImitatorSpectrum(t *testing.T, query string) io.ReadCloser {
	t.Helper()
	receiver := imitator.New(imitator.Options{Signals: []imitator.Signal{{
		Kind: imitator.SignalCarrier, FrequencyHz: 436_500_000, LevelDBFS: -20,
		SweepHz: 200_000, SweepPeriod: time.Second, // 800 kHz/s
	}}})
	hub := sdr.NewReceiverHub()
	hub.Register("p", sdr.PathConfig{SampleRateHz: 1_024_000}, func() (sdr.Receiver, error) { return receiver, nil })
	srv := httptest.NewServer(http.HandlerFunc(NewStreamHandler(hub).Spectrum))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/stream/v1/spectrum?radio_path=p&fps=20&bins=1024&"+query, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET spectrum: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp.Body
}

// binsAbove counts bins of the frame above the level: the trace width of the carrier.
func binsAbove(dbfs []float32, level float32) int {
	n := 0
	for _, v := range dbfs {
		if v > level {
			n++
		}
	}
	return n
}

// Peak hold keeps the trace of a sweeping carrier across UI frames. In half a
// second the triangle sweep goes from the center to +200 kHz and back, so the
// held trace covers ~200 bins, while one averaging window covers a few dozen.
func TestSpectrum_MaxHoldKeepsTraceAcrossFrames(t *testing.T) {
	const frames = 10 // 0.5 s at 20 fps
	traceWidth := func(query string, mode dsp.DisplayMode) int {
		body := openImitatorSpectrum(t, query)
		var f sstmFrame
		for range frames {
			f = readModeFrame(t, body, mode)
		}
		return binsAbove(f.dbfs, -80)
	}

	held, window := traceWidth("mode=maxhold", dsp.ModeMaxHold), traceWidth("mode=avg", dsp.ModeAvg)
	if held < 150 {
		t.Errorf("maxhold trace %d bins after %d frames, want ≥ 150: the hold was reset between frames", held, frames)
	}
	if window > 100 {
		t.Errorf("avg trace %d bins, want ≤ 100: one window must not accumulate earlier frames", window)
	}
}

// In peak hold every tick carries two frames: the hold and the live spectrum.
// The live trace follows the carrier, the hold never drops below it.
func TestSpectrum_MaxHoldAlsoSendsLiveFrames(t *testing.T) {
	const frames = 10
	body := openImitatorSpectrum(t, "mode=maxhold")
	var hold, live sstmFrame
	for range frames {
		hold = readModeFrame(t, body, dsp.ModeMaxHold)
		live = readSpectrumFrame(t, body)
		if live.mode != dsp.ModeCurrent {
			t.Fatalf("frame after the hold has mode %d, want the live frame (%d)", live.mode, dsp.ModeCurrent)
		}
		if live.centerHz != hold.centerHz || len(live.dbfs) != len(hold.dbfs) {
			t.Fatalf("live frame center %d bins %d, want the hold's %d and %d",
				live.centerHz, len(live.dbfs), hold.centerHz, len(hold.dbfs))
		}
	}
	for i := range hold.dbfs {
		if hold.dbfs[i] < live.dbfs[i]-0.01 {
			t.Fatalf("bin %d: hold %.1f below live %.1f", i, hold.dbfs[i], live.dbfs[i])
		}
	}
	if w := binsAbove(live.dbfs, -80); w > 100 {
		t.Errorf("live trace %d bins, want ≤ 100: it must not accumulate", w)
	}
	if w := binsAbove(hold.dbfs, -80); w < 150 {
		t.Errorf("hold trace %d bins, want ≥ 150", w)
	}
}

// Averaging over N frames (window_ms = N frame periods) spreads the sweeping
// carrier over the bins it passed during the window; one frame does not.
func TestSpectrum_AvgWindowAveragesAcrossFrames(t *testing.T) {
	const frames = 10
	body := openImitatorSpectrum(t, "mode=avg&window_ms=500")
	var f sstmFrame
	for range frames {
		f = readModeFrame(t, body, dsp.ModeAvg)
	}
	if w := binsAbove(f.dbfs, -80); w < 150 {
		t.Errorf("avg over 500 ms trace %d bins, want ≥ 150: frames are not averaged together", w)
	}
}

func TestAvgFrames(t *testing.T) {
	tests := []struct {
		windowMs string
		fps      int
		want     int
	}{
		{"", 20, 1},
		{"abc", 20, 1},
		{"50", 20, 1},
		{"10", 20, 1},
		{"500", 20, 10},
		{"2500", 20, 50},
		{"1000", 10, 10},
		{"100000", 20, maxAvgFrames},
	}
	for _, tt := range tests {
		if got := avgFrames(tt.windowMs, tt.fps); got != tt.want {
			t.Errorf("avgFrames(%q, %d) = %d, want %d", tt.windowMs, tt.fps, got, tt.want)
		}
	}
}

func TestParseDisplayMode(t *testing.T) {
	tests := []struct {
		input string
		want  dsp.DisplayMode
	}{
		{"", dsp.ModeAvg},
		{"avg", dsp.ModeAvg},
		{"current", dsp.ModeCurrent},
		{"maxhold", dsp.ModeMaxHold},
		{"minhold", dsp.ModeMinHold},
		{"exp_avg", dsp.ModeExpAvg},
		{"unknown", dsp.ModeAvg},
	}
	for _, tt := range tests {
		got := parseDisplayMode(tt.input)
		if got != tt.want {
			t.Errorf("parseDisplayMode(%q)=%d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestNextPow2(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{100, 128},
		{1024, 1024},
		{1025, 2048},
	}
	for _, tt := range tests {
		got := nextPow2(tt.in)
		if got != tt.want {
			t.Errorf("nextPow2(%d)=%d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseIntClamped(t *testing.T) {
	tests := []struct {
		s           string
		def, lo, hi int
		want        int
	}{
		{"", 10, 1, 100, 10},
		{"abc", 10, 1, 100, 10},
		{"5", 10, 1, 100, 5},
		{"0", 10, 1, 100, 1},
		{"200", 10, 1, 100, 100},
	}
	for _, tt := range tests {
		got := parseIntClamped(tt.s, tt.def, tt.lo, tt.hi)
		if got != tt.want {
			t.Errorf("parseIntClamped(%q,%d,%d,%d)=%d, want %d",
				tt.s, tt.def, tt.lo, tt.hi, got, tt.want)
		}
	}
}
