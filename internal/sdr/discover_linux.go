//go:build linux

package sdr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Известные USB SDR: vendor:product → (driver, название).
var knownSDRDevices = map[string]struct {
	driver string
	label  string
}{
	"0bda:2838": {"rtlsdr", "RTL-SDR (RTL2838)"},
	"0bda:2832": {"rtlsdr", "RTL-SDR (RTL2832U)"},
	"1d50:6089": {"hackrf", "HackRF One"},
	"1d50:60a1": {"airspy", "Airspy R2"},
}

// discoverUSBDevices перечисляет SDR по sysfs (/sys/bus/usb/devices).
func discoverUSBDevices() []Device {
	const usbRoot = "/sys/bus/usb/devices"
	entries, err := os.ReadDir(usbRoot)
	if err != nil {
		return nil
	}

	var out []Device
	seen := make(map[string]struct{})
	for _, ent := range entries {
		name := ent.Name()
		// Интересуют только узлы вида bus-port (1-3), не 1-3:1.0.
		if strings.Contains(name, ":") {
			continue
		}
		base := filepath.Join(usbRoot, name)
		vendor, okV := readTrimmed(filepath.Join(base, "idVendor"))
		product, okP := readTrimmed(filepath.Join(base, "idProduct"))
		if !okV || !okP {
			continue
		}
		key := strings.ToLower(vendor) + ":" + strings.ToLower(product)
		meta, known := knownSDRDevices[key]
		if !known {
			continue
		}

		label := meta.label
		if prodName, ok := readTrimmed(filepath.Join(base, "product")); ok && prodName != "" {
			label = prodName
		}
		serial, _ := readTrimmed(filepath.Join(base, "serial"))
		busnum, _ := readTrimmed(filepath.Join(base, "busnum"))
		devnum, _ := readTrimmed(filepath.Join(base, "devnum"))
		devPath := ""
		if busnum != "" && devnum != "" {
			devPath = fmt.Sprintf("/dev/bus/usb/%s/%s", busnum, devnum)
		}

		dedupeKey := meta.driver + "|" + serial + "|" + name
		if _, exists := seen[dedupeKey]; exists {
			continue
		}
		seen[dedupeKey] = struct{}{}

		out = append(out, Device{
			Driver:     meta.driver,
			Label:      label,
			Serial:     serial,
			DevicePath: devPath,
		})
	}
	return out
}

func readTrimmed(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(b))
	return s, s != ""
}
