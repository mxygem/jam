//go:build windows

package input

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/mxygem/jam/internal/clock"
)

var (
	procMidiInGetNumDevs  = winmm.NewProc("midiInGetNumDevs")
	procMidiInGetDevCapsW = winmm.NewProc("midiInGetDevCapsW")
	procMidiInOpen        = winmm.NewProc("midiInOpen")
	procMidiInStart       = winmm.NewProc("midiInStart")
	procMidiInStop        = winmm.NewProc("midiInStop")
	procMidiInReset       = winmm.NewProc("midiInReset")
	procMidiInClose       = winmm.NewProc("midiInClose")
)

const (
	callbackFunction = 0x30000
	mimData          = 0x3C3
)

type midiInCapsW struct {
	Mid           uint16
	Pid           uint16
	DriverVersion uint32
	Pname         [32]uint16
	Support       uint32
}

type winMIDI struct {
	mu      sync.Mutex
	handles []uintptr
	devices []DeviceInfo
	emit    func(RawEvent)
}

var (
	midiCallbackOnce sync.Once
	midiCallbackPtr  uintptr
	midiActive       *winMIDI
	midiActiveMu     sync.Mutex
)

// midiProc runs on a winmm thread. dwInstance carries the device index.
func midiProc(h, msg, instance, param1, param2 uintptr) uintptr {
	if msg != mimData {
		return 0
	}
	t := clock.Now()
	midiActiveMu.Lock()
	m := midiActive
	midiActiveMu.Unlock()
	if m == nil {
		return 0
	}
	status := byte(param1)
	d1 := int(param1>>8) & 0x7F
	d2 := int(param1>>16) & 0x7F
	dev := int(instance)
	switch status & 0xF0 {
	case 0x90:
		if d2 > 0 {
			m.emit(RawEvent{Time: t, Src: SrcMIDI, Device: dev, Kind: KindNote, Code: d1, Value: float32(d2) / 127, Down: true})
		} else {
			m.emit(RawEvent{Time: t, Src: SrcMIDI, Device: dev, Kind: KindNote, Code: d1})
		}
	case 0x80:
		m.emit(RawEvent{Time: t, Src: SrcMIDI, Device: dev, Kind: KindNote, Code: d1})
	case 0xB0:
		m.emit(RawEvent{Time: t, Src: SrcMIDI, Device: dev, Kind: KindCC, Code: d1, Value: float32(d2) / 127, Down: d2 >= 64})
	}
	return 0
}

func startMIDI(emit func(RawEvent)) midiSource {
	midiCallbackOnce.Do(func() { midiCallbackPtr = syscall.NewCallback(midiProc) })
	m := &winMIDI{emit: emit}
	n, _, _ := procMidiInGetNumDevs.Call()
	for i := 0; i < int(n); i++ {
		var caps midiInCapsW
		procMidiInGetDevCapsW.Call(uintptr(i), uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps))
		name := syscall.UTF16ToString(caps.Pname[:])
		var h uintptr
		r, _, _ := procMidiInOpen.Call(uintptr(unsafe.Pointer(&h)), uintptr(i), midiCallbackPtr, uintptr(i), callbackFunction)
		if r != 0 {
			m.devices = append(m.devices, DeviceInfo{Src: SrcMIDI, Index: i, Name: name, Detail: fmt.Sprintf("could not open (error %d), is another program using it?", r)})
			continue
		}
		procMidiInStart.Call(h)
		m.handles = append(m.handles, h)
		m.devices = append(m.devices, DeviceInfo{Src: SrcMIDI, Index: i, Name: name})
	}
	midiActiveMu.Lock()
	midiActive = m
	midiActiveMu.Unlock()
	return m
}

func (m *winMIDI) Devices() []DeviceInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DeviceInfo(nil), m.devices...)
}

func (m *winMIDI) Close() {
	midiActiveMu.Lock()
	if midiActive == m {
		midiActive = nil
	}
	midiActiveMu.Unlock()
	for _, h := range m.handles {
		procMidiInStop.Call(h)
		procMidiInReset.Call(h)
		procMidiInClose.Call(h)
	}
	m.handles = nil
}
