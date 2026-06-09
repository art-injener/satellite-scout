//go:build !linux

package sdr

// discoverUSBDevices — на не-Linux перечисление USB пока не реализовано.
func discoverUSBDevices() []Device {
	return nil
}
