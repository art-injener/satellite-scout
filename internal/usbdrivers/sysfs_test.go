package usbdrivers

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAttrs(t *testing.T, dir string, attrs map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, v := range attrs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestList_ReadsDevicesAndSkipsInterfaces(t *testing.T) {
	root := t.TempDir()
	writeAttrs(t, filepath.Join(root, "3-5"), map[string]string{
		"idVendor": "0bda", "idProduct": "2838", "busnum": "3", "devnum": "13",
		"serial": "00000001", "product": "RTL2838UHIDIR", "manufacturer": "Realtek",
	})
	// Interface of the same device: has no idVendor and must not be listed.
	writeAttrs(t, filepath.Join(root, "3-5:1.0"), map[string]string{"bInterfaceNumber": "00"})
	// Root hub without serial or product strings.
	writeAttrs(t, filepath.Join(root, "usb3"), map[string]string{
		"idVendor": "1d6b", "idProduct": "0002", "busnum": "3", "devnum": "1",
	})
	// Broken entry: unparsable vendor id.
	writeAttrs(t, filepath.Join(root, "4-1"), map[string]string{
		"idVendor": "zz", "idProduct": "0001", "busnum": "4", "devnum": "2",
	})

	got, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("devices=%+v, want the dongle and the root hub", got)
	}
	var rtl DeviceInfo
	for _, d := range got {
		if d.Name == "3-5" {
			rtl = d
		}
	}
	want := DeviceInfo{Name: "3-5", VendorID: 0x0bda, ProductID: 0x2838, Bus: 3, Address: 13,
		Serial: "00000001", Product: "RTL2838UHIDIR", Manufacturer: "Realtek"}
	if rtl != want {
		t.Errorf("dongle=%+v, want %+v", rtl, want)
	}
	if p := rtl.Path(); p != "/dev/bus/usb/003/013" {
		t.Errorf("Path()=%q, want /dev/bus/usb/003/013", p)
	}
}

func TestList_MissingRootIsAnError(t *testing.T) {
	if _, err := List(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("want an error for a missing sysfs root")
	}
}
