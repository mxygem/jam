package input

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/clock"
)

type padState struct {
	name    string
	buttons []bool
	axes    []float32
}

// Manager owns every input source. Background sources (XInput poller, MIDI
// callbacks) push into a queue; Poll drains it together with the keyboard
// and Ebitengine gamepads, once per game update.
type Manager struct {
	mu    sync.Mutex
	queue []RawEvent

	xi   xinputSource
	midi midiSource

	keys  []ebiten.Key
	pads  map[ebiten.GamepadID]*padState
	ids   []ebiten.GamepadID
	state map[ControlKey]float32 // last value of every control seen
}

// NewManager starts all input sources.
func NewManager() *Manager {
	m := &Manager{pads: map[ebiten.GamepadID]*padState{}, state: map[ControlKey]float32{}}
	m.xi = startXInput(m.push)
	m.midi = startMIDI(m.push)
	return m
}

func (m *Manager) push(ev RawEvent) {
	m.mu.Lock()
	if len(m.queue) < 1<<16 {
		m.queue = append(m.queue, ev)
	}
	m.mu.Unlock()
}

// NewBackground starts only the background sources (XInput, MIDI). It needs
// no window, which makes it usable from command line tools.
func NewBackground() *Manager {
	m := &Manager{pads: map[ebiten.GamepadID]*padState{}, state: map[ControlKey]float32{}}
	m.xi = startXInput(m.push)
	m.midi = startMIDI(m.push)
	return m
}

// PollQueue drains background events only (no Ebitengine polling).
func (m *Manager) PollQueue(dst []RawEvent) []RawEvent {
	m.mu.Lock()
	dst = append(dst, m.queue...)
	m.queue = m.queue[:0]
	m.mu.Unlock()
	return dst
}

// RescanMIDI reopens MIDI ports (e.g. after plugging a kit in).
func (m *Manager) RescanMIDI() {
	if m.midi != nil {
		m.midi.Close()
	}
	m.midi = startMIDI(m.push)
}

// Close stops background sources.
func (m *Manager) Close() {
	if m.xi != nil {
		m.xi.Close()
	}
	if m.midi != nil {
		m.midi.Close()
	}
}

// XInputActive reports whether the dedicated XInput poller is running.
func (m *Manager) XInputActive() bool { return m.xi != nil }

// Poll returns all events since the last call, oldest first. It must be
// called from the game's Update.
func (m *Manager) Poll(dst []RawEvent) []RawEvent {
	m.mu.Lock()
	start := len(dst)
	dst = append(dst, m.queue...)
	m.queue = m.queue[:0]
	m.mu.Unlock()

	now := clock.Now()
	m.keys = inpututil.AppendJustPressedKeys(m.keys[:0])
	for _, k := range m.keys {
		dst = append(dst, RawEvent{Time: now, Src: SrcKeyboard, Kind: KindKey, Code: int(k), Value: 1, Down: true})
	}
	m.keys = inpututil.AppendJustReleasedKeys(m.keys[:0])
	for _, k := range m.keys {
		dst = append(dst, RawEvent{Time: now, Src: SrcKeyboard, Kind: KindKey, Code: int(k)})
	}
	dst = m.pollPads(dst, now)

	evs := dst[start:]
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Time < evs[j].Time })
	m.track(evs)
	return dst
}

func (m *Manager) track(evs []RawEvent) {
	for _, e := range evs {
		if e.Kind == KindAxis || e.Kind == KindCC {
			m.state[e.Key()] = e.Value
		} else if e.Down {
			m.state[e.Key()] = 1
		} else {
			delete(m.state, e.Key())
		}
	}
}

// Inject records events as if a device had sent them (tests and tools).
func (m *Manager) Inject(evs []RawEvent) { m.track(evs) }

func (m *Manager) skipPad(name string) bool {
	// On Windows Ebitengine also reports XInput devices; the dedicated poller
	// already handles them with better timing.
	// Its name varies ("XInput Gamepad", "Unknown XInput Device"...).
	return m.xi != nil && runtime.GOOS == "windows" && strings.Contains(strings.ToLower(name), "xinput")
}

