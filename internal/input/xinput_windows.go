//go:build windows

package input

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/mxygem/jam/internal/clock"
)

type xinputGamepad struct {
	Buttons      uint16
	LeftTrigger  uint8
	RightTrigger uint8
	ThumbLX      int16
	ThumbLY      int16
	ThumbRX      int16
	ThumbRY      int16
}

type xinputState struct {
	Packet  uint32
	Gamepad xinputGamepad
}

type xinputVibration struct{ L, R uint16 }

type xinputCaps struct {
	Type      uint8
	SubType   uint8
	Flags     uint16
	Gamepad   xinputGamepad
	Vibration xinputVibration
}

var (
	xinputDLL      *syscall.LazyDLL
	procGetState   *syscall.LazyProc
	procGetCaps    *syscall.LazyProc
	winmm          = syscall.NewLazyDLL("winmm.dll")
	procTimeBegin  = winmm.NewProc("timeBeginPeriod")
	procTimeEnd    = winmm.NewProc("timeEndPeriod")
	xinputLoadOnce sync.Once
	xinputOK       bool
)

func loadXInput() bool {
	xinputLoadOnce.Do(func() {
		for _, name := range []string{"xinput1_4.dll", "xinput1_3.dll", "xinput9_1_0.dll"} {
			d := syscall.NewLazyDLL(name)
			if d.Load() == nil {
				xinputDLL = d
				procGetState = d.NewProc("XInputGetState")
				procGetCaps = d.NewProc("XInputGetCapabilities")
				xinputOK = procGetState.Find() == nil && procGetCaps.Find() == nil
				return
			}
		}
	})
	return xinputOK
}

func xinputGetState(slot int, s *xinputState) bool {
	r, _, _ := procGetState.Call(uintptr(slot), uintptr(unsafe.Pointer(s)))
	return r == 0
}

func xinputGetCaps(slot int, c *xinputCaps) bool {
	r, _, _ := procGetCaps.Call(uintptr(slot), 0, uintptr(unsafe.Pointer(c)))
	return r == 0
}

// Button bits in the order events are emitted for a single poll: frets and
// face buttons before the d-pad, so a chord change and a strum that land in
// the same report are seen frets-first.
var xinputBitOrder = []int{12, 13, 14, 15, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 10, 11}

type xinputPoller struct {
	emit func(RawEvent)
	stop chan struct{}

	mu        sync.Mutex
	connected [4]bool
	caps      [4]xinputCaps
	axes      [4][6]float32
	buttons   [4]uint16
	rate      float64 // measured polls per second
}

func startXInput(emit func(RawEvent)) xinputSource {
	if !loadXInput() {
		return nil
	}
	p := &xinputPoller{emit: emit, stop: make(chan struct{})}
	go p.run()
	return p
}

func axisValues(g xinputGamepad) [6]float32 {
	norm := func(v int16) float32 {
		f := float32(v) / 32767
		if f < -1 {
			f = -1
		}
		return f
	}
	return [6]float32{norm(g.ThumbLX), norm(g.ThumbLY), norm(g.ThumbRX), norm(g.ThumbRY), float32(g.LeftTrigger) / 255, float32(g.RightTrigger) / 255}
}

func (p *xinputPoller) run() {
	procTimeBegin.Call(1)
	defer procTimeEnd.Call(1)
	var nextScan [4]float64
	polls := 0
	rateStart := clock.Now()
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		now := clock.Now()
		for slot := 0; slot < 4; slot++ {
			p.mu.Lock()
			conn := p.connected[slot]
			p.mu.Unlock()
			// Polling an empty slot is slow, so only re-check those once a second.
			if !conn && now < nextScan[slot] {
				continue
			}
			var st xinputState
			ok := xinputGetState(slot, &st)
			t := clock.Now()
			if !ok {
				if conn {
					p.mu.Lock()
					p.connected[slot] = false
					p.mu.Unlock()
				}
				nextScan[slot] = now + 1
				continue
			}
			if !conn {
				var c xinputCaps
				xinputGetCaps(slot, &c)
				p.mu.Lock()
				p.connected[slot] = true
				p.caps[slot] = c
				p.buttons[slot] = st.Gamepad.Buttons
				p.axes[slot] = axisValues(st.Gamepad)
				p.mu.Unlock()
				continue
			}
			p.mu.Lock()
			prevB := p.buttons[slot]
			prevA := p.axes[slot]
			newA := axisValues(st.Gamepad)
			p.buttons[slot] = st.Gamepad.Buttons
			p.axes[slot] = newA
			p.mu.Unlock()
			if diff := prevB ^ st.Gamepad.Buttons; diff != 0 {
				for _, bit := range xinputBitOrder {
					if diff&(1<<bit) == 0 {
						continue
					}
					down := st.Gamepad.Buttons&(1<<bit) != 0
					v := float32(0)
					if down {
						v = 1
					}
					p.emit(RawEvent{Time: t, Src: SrcXInput, Device: slot, Kind: KindButton, Code: bit, Value: v, Down: down})
				}
			}
			for i := 0; i < 6; i++ {
				d := newA[i] - prevA[i]
				if d > 1.0/512 || d < -1.0/512 {
					p.emit(RawEvent{Time: t, Src: SrcXInput, Device: slot, Kind: KindAxis, Code: i, Value: newA[i]})
				}
			}
		}
		polls++
		if el := now - rateStart; el >= 2 {
			p.mu.Lock()
			p.rate = float64(polls) / el
			p.mu.Unlock()
			polls = 0
			rateStart = now
		}
		time.Sleep(time.Millisecond)
	}
}

func subtypeName(st uint8) string {
	switch st {
	case 0x01:
		return "gamepad"
	case 0x02:
		return "wheel"
	case 0x03:
		return "arcade stick"
	case 0x04:
		return "flight stick"
	case 0x05:
		return "dance pad"
	case 0x06, 0x07:
		return "guitar"
	case 0x08:
		return "drum kit"
	case 0x0B:
		return "bass guitar"
	case 0x13:
		return "arcade pad"
	}
	return fmt.Sprintf("subtype 0x%02X", st)
}

func (p *xinputPoller) Devices() []DeviceInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []DeviceInfo
	for i := 0; i < 4; i++ {
		if !p.connected[i] {
			continue
		}
		out = append(out, DeviceInfo{
			Src: SrcXInput, Index: i,
			Name:   fmt.Sprintf("XInput %s #%d", subtypeName(p.caps[i].SubType), i+1),
			Detail: fmt.Sprintf("subtype 0x%02X, polled at %.0f Hz", p.caps[i].SubType, p.rate),
		})
	}
	return out
}

func (p *xinputPoller) Axis(slot, axis int) (float32, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if slot < 0 || slot > 3 || axis < 0 || axis > 5 || !p.connected[slot] {
		return 0, false
	}
	return p.axes[slot][axis], true
}

func (p *xinputPoller) Button(slot, bit int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if slot < 0 || slot > 3 || !p.connected[slot] {
		return false
	}
	return p.buttons[slot]&(1<<bit) != 0
}

func (p *xinputPoller) Close() { close(p.stop) }
