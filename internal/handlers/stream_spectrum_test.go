package handlers

import (
	"context"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

// fakeReceiver — минимальный Receiver для тестов стрима.
type fakeReceiver struct {
	frames []sdr.IQFrame
}

func (f *fakeReceiver) Name() string                   { return "fake" }
func (f *fakeReceiver) Capabilities() sdr.Capabilities { return sdr.Capabilities{} }
func (f *fakeReceiver) Tune(uint64) error              { return nil }
func (f *fakeReceiver) SetGain(float32) error          { return nil }
func (f *fakeReceiver) Status() sdr.ReceiverStatus     { return sdr.ReceiverStatus{} }
func (f *fakeReceiver) Stop() error                    { return nil }

func (f *fakeReceiver) StartStream(_ context.Context, cfg sdr.StreamConfig) (<-chan sdr.IQFrame, error) {
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

	req := httptest.NewRequest("GET", "/api/stream/v1/spectrum", nil)
	rec := httptest.NewRecorder()
	h.Spectrum(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestSpectrum_UnknownRadioPath(t *testing.T) {
	hub := sdr.NewReceiverHub()
	h := NewStreamHandler(hub)

	req := httptest.NewRequest("GET", "/api/stream/v1/spectrum?radio_path=nope", nil)
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
	hub.Register("test-path", func() (sdr.Receiver, error) {
		return receiver, nil
	})

	h := NewStreamHandler(hub)

	req := httptest.NewRequest("GET",
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
