//go:build linux

package usbdrivers

import (
	"testing"
	"unsafe"
)

// Request codes as the kernel builds them from usbdevice_fs.h on 64-bit Linux
// (same values strace prints for libusb on amd64 and arm64).
func TestIoctlCodes_MatchKernel64(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("reference values are for 64-bit")
	}
	for name, tc := range map[string]struct{ got, want uintptr }{
		"CONTROL":          {ioctlControl, 0xc0185500},
		"BULK":             {ioctlBulk, 0xc0185502},
		"SUBMITURB":        {ioctlSubmitURB, 0x8038550a},
		"DISCARDURB":       {ioctlDiscardURB, 0x0000550b},
		"REAPURB":          {ioctlReapURB, 0x4008550c},
		"CLAIMINTERFACE":   {ioctlClaimInterface, 0x8004550f},
		"RELEASEINTERFACE": {ioctlReleaseInterface, 0x80045510},
		"DISCONNECT_CLAIM": {ioctlDisconnectClaim, 0x8108551b},
		"CLEAR_HALT":       {ioctlClearHalt, 0x80045515},
	} {
		if tc.got != tc.want {
			t.Errorf("USBDEVFS_%s=%#x, want %#x", name, tc.got, tc.want)
		}
	}
}

func TestKernelStructLayouts64(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("reference sizes are for 64-bit")
	}
	for name, tc := range map[string]struct{ got, want uintptr }{
		"usbdevfs_ctrltransfer":      {unsafe.Sizeof(ctrlTransfer{}), 24},
		"usbdevfs_bulktransfer":      {unsafe.Sizeof(bulkTransfer{}), 24},
		"usbdevfs_urb":               {unsafe.Sizeof(urb{}), 56},
		"usbdevfs_disconnect_claim":  {unsafe.Sizeof(disconnectClaim{}), 264},
		"usbdevfs_urb.buffer offset": {unsafe.Offsetof(urb{}.Buffer), 16},
		"usbdevfs_urb.usercontext":   {unsafe.Offsetof(urb{}.UserContext), 48},
		"usbdevfs_ctrltransfer.data": {unsafe.Offsetof(ctrlTransfer{}.Data), 16},
	} {
		if tc.got != tc.want {
			t.Errorf("%s=%d, want %d", name, tc.got, tc.want)
		}
	}
}
