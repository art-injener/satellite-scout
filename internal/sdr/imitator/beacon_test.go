package imitator

import (
	"context"
	"errors"
	"math"
	"math/cmplx"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax100"
	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/usp"
)

func testBeacon() Signal {
	return Signal{
		Kind:        SignalBeacon,
		FrequencyHz: testCenter + 10_000,
		Frame: ax25.Frame{
			Dest: ax25.Address{Call: "CQ"},
			Src:  ax25.Address{Call: "RS0SIM"},
			Info: []byte("test beacon"),
		},
		Period: time.Second,
	}
}

func newTestBeaconGen(t *testing.T, sig Signal) *beaconGen {
	t.Helper()
	g, err := newBeaconGen(sig, testRate)
	if err != nil {
		t.Fatalf("newBeaconGen: %v", err)
	}
	return g
}

func TestBeacon_BurstThenSilenceThenBurst(t *testing.T) {
	g := newTestBeaconGen(t, testBeacon())
	burst := g.burstSamples()
	if burst <= 0 || burst >= int(testRate) {
		t.Fatalf("burst=%d samples, want within (0, period)", burst)
	}

	x := make([]complex64, int(testRate)+burst/2)
	g.fillClean(x)

	for i, v := range x[:burst] {
		if mag := cmplx.Abs(complex128(v)); math.Abs(mag-1) > 1e-4 {
			t.Fatalf("burst sample %d: |x|=%g, want 1", i, mag)
		}
	}
	if !isSilent(x[burst:int(testRate)]) {
		t.Error("expected silence between bursts")
	}
	if isSilent(x[int(testRate):]) {
		t.Error("expected next burst after period")
	}
}

func TestBeacon_SignalAtOffsetFromCenter(t *testing.T) {
	s := newTestSource(t, testBeacon())
	x := make([]complex64, 4800)
	s.fill(x, testCenter)
	if f := meanFreq(x); math.Abs(f-10_000) > 50 {
		t.Errorf("mean freq=%.1f Hz, want 10000", f)
	}
}

func TestBeacon_FSK9600BurstIsEightTimesShorter(t *testing.T) {
	afsk := newTestBeaconGen(t, testBeacon())
	sig := testBeacon()
	sig.Modem = BeaconFSK9600G3RUH
	fsk := newTestBeaconGen(t, sig)

	want := float64(afsk.burstSamples()) / 8
	if got := float64(fsk.burstSamples()); math.Abs(got-want) > 1 {
		t.Errorf("9600 burst=%v samples, want %v", got, want)
	}
	x := make([]complex64, fsk.burstSamples()+100)
	fsk.fillClean(x)
	if isSilent(x[:fsk.burstSamples()]) || !isSilent(x[fsk.burstSamples():]) {
		t.Error("expected 9600 burst followed by silence")
	}
}

func TestBeacon_UnknownModem(t *testing.T) {
	sig := testBeacon()
	sig.Modem = "qpsk_ccsds" // ещё нет в маяке имитатора
	if _, err := newSignalSource(sig, testRate); !errors.Is(err, ErrInvalidSignal) {
		t.Errorf("err=%v, want ErrInvalidSignal", err)
	}
}

func TestBeacon_GEOSCANFrameTooLong(t *testing.T) {
	sig := testBeacon()
	sig.Modem = BeaconFSK9600GEOSCAN
	// 16 bytes of addresses, control and PID leave 56 bytes of info in the long frame.
	sig.Frame.Info = make([]byte, 57)
	if _, err := newSignalSource(sig, testRate); !errors.Is(err, ErrInvalidSignal) {
		t.Errorf("err=%v, want ErrInvalidSignal", err)
	}
	sig.Frame.Info = sig.Frame.Info[:56]
	if _, err := newSignalSource(sig, testRate); err != nil {
		t.Errorf("56 bytes of info: %v", err)
	}
}

func TestBeacon_InvalidFrame(t *testing.T) {
	sig := testBeacon()
	sig.Frame.Src.Call = ""
	if _, err := newSignalSource(sig, testRate); !errors.Is(err, ax25.ErrInvalidCallsign) {
		t.Errorf("err=%v, want ErrInvalidCallsign", err)
	}
}

