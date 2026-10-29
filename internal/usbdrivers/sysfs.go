// Package usbdrivers — USB-транспорт на usbfs ядра Linux без CGO и libusb: перечисление устройств
// по sysfs, захват интерфейса, управляющие и потоковые передачи через ioctl из usbdevice_fs.h.
// Протокол конкретного чипа сюда не входит — он в internal/sdr/chipdrivers.
package usbdrivers

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SysfsRoot — каталог устройств USB в sysfs.
const SysfsRoot = "/sys/bus/usb/devices"

// DeviceInfo — устройство USB, каким его видит sysfs.
type DeviceInfo struct {
	Name         string // имя узла в sysfs, например 3-5
	VendorID     uint16
	ProductID    uint16
	Bus          int
	Address      int // номер устройства на шине
	Serial       string
	Product      string
	Manufacturer string
}

// Path — узел устройства в usbfs, который открывает Open.
func (d DeviceInfo) Path() string {
	return fmt.Sprintf("/dev/bus/usb/%03d/%03d", d.Bus, d.Address)
}

// List перечисляет устройства в root (обычно SysfsRoot). Интерфейсы (узлы вида 3-5:1.0)
// и записи с нечитаемыми идентификаторами пропускаются.
func List(root string) ([]DeviceInfo, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []DeviceInfo
	for _, e := range entries {
		if strings.Contains(e.Name(), ":") {
			continue
		}
		if d, ok := readDevice(filepath.Join(root, e.Name())); ok {
			d.Name = e.Name()
			out = append(out, d)
		}
	}
	return out, nil
}

func readDevice(dir string) (DeviceInfo, bool) {
	var d DeviceInfo
	vendor, okV := readHex16(filepath.Join(dir, "idVendor"))
	product, okP := readHex16(filepath.Join(dir, "idProduct"))
	bus, okB := readInt(filepath.Join(dir, "busnum"))
	addr, okA := readInt(filepath.Join(dir, "devnum"))
	if !okV || !okP || !okB || !okA {
		return d, false
	}
	d.VendorID, d.ProductID, d.Bus, d.Address = vendor, product, bus, addr
	d.Serial = readAttr(filepath.Join(dir, "serial"))
	d.Product = readAttr(filepath.Join(dir, "product"))
	d.Manufacturer = readAttr(filepath.Join(dir, "manufacturer"))
	return d, true
}

func readAttr(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readHex16(path string) (uint16, bool) {
	v, err := strconv.ParseUint(readAttr(path), 16, 16)
	return uint16(v), err == nil
}

func readInt(path string) (int, bool) {
	v, err := strconv.Atoi(readAttr(path))
	return v, err == nil
}
