//go:build !linux

package rtlsdr

import "errors"

func openUSB(Options) (usbDevice, error) {
	return nil, errors.New("rtlsdr: USB access is implemented only on Linux")
}