// Beacons of different formats differ by text: the telemetry table shows which one was received.
func TestDefaultBeaconFrame_NamesModem(t *testing.T) {
	cases := []struct {
		modem BeaconModem
		want  string
	}{
		{"", "Satellite Scout imitator beacon: AFSK 1200"},
		{BeaconAFSK1200, "Satellite Scout imitator beacon: AFSK 1200"},
		{BeaconFSK1200, "Satellite Scout imitator beacon: FSK 1200"},
		{BeaconFSK4800, "Satellite Scout imitator beacon: FSK 4800"},
		{BeaconGFSK1200, "Satellite Scout imitator beacon: GFSK 1200"},
		{BeaconGFSK4800, "Satellite Scout imitator beacon: GFSK 4800"},
		{BeaconGFSK9600, "Satellite Scout imitator beacon: GFSK 9600"},
		{BeaconGMSK9600, "Satellite Scout imitator beacon: GMSK 9600"},
		{BeaconGMSK4800, "Satellite Scout imitator beacon: GMSK 4800"},
		{BeaconBPSK1200, "Satellite Scout imitator beacon: BPSK 1200"},
		{BeaconBPSK9600, "Satellite Scout imitator beacon: BPSK 9600"},
		{BeaconFSK9600G3RUH, "Satellite Scout imitator beacon: FSK 9600 G3RUH"},
	}
	for _, c := range cases {
		if got := string(DefaultBeaconFrame(c.modem).Info); got != c.want {
			t.Errorf("modem %q: info %q, want %q", c.modem, got, c.want)
		}
	}
}

func TestDefaultBeaconFrame_GEOSCANCarriesBeaconTelemetry(t *testing.T) {
	params, ok := geoscan.BeaconParams(DefaultBeaconFrame(BeaconFSK9600GEOSCAN))
	if !ok {
		t.Fatal("GEOSCAN imitator frame is not a Geoscan beacon")
	}
	for _, p := range params {
		if p.ID == "eps_cell_voltage_full" && p.Value == "0,000" {
			t.Error("battery voltage is zero, want a plausible value")
		}
	}
}

func TestDefaultBeaconFrame_AX100CarriesSuchaiTelemetry(t *testing.T) {
	raw := DefaultBeaconFrame(BeaconGMSK4800AX100).Info
	params, sat, ok := ax100.SuchaiBeaconParams(raw)
	if !ok || sat != "SUCHAI-2" {
		t.Fatalf("ok=%v sat=%q", ok, sat)
	}
	for _, p := range params {
		if p.ID == "eps_vbatt" && p.Value == "0,00" {
			t.Error("battery voltage is zero")
		}
	}
}

func TestDefaultBeaconFrame_USPCarriesBeaconTelemetry(t *testing.T) {
	f := DefaultBeaconFrame(BeaconGMSK4800USP)
	raw, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	params, kind, ok := usp.BeaconParams(raw)
	if !ok || kind != "beacon" {
		t.Fatalf("ok=%v kind=%q", ok, kind)
	}
	found := false
	for _, p := range params {
		if p.ID == "uab" && p.Value != "0,0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("params=%v, want non-zero uab", params)
	}
}

func TestDefaultBeaconFrame_FitsDefaultPeriod(t *testing.T) {
	sig := testBeacon()
	sig.Frame = DefaultBeaconFrame(BeaconAFSK1200)
	sig.Period = DefaultBeaconPeriod
	rate := float64(New(Options{}).Capabilities().RecommendedSampleRate)
	g, err := newBeaconGen(sig, rate)
	if err != nil {
		t.Fatalf("newBeaconGen: %v", err)
	}
	if burst := float64(g.burstSamples()) / rate; burst >= sig.Period.Seconds() {
		t.Errorf("burst %.2f s does not fit into period %v", burst, sig.Period)
	}
}

func TestReceiver_StartStream_InvalidBeacon(t *testing.T) {
	sig := testBeacon()
	sig.Frame.Dest.SSID = 16
	r := New(Options{Signals: []Signal{sig}})
	_, err := r.StartStream(context.Background(), sdr.StreamConfig{SampleRate: testRate})
	if !errors.Is(err, ax25.ErrInvalidSSID) {
		t.Fatalf("err=%v, want ErrInvalidSSID", err)
	}
	if r.Status().Running {
		t.Error("receiver must not be running after failed start")
	}
}
