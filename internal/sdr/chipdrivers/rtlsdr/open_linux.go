//go:build linux

package rtlsdr

import (
	"github.com/art-injener/satellite-scout/internal/usbdrivers"
)

// linuxDongle — донгл через usbfs с захваченным интерфейсом 0.
type linuxDongle struct {
	*usbdrivers.Device
}

func openUSB(o Options) (usbDevice, error) {
	info, err := findDongle(usbdrivers.SysfsRoot, o)
	if err != nil {
		return nil, err
	}
	d, err := usbdrivers.Open(info.Path())
	if err != nil {
		return nil, err
	}
	// Драйвер DVB ядра отцепляется и обратно не возвращается до переподключения донгла.
	if err := d.ClaimInterface(0); err != nil {
		_ = d.Close()
		return nil, err
	}
	return linuxDongle{d}, nil
}

func (d linuxDongle) OpenStream(endpoint uint8, buffers, size int) (bulkStream, error) {
	s, err := d.Stream(endpoint, buffers, size)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d linuxDongle) Close() error {
	_ = d.ReleaseInterface(0)
	return d.Device.Close()
}
