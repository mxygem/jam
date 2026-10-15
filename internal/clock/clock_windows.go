//go:build windows

package clock

import (
	"syscall"
	"unsafe"
)

// QueryPerformanceCounter gives sub-microsecond resolution on Windows, where
// the runtime's monotonic time can be much coarser.
var (
	kernel32  = syscall.NewLazyDLL("kernel32.dll")
	procQPC   = kernel32.NewProc("QueryPerformanceCounter")
	procQPF   = kernel32.NewProc("QueryPerformanceFrequency")
	freq      float64
	startTick int64
)

func init() {
	var f int64
	procQPF.Call(uintptr(unsafe.Pointer(&f)))
	if f <= 0 {
		f = 10_000_000
	}
	freq = float64(f)
	procQPC.Call(uintptr(unsafe.Pointer(&startTick)))
}

func now() float64 {
	var t int64
	procQPC.Call(uintptr(unsafe.Pointer(&t)))
	return float64(t-startTick) / freq
}
