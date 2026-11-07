package rtlsdr

import (
	"fmt"

	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/usbdrivers"
)

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

// findDongle ищет донгл в sysfs при каждом открытии по правилу sdr.MatchDevice: путь из конфига
// после переподключения устаревает, а серийный номер у донглов бывает одинаковым.
func findDongle(root string, o Options) (usbdrivers.DeviceInfo, error) {
	devs, err := usbdrivers.List(root)
	if err != nil {
		return usbdrivers.DeviceInfo{}, fmt.Errorf("rtlsdr: list USB: %w", err)
	}
	var dongles []usbdrivers.DeviceInfo
	var candidates []sdr.Device
	for _, d := range devs {
		if isDongle(d) {
			dongles = append(dongles, d)
			candidates = append(candidates, sdr.Device{Serial: d.Serial, DevicePath: d.Path()})
		}
	}
	i, err := sdr.MatchDevice(candidates, o.Serial, o.DevicePath)
	if err != nil {
		return usbdrivers.DeviceInfo{}, fmt.Errorf("rtlsdr: %w", err)
	}
	return dongles[i], nil
}
