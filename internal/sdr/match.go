package sdr

import (
	"errors"
	"fmt"
)

var (
	// ErrDeviceNotFound — подходящее устройство не подключено.
	ErrDeviceNotFound = errors.New("sdr: device not found")
	// ErrDeviceAmbiguous — серийный номер есть у нескольких устройств, а сохранённый путь ни на одно не указывает.
	ErrDeviceAmbiguous = errors.New("sdr: several devices share the serial number")
)

// MatchDevice выбирает устройство тракта среди devs одного драйвера и возвращает его индекс.
// Путь после переподключения меняется, а серийный номер у донглов бывает одинаковым. Поэтому путь
// берётся, если по нему стоит устройство с тем же серийным номером, иначе — единственное устройство
// с этим номером. Несколько таких — ошибка: молча открыть донгл с чужой антенной хуже.
// Без серийного номера и пути берётся первое устройство.
func MatchDevice(devs []Device, serial, path string) (int, error) {
	var candidates []int
	for i, d := range devs {
		if serial == "" || d.Serial == serial {
			candidates = append(candidates, i)
		}
	}
	if path != "" {
		for _, i := range candidates {
			if devs[i].DevicePath == path {
				return i, nil
			}
		}
		if serial == "" {
			return 0, fmt.Errorf("%w: %s", ErrDeviceNotFound, path)
		}
	}
	switch {
	case len(candidates) == 0 && serial != "":
		return 0, fmt.Errorf("%w: serial %q", ErrDeviceNotFound, serial)
	case len(candidates) == 0:
		return 0, ErrDeviceNotFound
	case len(candidates) > 1 && serial != "":
		return 0, fmt.Errorf("%w: %d devices with serial %q, path %q not found", ErrDeviceAmbiguous,
			len(candidates), serial, path)
	}
	return candidates[0], nil
}
