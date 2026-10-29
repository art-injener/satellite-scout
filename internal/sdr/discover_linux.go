//go:build linux

package sdr

import (
	"fmt"

	"github.com/art-injener/satellite-scout/internal/usbdrivers"
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
	devs, err := usbdrivers.List(usbdrivers.SysfsRoot)
	if err != nil {
		return nil
	}
	var out []Device
	for _, d := range devs {
		meta, known := knownSDRDevices[fmt.Sprintf("%04x:%04x", d.VendorID, d.ProductID)]
		if !known {
			continue
		}
		label := meta.label
		if d.Product != "" {
			label = d.Product
		}
		out = append(out, Device{
			Driver:     meta.driver,
			Label:      label,
			Serial:     d.Serial,
			DevicePath: d.Path(),
		})
	}
	return out
}
