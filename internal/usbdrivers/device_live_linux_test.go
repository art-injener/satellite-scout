//go:build linux

package usbdrivers

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

// Live check on an RTL-SDR dongle. It detaches the DVB kernel driver until the dongle is
// replugged, so it runs only with SCOUT_USB_LIVE=1.
const (
	rtlVendor  = 0x0bda
	rtlProduct = 0x2838
	rtlBulkIn  = 0x81

	// RTL2832U vendor requests: register read is IN with index = block << 8,
	// write is OUT with index = block << 8 | 0x10.
	rtlReadReq  = 0xc0
	rtlWriteReq = 0x40
	rtlBlockUSB = 1
	rtlSysctl   = 0x2000
	rtlEPACtl   = 0x2148 // endpoint A control: 0x1000 stalls it, 0x0002 resets the FIFO
)

// unstall takes the stall the DVB driver leaves on endpoint A off the chip and the host side.
func unstall(t *testing.T, dev *Device) {
	t.Helper()
	if _, err := dev.Control(rtlWriteReq, 0, rtlEPACtl, rtlBlockUSB<<8|0x10, []byte{0, 0}, 300*time.Millisecond); err != nil {
		t.Fatalf("write USB_EPA_CTL: %v", err)
	}
	if err := dev.ClearHalt(rtlBulkIn); err != nil {
		t.Fatal(err)
	}
}

func openLiveRTL(t *testing.T) *Device {
	t.Helper()
	if os.Getenv("SCOUT_USB_LIVE") != "1" {
		t.Skip("set SCOUT_USB_LIVE=1 with an RTL-SDR plugged in")
	}
	devs, err := List(SysfsRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devs {
		if d.VendorID == rtlVendor && d.ProductID == rtlProduct {
			t.Logf("dongle %s at %s: %s %s serial %s", d.Name, d.Path(), d.Manufacturer, d.Product, d.Serial)
			dev, err := Open(d.Path())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = dev.Close() })
			if err := dev.ClaimInterface(0); err != nil {
				t.Fatal(err)
			}
			return dev
		}
	}
	t.Skip("no RTL-SDR (0bda:2838) found")
	return nil
}

func TestLive_ControlReadsRTLRegister(t *testing.T) {
	dev := openLiveRTL(t)
	buf := make([]byte, 1)
	n, err := dev.Control(rtlReadReq, 0, rtlSysctl, rtlBlockUSB<<8, buf, 300*time.Millisecond)
	if err != nil || n != 1 {
		t.Fatalf("read USB_SYSCTL: %d bytes, %v", n, err)
	}
	t.Logf("USB_SYSCTL = %#02x", buf[0])
}

// Without the chip set up for streaming the endpoint may stay silent: a timeout proves
// the bulk call itself is well formed, a wrong layout would give EINVAL.
func TestLive_BulkReadIsAccepted(t *testing.T) {
	dev := openLiveRTL(t)
	unstall(t, dev)
	buf := make([]byte, 512)
	n, err := dev.BulkRead(rtlBulkIn, buf, 200*time.Millisecond)
	if err != nil && !errors.Is(err, syscall.ETIMEDOUT) {
		t.Fatalf("bulk read: %v, want data or ETIMEDOUT", err)
	}
	t.Logf("bulk read: %d bytes, %v", n, err)
}

func TestLive_StreamStartsAndCloses(t *testing.T) {
	dev := openLiveRTL(t)
	unstall(t, dev)
	s, err := dev.Stream(rtlBulkIn, 4, 16384)
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan error, 1)
	go func() {
		_, err := s.Read()
		read <- err
	}()
	time.Sleep(300 * time.Millisecond)
	closed := make(chan struct{})
	go func() { _ = s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return: discarded URBs were not reaped")
	}
	if err := <-read; err != nil && !errors.Is(err, ErrClosed) {
		t.Errorf("Read=%v, want data or ErrClosed", err)
	}
}
