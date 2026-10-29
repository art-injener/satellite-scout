package sdr

import (
	"errors"
	"strings"
	"testing"
)

func serviceWithDongle(p Probe) *Service {
	svc := NewService().WithProbe("rtlsdr", p)
	svc.discover = func() []Device {
		return []Device{{Driver: "rtlsdr", Label: "RTL2838UHIDIR", Serial: "00000001", DevicePath: "/dev/bus/usb/003/013"}}
	}
	return svc
}

func TestService_TestRunsDriverProbe(t *testing.T) {
	var gotSerial, gotPath string
	svc := serviceWithDongle(func(serial, path string) (ProbeReport, error) {
		gotSerial, gotPath = serial, path
		return ProbeReport{
			Tuner: "R820T", MinFreqHz: 24_100_000, MaxFreqHz: 1_766_000_000,
			CenterHz: 436_500_000, SampleRate: 2_048_000, GainDB: -1, Samples: 256,
		}, nil
	})
	resp := svc.Test(TestRequest{Driver: "rtlsdr", Serial: "00000001"})
	if !resp.OK {
		t.Fatalf("probe failed: %v", resp.Lines)
	}
	if gotSerial != "00000001" || gotPath != "/dev/bus/usb/003/013" {
		t.Errorf("probe opened serial %q path %q", gotSerial, gotPath)
	}
	text := strings.Join(resp.Lines, "\n")
	for _, want := range []string{"R820T", "24.1 – 1766 МГц", "2.048 MS/s", "436.500 МГц", "АРУ", "256 сэмплов", "Итог: устройство готово"} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
}

func TestService_TestReportsProbeFailure(t *testing.T) {
	svc := serviceWithDongle(func(string, string) (ProbeReport, error) {
		return ProbeReport{}, errors.New("device or resource busy")
	})
	resp := svc.Test(TestRequest{Driver: "rtlsdr", Serial: "00000001"})
	if resp.OK {
		t.Fatal("probe failure reported as OK")
	}
	if text := strings.Join(resp.Lines, "\n"); !strings.Contains(text, "device or resource busy") {
		t.Errorf("error not shown:\n%s", text)
	}
}

func TestService_ListDevices_IncludesSimulated(t *testing.T) {
	svc := NewService()
	resp := svc.ListDevices()
	if len(resp.Devices) == 0 {
		t.Fatal("expected at least simulated device")
	}
	if resp.Devices[0].Driver != "simulated" {
		t.Errorf("first device driver = %q, want simulated", resp.Devices[0].Driver)
	}
}

func TestService_TestSimulated(t *testing.T) {
	svc := NewService()
	resp := svc.Test(TestRequest{Driver: "simulated"})
	if !resp.OK {
		t.Fatalf("expected ok, lines=%v", resp.Lines)
	}
	if len(resp.Lines) < 3 {
		t.Errorf("expected detailed lines, got %v", resp.Lines)
	}
}

func TestService_TestUnknownDriver(t *testing.T) {
	svc := NewService()
	resp := svc.Test(TestRequest{Driver: "unknown"})
	if resp.OK {
		t.Error("expected failure for unknown driver")
	}
}
