//go:build linux

package usbdrivers

import (
	"fmt"
	"math"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// Device — открытый узел устройства в usbfs (/dev/bus/usb/BBB/DDD).
type Device struct {
	fd   int
	path string
}

// Open открывает устройство на чтение и запись. Нужны права на узел: правило udev
// или ACL сеанса пользователя.
func Open(path string) (*Device, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("usb open %s: %w", path, err)
	}
	return &Device{fd: fd, path: path}, nil
}

// Close закрывает устройство; ядро само отпускает захваченные интерфейсы.
// Открытые потоки закрываются до него.
func (d *Device) Close() error {
	if d.fd < 0 {
		return nil
	}
	err := syscall.Close(d.fd)
	d.fd = -1
	return err
}

// ClaimInterface захватывает интерфейс, отцепив от него драйвер ядра (у RTL-SDR —
// dvb_usb_rtl28xxu). Интерфейс, уже захваченный через usbfs другой программой, не отбирается:
// тогда ошибка EBUSY. Драйвер ядра обратно не возвращается — до переподключения донгла.
func (d *Device) ClaimInterface(n uint32) error {
	dc := disconnectClaim{Interface: n, Flags: disconnectClaimExceptDriver}
	copy(dc.Driver[:], "usbfs")
	if _, err := d.ioctl(ioctlDisconnectClaim, unsafe.Pointer(&dc)); err != nil {
		return fmt.Errorf("usb claim interface %d of %s: %w", n, d.path, err)
	}
	return nil
}

// ReleaseInterface отпускает захваченный интерфейс.
func (d *Device) ReleaseInterface(n uint32) error {
	if _, err := d.ioctl(ioctlReleaseInterface, unsafe.Pointer(&n)); err != nil {
		return fmt.Errorf("usb release interface %d: %w", n, err)
	}
	return nil
}

// ClearHalt снимает остановку с конечной точки: после STALL от устройства передачи
// на неё отвергаются с EPIPE, пока остановку не снять на обеих сторонах.
func (d *Device) ClearHalt(endpoint uint8) error {
	ep := uint32(endpoint)
	if _, err := d.ioctl(ioctlClearHalt, unsafe.Pointer(&ep)); err != nil {
		return fmt.Errorf("usb clear halt %#02x: %w", endpoint, err)
	}
	return nil
}

// Control выполняет управляющую передачу. Направление задаёт старший бит requestType:
// с ним данные читаются из устройства в data, без него пишутся из data.
// Возвращает число переданных байт. Нулевой timeout — ждать без ограничения.
func (d *Device) Control(requestType, request uint8, value, index uint16, data []byte, timeout time.Duration) (int, error) {
	if len(data) > math.MaxUint16 {
		return 0, fmt.Errorf("usb control: %d bytes, at most %d", len(data), math.MaxUint16)
	}
	ct := ctrlTransfer{
		RequestType: requestType, Request: request, Value: value, Index: index,
		Length: uint16(len(data)), Timeout: millis(timeout),
	}
	var pin runtime.Pinner
	defer pin.Unpin()
	if len(data) > 0 {
		// Адрес буфера лежит внутри структуры, о нём знает только ядро: буфер закрепляется.
		pin.Pin(&data[0])
		ct.Data = uintptr(unsafe.Pointer(&data[0]))
	}
	n, err := d.ioctl(ioctlControl, unsafe.Pointer(&ct))
	if err != nil {
		return 0, fmt.Errorf("usb control %#02x/%#02x value %#04x index %#04x: %w", requestType, request, value, index, err)
	}
	return n, nil
}

// BulkRead читает одну потоковую передачу с конечной точки endpoint (с битом 0x80).
// Для непрерывного приёма — Stream.
func (d *Device) BulkRead(endpoint uint8, buf []byte, timeout time.Duration) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	var pin runtime.Pinner
	defer pin.Unpin()
	pin.Pin(&buf[0])
	bt := bulkTransfer{Endpoint: uint32(endpoint), Length: uint32(len(buf)), Timeout: millis(timeout),
		Data: uintptr(unsafe.Pointer(&buf[0]))}
	n, err := d.ioctl(ioctlBulk, unsafe.Pointer(&bt))
	if err != nil {
		return 0, fmt.Errorf("usb bulk read %#02x: %w", endpoint, err)
	}
	return n, nil
}

// Stream запускает непрерывное чтение с конечной точки: buffers передач по size байт.
func (d *Device) Stream(endpoint uint8, buffers, size int) (*Stream, error) {
	if buffers <= 0 || size <= 0 {
		return nil, fmt.Errorf("usb stream: %d buffers of %d bytes", buffers, size)
	}
	q := &urbQueue{dev: d, urbs: make([]urb, buffers), addr: make([]uintptr, buffers)}
	// Ядро помнит адреса передач и буферов от отправки до сбора: всё закреплено до закрытия потока.
	q.pin.Pin(&q.urbs[0])
	bufs := make([][]byte, buffers)
	for i := range bufs {
		bufs[i] = make([]byte, size)
		q.pin.Pin(&bufs[i][0])
		q.urbs[i] = urb{Type: urbTypeBulk, Endpoint: endpoint,
			Buffer: uintptr(unsafe.Pointer(&bufs[i][0])), BufferLength: int32(size)}
		q.addr[i] = uintptr(unsafe.Pointer(&q.urbs[i]))
	}
	return startStream(q, bufs)
}

// urbQueue — передачи потока в ядре.
type urbQueue struct {
	dev  *Device
	urbs []urb
	addr []uintptr // адрес urbs[i]: его ядро возвращает из REAPURB
	pin  runtime.Pinner
}

func (q *urbQueue) submit(i int) error {
	q.urbs[i].Status, q.urbs[i].ActualLength = 0, 0
	_, err := q.dev.ioctl(ioctlSubmitURB, unsafe.Pointer(&q.urbs[i]))
	return err
}

func (q *urbQueue) discard(i int) error {
	_, err := q.dev.ioctl(ioctlDiscardURB, unsafe.Pointer(&q.urbs[i]))
	return err
}

func (q *urbQueue) reap() (int, int32, int, error) {
	var p uintptr
	if _, err := q.dev.ioctl(ioctlReapURB, unsafe.Pointer(&p)); err != nil {
		return 0, 0, 0, err
	}
	for i, a := range q.addr {
		if a == p {
			return i, q.urbs[i].Status, int(q.urbs[i].ActualLength), nil
		}
	}
	return 0, 0, 0, fmt.Errorf("usb reap: unknown urb %#x", p)
}

func (q *urbQueue) release() { q.pin.Unpin() }

// ioctl повторяет вызов, прерванный сигналом: среда Go шлёт их потокам для вытеснения горутин.
// Указатель превращается в число прямо в аргументах Syscall: так объект не переедет до конца вызова.
func (d *Device) ioctl(req uintptr, arg unsafe.Pointer) (int, error) {
	for {
		r, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(d.fd), req, uintptr(arg))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return 0, errno
		}
		return int(r), nil
	}
}

func millis(t time.Duration) uint32 {
	return uint32(min(t.Milliseconds(), math.MaxUint32))
}
