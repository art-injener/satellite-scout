package sdr

import (
	"errors"
	"testing"
)

// twinDongles: two dongles share the factory serial, a third one has its own.
var twinDongles = []Device{
	{Driver: "rtlsdr", Serial: "00000001", DevicePath: "/dev/bus/usb/003/013"},
	{Driver: "rtlsdr", Serial: "00000001", DevicePath: "/dev/bus/usb/003/014"},
	{Driver: "rtlsdr", Serial: "00000002", DevicePath: "/dev/bus/usb/003/015"},
}

func TestMatchDevice_Picks(t *testing.T) {
	cases := []struct {
		name         string
		serial, path string
		want         int
	}{
		{"path tells twins apart", "00000001", "/dev/bus/usb/003/014", 1},
		{"unique serial survives a stale path", "00000002", "/dev/bus/usb/003/099", 2},
		{"serial wins over a path of another serial", "00000002", "/dev/bus/usb/003/013", 2},
		{"unique serial without path", "00000002", "", 2},
		{"path without serial", "", "/dev/bus/usb/003/014", 1},
		{"nothing saved takes the first", "", "", 0},
	}
	for _, tc := range cases {
		got, err := MatchDevice(twinDongles, tc.serial, tc.path)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: device %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestMatchDevice_TwinsWithStalePathAreAmbiguous(t *testing.T) {
	for _, path := range []string{"/dev/bus/usb/003/099", ""} {
		if _, err := MatchDevice(twinDongles, "00000001", path); !errors.Is(err, ErrDeviceAmbiguous) {
			t.Errorf("path %q: err = %v, want ErrDeviceAmbiguous", path, err)
		}
	}
}

func TestMatchDevice_NotFound(t *testing.T) {
	cases := []struct{ serial, path string }{
		{"missing", ""},
		{"missing", "/dev/bus/usb/003/013"},
		{"", "/dev/bus/usb/001/002"},
	}
	for _, tc := range cases {
		if _, err := MatchDevice(twinDongles, tc.serial, tc.path); !errors.Is(err, ErrDeviceNotFound) {
			t.Errorf("%+v: err = %v, want ErrDeviceNotFound", tc, err)
		}
	}
	if _, err := MatchDevice(nil, "", ""); !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("no devices: err = %v, want ErrDeviceNotFound", err)
	}
}
