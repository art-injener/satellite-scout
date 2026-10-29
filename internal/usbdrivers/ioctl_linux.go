//go:build linux

package usbdrivers

import "unsafe"

// Структуры usbdevice_fs.h один в один: Go выравнивает поля так же, как C, поэтому размеры
// совпадают и на 64-, и на 32-битных платформах. Указатели на буферы — uintptr: ядро читает
// и пишет их только внутри вызова ioctl, а куча Go объекты не перемещает.

// ctrlTransfer — struct usbdevfs_ctrltransfer.
type ctrlTransfer struct {
	RequestType uint8
	Request     uint8
	Value       uint16
	Index       uint16
	Length      uint16
	Timeout     uint32 // мс
	Data        uintptr
}

// bulkTransfer — struct usbdevfs_bulktransfer.
type bulkTransfer struct {
	Endpoint uint32
	Length   uint32
	Timeout  uint32 // мс
	Data     uintptr
}

// urb — struct usbdevfs_urb без кадров изохронной передачи.
type urb struct {
	Type            uint8
	Endpoint        uint8
	Status          int32 // 0 или отрицательный код ошибки
	Flags           uint32
	Buffer          uintptr
	BufferLength    int32
	ActualLength    int32
	StartFrame      int32
	NumberOfPackets int32
	ErrorCount      int32
	Signr           uint32
	UserContext     uintptr
}

// disconnectClaim — struct usbdevfs_disconnect_claim.
type disconnectClaim struct {
	Interface uint32
	Flags     uint32
	Driver    [256]byte
}

const (
	urbTypeBulk = 3

	// disconnectClaimExceptDriver: отцепить любой драйвер, кроме названного в Driver.
	disconnectClaimExceptDriver = 0x02
)

// Кодирование номера ioctl — общее для amd64, arm64, arm, 386 и riscv64:
// направление в битах 30–31, размер аргумента в 16–29, тип в 8–15, номер в 0–7.
const (
	iocNone  = 0
	iocWrite = 1
	iocRead  = 2
	iocUSB   = 'U'
)

func ioc(dir, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | iocUSB<<8 | nr
}

var (
	ioctlControl          = ioc(iocRead|iocWrite, 0, unsafe.Sizeof(ctrlTransfer{}))
	ioctlBulk             = ioc(iocRead|iocWrite, 2, unsafe.Sizeof(bulkTransfer{}))
	ioctlSubmitURB        = ioc(iocRead, 10, unsafe.Sizeof(urb{}))
	ioctlDiscardURB       = ioc(iocNone, 11, 0)
	ioctlReapURB          = ioc(iocWrite, 12, unsafe.Sizeof(uintptr(0)))
	ioctlClaimInterface   = ioc(iocRead, 15, unsafe.Sizeof(uint32(0)))
	ioctlReleaseInterface = ioc(iocRead, 16, unsafe.Sizeof(uint32(0)))
	ioctlClearHalt        = ioc(iocRead, 21, unsafe.Sizeof(uint32(0)))
	ioctlDisconnectClaim  = ioc(iocRead, 27, unsafe.Sizeof(disconnectClaim{}))
)
