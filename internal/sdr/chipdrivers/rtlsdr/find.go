package rtlsdr

import (
	"errors"
	"fmt"

	"github.com/art-injener/satellite-scout/internal/usbdrivers"
)

// ErrNotFound — подходящий донгл не подключён.
var ErrNotFound = errors.New("rtlsdr: dongle not found")

// dongleIDs — vendor:product донглов на RTL2832U.
var dongleIDs = [][2]uint16{
	{0x0bda, 0x2838},
	{0x0bda, 0x2832},
}

func isDongle(d usbdrivers.DeviceInfo) bool {
	for _, id := range dongleIDs {
		if d.VendorID == id[0] && d.ProductID == id[1] {
			return true
		}
	}
	return false
}

// findDongle ищет донгл в sysfs: по серийному номеру, иначе по пути, иначе первый.
// Путь после переподключения меняется, поэтому серийный номер важнее.
func findDongle(root string, o Options) (usbdrivers.DeviceInfo, error) {
	devs, err := usbdrivers.List(root)
	if err != nil {
		return usbdrivers.DeviceInfo{}, fmt.Errorf("rtlsdr: list USB: %w", err)
	}
	var first *usbdrivers.DeviceInfo
	for i := range devs {
		d := devs[i]
		if !isDongle(d) {
			continue
		}
		switch {
		case o.Serial != "":
			if d.Serial == o.Serial {
				return d, nil
			}
		case o.DevicePath != "":
			if d.Path() == o.DevicePath {
				return d, nil
			}
		default:
			if first == nil {
				first = &devs[i]
			}
		}
	}
	if first != nil {
		return *first, nil
	}
	switch {
	case o.Serial != "":
		return usbdrivers.DeviceInfo{}, fmt.Errorf("%w: serial %q", ErrNotFound, o.Serial)
	case o.DevicePath != "":
		return usbdrivers.DeviceInfo{}, fmt.Errorf("%w: %s", ErrNotFound, o.DevicePath)
	}
	return usbdrivers.DeviceInfo{}, ErrNotFound
}
