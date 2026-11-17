//go:build windows

package clipboard

import (
	"errors"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procFormatAvailable  = user32.NewProc("IsClipboardFormatAvailable")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalSize       = kernel32.NewProc("GlobalSize")
	procDragQueryFileW   = shell32.NewProc("DragQueryFileW")
)

const (
	cfUnicodeText = 13
	cfHDrop       = 15 // files or folders copied in Explorer
)

func read() (string, error) {
	// The clipboard belongs to the thread that opened it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	opened := false
	for range 10 { // another program may have it open for a moment
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			opened = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !opened {
		return "", errors.New("the clipboard is in use by another program")
	}
	defer procCloseClipboard.Call()

	if ok, _, _ := procFormatAvailable.Call(cfUnicodeText); ok != 0 {
		h, _, _ := procGetClipboardData.Call(cfUnicodeText)
		if h == 0 {
			return "", errors.New("couldn't read the clipboard")
		}
		p, _, _ := procGlobalLock.Call(h)
		if p == 0 {
			return "", errors.New("couldn't read the clipboard")
		}
		defer procGlobalUnlock.Call(h)
		size, _, _ := procGlobalSize.Call(h)
		// p is memory Windows owns, not a Go pointer.
		u := unsafe.Slice((*uint16)(*(*unsafe.Pointer)(unsafe.Pointer(&p))), int(size/2))
		for i, c := range u {
			if c == 0 {
				u = u[:i]
				break
			}
		}
		return string(utf16.Decode(u)), nil
	}
	if ok, _, _ := procFormatAvailable.Call(cfHDrop); ok != 0 {
		h, _, _ := procGetClipboardData.Call(cfHDrop)
		if h == 0 {
			return "", errors.New("couldn't read the clipboard")
		}
		buf := make([]uint16, 32768)
		n, _, _ := procDragQueryFileW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		return string(utf16.Decode(buf[:n])), nil
	}
	return "", nil
}
