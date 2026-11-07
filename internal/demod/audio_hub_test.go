package demod

import (
	"slices"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

func TestAudioHub_DeliversToListenersOfThePath(t *testing.T) {
	h := NewAudioHub()
	a, unsubA := h.Subscribe("radio_path_0")
	defer unsubA()
	other, unsubOther := h.Subscribe("radio_path_1")
	defer unsubOther()

	h.Publish("radio_path_0", 1000, 436_000_000, []float32{0, 0.5, -1, 2, -3})

	select {
	case b := <-a:
		if b.TimestampMs != 1000 || b.FrequencyHz != 436_000_000 {
			t.Errorf("block=%+v, want the block time and channel frequency", b)
		}
		// Full scale is ±1; louder samples are clipped.
		if want := []int16{0, 16384, -32767, 32767, -32767}; !slices.Equal(b.Samples, want) {
			t.Errorf("samples=%v, want %v", b.Samples, want)
		}
	case <-time.After(time.Second):
		t.Fatal("listener got nothing")
	}
	select {
	case b := <-other:
		t.Errorf("listener of another path got %+v", b)
	default:
	}
}

func TestAudioHub_SlowListenerDoesNotBlock(t *testing.T) {
	h := NewAudioHub()
	ch, unsub := h.Subscribe("radio_path_0")
	defer unsub()
	done := make(chan struct{})
	go func() {
		for range 10 * audioListenerBuffer {
			h.Publish("radio_path_0", 0, 0, make([]float32, 100))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on a listener that does not read")
	}
	if len(ch) != audioListenerBuffer {
		t.Errorf("queued blocks=%d, want the buffer %d full", len(ch), audioListenerBuffer)
	}
}

func TestAudioHub_UnsubscribeClosesTheChannel(t *testing.T) {
	h := NewAudioHub()
	ch, unsub := h.Subscribe("radio_path_0")
	unsub()
	unsub()
	if _, ok := <-ch; ok {
		t.Error("channel open after unsubscribe")
	}
	h.Publish("radio_path_0", 0, 0, []float32{1})
}

func TestService_PublishesAudio(t *testing.T) {
	audio := NewAudioHub()
	listener, unsub := audio.Subscribe("radio_path_0")
	defer unsub()
	s := voiceSettings("fm", 15_000, -90)
	svc := NewService(&channelsStub{s: s}, (&sinkStub{}).emit).WithAudio(audio)

	frames := toFrames(fmToneAir(0.25), testRate, sdr.DefaultBlockSize, 0)
	ch := make(chan sdr.IQFrame, len(frames))
	for _, f := range frames {
		ch <- f
	}
	close(ch)
	go svc.Run("radio_path_0", ch)

	var got int
	for got < AudioRate/4*99/100 {
		select {
		case b := <-listener:
			if b.FrequencyHz != testCenter+testOffset {
				t.Fatalf("block frequency=%d, want the channel %d", b.FrequencyHz, testCenter+testOffset)
			}
			got += len(b.Samples)
		case <-time.After(5 * time.Second):
			t.Fatalf("audio samples=%d, want about %d for 0.25 s", got, AudioRate/4)
		}
	}
}
