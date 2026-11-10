package handlers

import (
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

func TestIQ_MissingOrUnknownRadioPath(t *testing.T) {
	h := NewStreamHandler(sdr.NewReceiverHub())
	for _, url := range []string{"/api/stream/v1/iq", "/api/stream/v1/iq?radio_path=nope"} {
		rec := httptest.NewRecorder()
		h.IQ(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d, want 400", url, rec.Code)
		}
	}
}

func TestIQ_StreamsComplex64Frames(t *testing.T) {
	hub := sdr.NewReceiverHub()
	hub.Register("p", sdr.PathConfig{}, func() (sdr.Receiver, error) { return &liveReceiver{}, nil })

	srv := httptest.NewServer(http.HandlerFunc(NewStreamHandler(hub).IQ))
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
	if _, err = io.ReadFull(resp.Body, hdr); err != nil {
		t.Fatalf("read header: %v", err)
	}
	if dsp.DataType(hdr[5]) != dsp.DataTypeIQC64 {
		t.Fatalf("datatype=%d, want IQ cf32", hdr[5])
	}
	if center := binary.LittleEndian.Uint64(hdr[16:24]); center != 1_024_000/2 {
		t.Errorf("center_hz=%d, want the receiver center", center)
	}
	if span := binary.LittleEndian.Uint64(hdr[24:32]); span != 1_024_000 {
		t.Errorf("span_hz=%d, want the sample rate", span)
	}
	plen := binary.LittleEndian.Uint32(hdr[32:36])
	if plen != sdr.DefaultBlockSize*8 {
		t.Fatalf("payload %d bytes, want %d complex64 samples", plen, sdr.DefaultBlockSize)
	}
	payload := make([]byte, plen)
	if _, err = io.ReadFull(resp.Body, payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
}
