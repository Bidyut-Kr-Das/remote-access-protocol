//go:build windows

package input

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// win32 INPUT on amd64: 4-byte type, 4 pad, 32-byte union = 40 bytes.
type winInput struct {
	Type  uint32
	_     [4]byte
	Union [32]byte
}

type mouseInput struct {
	dx, dy      int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type keybdInput struct {
	wVk, wScan  uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

var (
	user32       = windows.NewLazySystemDLL("user32.dll")
	procSendInp  = user32.NewProc("SendInput")
	procMapVirtK = user32.NewProc("MapVirtualKeyW")
)

func init() {
	if unsafe.Sizeof(winInput{}) != 40 || unsafe.Sizeof(mouseInput{}) > 32 || unsafe.Sizeof(keybdInput{}) > 32 {
		panic("input: unexpected INPUT layout")
	}
	ScanOf = func(vk uint16) uint16 {
		r, _, _ := procMapVirtK.Call(uintptr(vk), 0)
		return uint16(r)
	}
	Send = func(ev []Event) {
		if len(ev) == 0 {
			return
		}
		arr := make([]winInput, len(ev))
		for n, e := range ev {
			if e.Mouse {
				arr[n].Type = 0
				*(*mouseInput)(unsafe.Pointer(&arr[n].Union)) = mouseInput{
					dx: e.X, dy: e.Y, mouseData: uint32(e.Wheel), dwFlags: e.Flags}
			} else {
				arr[n].Type = 1
				*(*keybdInput)(unsafe.Pointer(&arr[n].Union)) = keybdInput{
					wScan: e.Scan, dwFlags: e.Flags}
			}
		}
		procSendInp.Call(uintptr(len(arr)), uintptr(unsafe.Pointer(&arr[0])), unsafe.Sizeof(arr[0]))
	}
}
