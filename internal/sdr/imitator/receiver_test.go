package imitator

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

func TestReceiver_Name(t *testing.T) {
	r := New(Options{})
	if got := r.Name(); got != "imitator" {
		t.Fatalf("Name() = %q, want %q", got, "imitator")
	}
}

func TestReceiver_Capabilities(t *testing.T) {
	r := New(Options{})
	caps := r.Capabilities()
	if caps.MinFreqHz == 0 || caps.MaxFreqHz == 0 {
		t.Fatal("capabilities must have non-zero freq range")
	}
	if caps.MaxFreqHz <= caps.MinFreqHz {
		t.Fatalf("MaxFreqHz (%d) must be > MinFreqHz (%d)", caps.MaxFreqHz, caps.MinFreqHz)
	}
}

func TestReceiver_Tune(t *testing.T) {
	r := New(Options{})
	caps := r.Capabilities()

	if err := r.Tune(caps.MinFreqHz); err != nil {
		t.Fatalf("Tune(MinFreqHz) error: %v", err)
	}
	if err := r.Tune(caps.MaxFreqHz); err != nil {
		t.Fatalf("Tune(MaxFreqHz) error: %v", err)
	}
	if err := r.Tune(0); err == nil {
		t.Fatal("Tune(0) should return error")
	}
	if err := r.Tune(caps.MaxFreqHz + 1); err == nil {
		t.Fatal("Tune(above max) should return error")
	}
}

func TestReceiver_StartStream_SyntheticTone(t *testing.T) {
	r := New(Options{ToneOffsetHz: 1000})

	cfg := sdr.StreamConfig{
		CenterHz:   145_900_000,
		SampleRate: 48000,
		BlockSize:  1024,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := r.StartStream(ctx, cfg)
	if err != nil {
		t.Fatalf("StartStream error: %v", err)
	}

	const wantFrames = 3
	received := 0
	for frame := range ch {
		if len(frame.Samples) != cfg.BlockSize {
			t.Fatalf("frame %d: len(Samples)=%d, want %d", received, len(frame.Samples), cfg.BlockSize)
		}
		if frame.CenterHz != cfg.CenterHz {
			t.Fatalf("frame %d: CenterHz=%d, want %d", received, frame.CenterHz, cfg.CenterHz)
		}
		if frame.SampleRate != cfg.SampleRate {
			t.Fatalf("frame %d: SampleRate=%d, want %d", received, frame.SampleRate, cfg.SampleRate)
		}
		if frame.TimestampMs == 0 {
			t.Fatalf("frame %d: zero timestamp", received)
		}
		allZero := true
		for _, s := range frame.Samples {
			if s != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Fatalf("frame %d: all samples are zero", received)
		}
		received++
		if received >= wantFrames {
			cancel()
		}
	}
	if received < wantFrames {
		t.Fatalf("received %d frames, want >= %d", received, wantFrames)
	}
}

func TestReceiver_StartStream_ReplayReader(t *testing.T) {
	const numSamples = 2048
	buf := &bytes.Buffer{}
	for i := range numSamples {
		phase := 2 * math.Pi * float64(i) * 1000 / 48000
		re := float32(math.Cos(phase))
		im := float32(math.Sin(phase))
		binary.Write(buf, binary.LittleEndian, re)
		binary.Write(buf, binary.LittleEndian, im)
	}

	r := New(Options{Source: buf})

	cfg := sdr.StreamConfig{
		CenterHz:   145_900_000,
		SampleRate: 48000,
		BlockSize:  512,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := r.StartStream(ctx, cfg)
	if err != nil {
		t.Fatalf("StartStream error: %v", err)
	}

	totalSamples := 0
	for frame := range ch {
		if len(frame.Samples) != cfg.BlockSize {
			t.Fatalf("frame len=%d, want %d", len(frame.Samples), cfg.BlockSize)
		}
		totalSamples += len(frame.Samples)
	}

	if totalSamples != numSamples {
		t.Fatalf("total samples=%d, want %d", totalSamples, numSamples)
	}
}

func TestReceiver_Stop(t *testing.T) {
	r := New(Options{ToneOffsetHz: 1000})

	ctx := context.Background()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{
		CenterHz:   145_900_000,
		SampleRate: 48000,
		BlockSize:  256,
	})
	if err != nil {
		t.Fatalf("StartStream error: %v", err)
	}

	<-ch
	if stopErr := r.Stop(); stopErr != nil {
		t.Fatalf("Stop() error: %v", stopErr)
	}

	select {
	case _, ok := <-ch:
		if ok {
			for range ch {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after Stop()")
	}
}

func TestReceiver_ContextCancel(t *testing.T) {
	r := New(Options{ToneOffsetHz: 1000})

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := r.StartStream(ctx, sdr.StreamConfig{
		CenterHz:   145_900_000,
		SampleRate: 48000,
		BlockSize:  256,
	})
	if err != nil {
		t.Fatalf("StartStream error: %v", err)
	}

	<-ch
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			for range ch {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after context cancel")
	}
}
