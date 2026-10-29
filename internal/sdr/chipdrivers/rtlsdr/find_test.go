package rtlsdr

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeSysfsDevice(t *testing.T, root, name string, attrs map[string]string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for k, v := range attrs {
		if err := os.WriteFile(filepath.Join(dir, k), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func sysfsWithDongles(t *testing.T) string {
	root := t.TempDir()
	writeSysfsDevice(t, root, "1-1", map[string]string{"idVendor": "046d", "idProduct": "c52b", "busnum": "1", "devnum": "2"})
	writeSysfsDevice(t, root, "3-5", map[string]string{
		"idVendor": "0bda", "idProduct": "2838", "busnum": "3", "devnum": "13", "serial": "00000001",
	})
	writeSysfsDevice(t, root, "3-6", map[string]string{
		"idVendor": "0bda", "idProduct": "2832", "busnum": "3", "devnum": "14", "serial": "00000002",
	})
	return root
}

func TestFindDongle(t *testing.T) {
	root := sysfsWithDongles(t)
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{"by serial", Options{Serial: "00000002", DevicePath: "/dev/bus/usb/003/013"}, "/dev/bus/usb/003/014"},
		{"by path", Options{DevicePath: "/dev/bus/usb/003/014"}, "/dev/bus/usb/003/014"},
		{"first dongle", Options{}, "/dev/bus/usb/003/013"},
	}
	for _, tc := range cases {
		d, err := findDongle(root, tc.opts)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if d.Path() != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, d.Path(), tc.want)
		}
	}

	for _, o := range []Options{{Serial: "missing"}, {DevicePath: "/dev/bus/usb/001/002"}} {
		if _, err := findDongle(root, o); !errors.Is(err, ErrNotFound) {
			t.Errorf("%+v: err = %v, want ErrNotFound", o, err)
		}
	}
}