func (m *Manager) pollPads(dst []RawEvent, now float64) []RawEvent {
	m.ids = ebiten.AppendGamepadIDs(m.ids[:0])
	seen := map[ebiten.GamepadID]bool{}
	for _, id := range m.ids {
		seen[id] = true
		p, ok := m.pads[id]
		if !ok {
			name := ebiten.GamepadName(id)
			p = &padState{name: name}
			m.pads[id] = p
		}
		if m.skipPad(p.name) {
			continue
		}
		nb := ebiten.GamepadButtonCount(id)
		na := ebiten.GamepadAxisCount(id)
		if len(p.buttons) != nb {
			p.buttons = make([]bool, nb)
			for b := 0; b < nb; b++ {
				p.buttons[b] = ebiten.IsGamepadButtonPressed(id, ebiten.GamepadButton(b))
			}
		}
		if len(p.axes) != na {
			p.axes = make([]float32, na)
			for a := 0; a < na; a++ {
				p.axes[a] = float32(ebiten.GamepadAxisValue(id, ebiten.GamepadAxisType(a)))
			}
		}
		for b := 0; b < nb; b++ {
			down := ebiten.IsGamepadButtonPressed(id, ebiten.GamepadButton(b))
			if down != p.buttons[b] {
				p.buttons[b] = down
				v := float32(0)
				if down {
					v = 1
				}
				dst = append(dst, RawEvent{Time: now, Src: SrcGamepad, Device: int(id), Kind: KindButton, Code: b, Value: v, Down: down})
			}
		}
		for a := 0; a < na; a++ {
			v := float32(ebiten.GamepadAxisValue(id, ebiten.GamepadAxisType(a)))
			if d := v - p.axes[a]; d > 1.0/512 || d < -1.0/512 {
				p.axes[a] = v
				dst = append(dst, RawEvent{Time: now, Src: SrcGamepad, Device: int(id), Kind: KindAxis, Code: a, Value: v})
			}
		}
	}
	for id := range m.pads {
		if !seen[id] {
			delete(m.pads, id)
		}
	}
	return dst
}

// Value returns the last known value of a control and whether it is known.
func (m *Manager) Value(k ControlKey) (float32, bool) {
	switch k.Src {
	case SrcXInput:
		if m.xi != nil {
			if k.Kind == KindAxis {
				return m.xi.Axis(k.Device, k.Code)
			}
			if m.xi.Button(k.Device, k.Code) {
				return 1, true
			}
			return 0, true
		}
	case SrcGamepad:
		if p, ok := m.pads[ebiten.GamepadID(k.Device)]; ok {
			if k.Kind == KindAxis && k.Code < len(p.axes) {
				return p.axes[k.Code], true
			}
			if k.Kind == KindButton && k.Code < len(p.buttons) {
				if p.buttons[k.Code] {
					return 1, true
				}
				return 0, true
			}
		}
	}
	v, ok := m.state[k]
	return v, ok
}

// AxisSnapshot returns the current value of every known axis.
func (m *Manager) AxisSnapshot() map[ControlKey]float32 {
	out := map[ControlKey]float32{}
	if m.xi != nil {
		for _, d := range m.xi.Devices() {
			for a := 0; a < 6; a++ {
				if v, ok := m.xi.Axis(d.Index, a); ok {
					out[ControlKey{SrcXInput, d.Index, KindAxis, a}] = v
				}
			}
		}
	}
	for id, p := range m.pads {
		if m.skipPad(p.name) {
			continue
		}
		for a, v := range p.axes {
			out[ControlKey{SrcGamepad, int(id), KindAxis, a}] = v
		}
	}
	for k, v := range m.state {
		if _, ok := out[k]; !ok && (k.Kind == KindCC || k.Kind == KindAxis) {
			out[k] = v
		}
	}
	return out
}

// Devices lists connected devices. The keyboard is always first.
func (m *Manager) Devices() []DeviceInfo {
	out := []DeviceInfo{{Src: SrcKeyboard, Name: "Keyboard"}}
	if m.xi != nil {
		out = append(out, m.xi.Devices()...)
	}
	ids := make([]int, 0, len(m.pads))
	for id := range m.pads {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	for _, id := range ids {
		p := m.pads[ebiten.GamepadID(id)]
		if m.skipPad(p.name) {
			continue
		}
		out = append(out, DeviceInfo{Src: SrcGamepad, Index: id, Name: p.name, Detail: fmt.Sprintf("%d buttons, %d axes", len(p.buttons), len(p.axes))})
	}
	if m.midi != nil {
		out = append(out, m.midi.Devices()...)
	}
	return out
}

// DeviceName returns a display name for a device.
func (m *Manager) DeviceName(src Source, index int) string {
	for _, d := range m.Devices() {
		if d.Src == src && (src == SrcKeyboard || d.Index == index) {
			return d.Name
		}
	}
	return fmt.Sprintf("%s #%d", src, index)
}

// KeyName names a keyboard key code.
func KeyName(code int) string { return ebiten.Key(code).String() }

// Describe renders an event for debugging and the probe tool.
func (m *Manager) Describe(e RawEvent) string {
	name := ControlName(e.Src, e.Kind, e.Code, KeyName)
	dev := "Keyboard"
	if e.Src != SrcKeyboard {
		dev = m.DeviceName(e.Src, e.Device)
	}
	switch e.Kind {
	case KindAxis, KindCC:
		return fmt.Sprintf("%s: %s = %+.3f", dev, name, e.Value)
	case KindNote:
		if e.Down {
			return fmt.Sprintf("%s: %s on, velocity %d", dev, name, int(e.Value*127+0.5))
		}
		return fmt.Sprintf("%s: %s off", dev, name)
	}
	state := "up"
	if e.Down {
		state = "down"
	}
	return fmt.Sprintf("%s: %s %s", dev, name, state)
}
