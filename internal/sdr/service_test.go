package sdr

import "testing"

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
