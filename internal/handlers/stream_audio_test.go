package handlers

import (
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-injener/satellite-scout/internal/demod"
	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

func TestAudio_MissingOrUnknownRadioPath(t *testing.T) {
	hub := sdr.NewReceiverHub()
	h := NewStreamHandler(hub).WithAudio(demod.NewAudioHub())
	for _, url := range []string{"/api/stream/v1/audio", "/api/stream/v1/audio?radio_path=nope"} {
		rec := httptest.NewRecorder()
		h.Audio(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d, want 400", url, rec.Code)
		}
	}
}

func TestAudio_UnavailableWithoutAudioHub(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stream/v1/audio?radio_path=p", nil)
	NewStreamHandler(sdr.NewReceiverHub()).Audio(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503", rec.Code)
	}
}

// The listener alone keeps the device stream: no spectrum viewer is open.
func TestAudio_StreamsPCMFramesOfTheChannel(t *testing.T) {
	hub := sdr.NewReceiverHub()
	receiver := &liveReceiver{}
	hub.Register("p", sdr.PathConfig{}, func() (sdr.Receiver, error) { return receiver, nil })
	audio := demod.NewAudioHub()
	hub.SetStreamTap(demod.NewService(hub, func(string, []byte) {}).WithAudio(audio).Run)
	ch := sdr.ChannelSettings{OffsetHz: 50_000, BandwidthHz: 15_000, SquelchDBFS: -90, Demod: "fm"}
	if err := hub.SetChannel("p", ch); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(NewStreamHandler(hub).WithAudio(audio).Audio))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "?radio_path=p")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	hdr := make([]byte, dsp.HeaderSize)
	for {
		if _, err = io.ReadFull(resp.Body, hdr); err != nil {
			t.Fatalf("read header: %v", err)
		}
		payload := make([]byte, binary.LittleEndian.Uint32(hdr[32:36]))
		if _, err = io.ReadFull(resp.Body, payload); err != nil {
			t.Fatalf("read payload: %v", err)
		}
		if dsp.DataType(hdr[5]) != dsp.DataTypePCMI16 {
			continue
		}
		// liveReceiver is tuned to half its sample rate.
		if center := binary.LittleEndian.Uint64(hdr[16:24]); center != 1_024_000/2+50_000 {
			t.Errorf("center_hz=%d, want the channel frequency", center)
		}
		if span := binary.LittleEndian.Uint64(hdr[24:32]); span != demod.AudioRate {
			t.Errorf("span_hz=%d, want the audio rate %d", span, demod.AudioRate)
		}
		if n := len(payload); n%2 != 0 || n < 2*audioChunkSamples {
			t.Errorf("payload %d bytes, want int16 samples of at least %d", n, audioChunkSamples)
		}
		return
	}
}
