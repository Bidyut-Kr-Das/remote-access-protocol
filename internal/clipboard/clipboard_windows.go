//go:build windows

package clipboard

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	kernel32        = windows.NewLazySystemDLL("kernel32.dll")
	openClipboard   = user32.NewProc("OpenClipboard")
	closeClipboard  = user32.NewProc("CloseClipboard")
	emptyClipboard  = user32.NewProc("EmptyClipboard")
	getClipboard    = user32.NewProc("GetClipboardData")
	setClipboard    = user32.NewProc("SetClipboardData")
	clipboardSeqNum = user32.NewProc("GetClipboardSequenceNumber")
	globalAlloc     = kernel32.NewProc("GlobalAlloc")
	globalFree      = kernel32.NewProc("GlobalFree")
	globalLock      = kernel32.NewProc("GlobalLock")
	globalUnlock    = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 2
)

var errBusy = errors.New("clipboard: busy")

// open retries briefly: another app may hold the clipboard for a few ms.
func open() error {
	for i := 0; i < 5; i++ {
		if r, _, _ := openClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errBusy
}

func seq() uint32 {
	r, _, _ := clipboardSeqNum.Call()
	return uint32(r)
}

func get() (string, error) {
	if err := open(); err != nil {
		return "", err
	}
	defer closeClipboard.Call()
	h, _, _ := getClipboard.Call(cfUnicodeText)
	if h == 0 {
		return "", nil // no text on the clipboard
	}
	p, _, e := globalLock.Call(h)
	if p == 0 {
		return "", e
	}
	defer globalUnlock.Call(h)
	return windows.UTF16PtrToString((*uint16)(ptr(p))), nil
}

func set(text string) error {
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	h, _, e := globalAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return e
	}
	p, _, e := globalLock.Call(h)
	if p == 0 {
		globalFree.Call(h)
		return e
	}
	copy(unsafe.Slice((*uint16)(ptr(p)), len(u)), u)
	globalUnlock.Call(h)
	if err := open(); err != nil {
		globalFree.Call(h)
		return err
	}
	defer closeClipboard.Call()
	emptyClipboard.Call()
	if r, _, e := setClipboard.Call(cfUnicodeText, h); r == 0 {
		globalFree.Call(h) // ownership only transfers on success
		return e
	}
	return nil
}

// ptr converts a GlobalLock result; the memory is not Go-managed, so vet's uintptr rule does not apply.
func ptr(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }
