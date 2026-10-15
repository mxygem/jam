package input

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
)

// Binding ties one control to an action.
type Binding struct {
	Src        string  `json:"src"`                   // keyboard, xinput, gamepad, midi
	Device     int     `json:"device"`                // -1 matches any device of the source
	DeviceName string  `json:"device_name,omitempty"` // MIDI ports are matched by name
	Kind       string  `json:"kind"`                  // button, axis, key, note, cc
	Code       int     `json:"code,omitempty"`
	Key        string  `json:"key,omitempty"`       // keyboard key name
	Rest       float32 `json:"rest,omitempty"`      // axis value at rest
	Full       float32 `json:"full,omitempty"`      // axis value when fully engaged
	Threshold  float32 `json:"threshold,omitempty"` // 0..1 of rest->full that counts as pressed
	MinVel     float32 `json:"min_vel,omitempty"`   // MIDI: ignore softer hits (0..1)
}

// Describe renders the binding for the UI.
func (b Binding) Describe() string {
	src, _ := ParseSource(b.Src)
	kind, _ := ParseKind(b.Kind)
	code := b.Code
	if src == SrcKeyboard {
		var k ebiten.Key
		if err := k.UnmarshalText([]byte(b.Key)); err == nil {
			code = int(k)
		}
	}
	name := ControlName(src, kind, code, KeyName)
	if kind == KindAxis {
		dir := "+"
		if b.Full < b.Rest {
			dir = "-"
		}
		name += " " + dir
	}
	switch src {
	case SrcKeyboard:
		return "Key " + name
	case SrcMIDI:
		if b.DeviceName != "" {
			return name + " on " + b.DeviceName
		}
		return "MIDI " + name
	case SrcXInput:
		return "XInput " + name
	}
	return fmt.Sprintf("Gamepad %s", name)
}

// Family groups instruments that share a binding profile.
type Family string

const (
	FamilyGuitar Family = "guitar"
	FamilyDrums  Family = "drums"
)

// Profile is the full set of bindings for an instrument family.
type Profile struct {
	Family   Family               `json:"family"`
	Bindings map[string][]Binding `json:"bindings"` // action name -> bindings
}

// Get returns the bindings for an action.
func (p *Profile) Get(a action.Action) []Binding { return p.Bindings[a.String()] }

// Set replaces the bindings of one source kind for an action, keeping
// bindings from other sources (so binding a guitar fret keeps the keyboard
// key for the same action).
func (p *Profile) Set(a action.Action, b Binding) {
	if p.Bindings == nil {
		p.Bindings = map[string][]Binding{}
	}
	var keep []Binding
	for _, old := range p.Bindings[a.String()] {
		if old.Src != b.Src {
			keep = append(keep, old)
		}
	}
	p.Bindings[a.String()] = append(keep, b)
}

// SetSource replaces every binding of src for an action with bs.
func (p *Profile) SetSource(a action.Action, src Source, bs []Binding) {
	if p.Bindings == nil {
		p.Bindings = map[string][]Binding{}
	}
	var keep []Binding
	for _, old := range p.Bindings[a.String()] {
		if old.Src != src.String() {
			keep = append(keep, old)
		}
	}
	p.Bindings[a.String()] = append(keep, bs...)
}

// Clear removes bindings of a source for an action.
func (p *Profile) Clear(a action.Action, src Source) {
	var keep []Binding
	for _, old := range p.Bindings[a.String()] {
		if old.Src != src.String() {
			keep = append(keep, old)
		}
	}
	p.Bindings[a.String()] = keep
}

// Bindings is the saved bindings file.
type Bindings struct {
	Version  int                 `json:"version"`
	Profiles map[Family]*Profile `json:"profiles"`
}

// Profile returns the profile for a family, creating defaults if missing.
func (b *Bindings) Profile(f Family) *Profile {
	if b.Profiles == nil {
		b.Profiles = map[Family]*Profile{}
	}
	p, ok := b.Profiles[f]
	if !ok {
		p = DefaultProfile(f)
		b.Profiles[f] = p
	}
	return p
}

// LoadBindings reads the bindings file, falling back to defaults.
func LoadBindings(path string) (*Bindings, error) {
	b := &Bindings{Version: 1}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			b.Profile(FamilyGuitar)
			b.Profile(FamilyDrums)
			return b, nil
		}
		return b, err
	}
	if err := json.Unmarshal(data, b); err != nil {
		return &Bindings{Version: 1}, fmt.Errorf("bindings file %s is invalid, using defaults: %w", path, err)
	}
	b.Profile(FamilyGuitar)
	b.Profile(FamilyDrums)
	return b, nil
}

// Save writes the bindings file.
func (b *Bindings) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func key(name string) Binding { return Binding{Src: "keyboard", Device: -1, Kind: "key", Key: name} }
func xbtn(bit int) Binding  { return Binding{Src: "xinput", Device: -1, Kind: "button", Code: bit} }
func note(n int) Binding    { return Binding{Src: "midi", Device: -1, Kind: "note", Code: n, MinVel: 0.03} }

// DefaultProfile returns the out-of-the-box bindings. Guitar defaults match
// Guitar Hero style Xbox 360 guitars (which is how CRKD guitars present on
// PC); drum defaults follow the General MIDI percussion map.
func DefaultProfile(f Family) *Profile {
	p := &Profile{Family: f, Bindings: map[string][]Binding{}}
	add := func(a action.Action, bs ...Binding) { p.Bindings[a.String()] = append(p.Bindings[a.String()], bs...) }
	switch f {
	case FamilyGuitar:
		add(action.Green, xbtn(12), key("A"))
		add(action.Red, xbtn(13), key("S"))
		add(action.Yellow, xbtn(15), key("D"))
		add(action.Blue, xbtn(14), key("F"))
		add(action.Orange, xbtn(8), key("G"))
		add(action.StrumUp, xbtn(0), key("ArrowUp"))
		add(action.StrumDown, xbtn(1), key("ArrowDown"))
		add(action.StarPower, xbtn(5), key("Space"))
		add(action.Whammy, Binding{Src: "xinput", Device: -1, Kind: "axis", Code: 2, Rest: -1, Full: 1})
		add(action.Tilt, Binding{Src: "xinput", Device: -1, Kind: "axis", Code: 3, Rest: 0, Full: 1, Threshold: 0.6})
		add(action.Start, xbtn(4), key("Enter"))
		add(action.Select, xbtn(5), key("Backspace"))
	case FamilyDrums:
		add(action.Kick, note(35), note(36), key("Space"))
		add(action.DrumRed, note(38), note(40), note(37), key("A"))
		add(action.DrumYellow, note(48), note(50), key("S"))
		add(action.DrumBlue, note(45), note(47), key("D"))
		add(action.DrumGreen, note(41), note(43), key("F"))
		add(action.CymYellow, note(42), note(46), note(22), note(26), key("W"))
		add(action.CymBlue, note(51), note(53), note(59), key("E"))
		add(action.CymGreen, note(49), note(57), note(52), note(55), key("R"))
		add(action.StarPower, key("Q"))
		add(action.Start, key("Enter"))
		add(action.Select, key("Backspace"))
	}
	return p
}

// compiled is a binding resolved for fast matching.
type compiled struct {
	act    action.Action
	src    Source
	device int
	kind   Kind
	code   int
	rest   float32
	full   float32
	thr    float32
	minVel float32
	desc   string
}

func compile(a action.Action, b Binding, devices []DeviceInfo) (compiled, bool) {
	src, ok := ParseSource(b.Src)
	if !ok {
		return compiled{}, false
	}
	kind, ok := ParseKind(b.Kind)
	if !ok {
		return compiled{}, false
	}
	c := compiled{act: a, src: src, device: b.Device, kind: kind, code: b.Code, rest: b.Rest, full: b.Full, thr: b.Threshold, minVel: b.MinVel}
	if src == SrcKeyboard {
		var k ebiten.Key
		if err := k.UnmarshalText([]byte(b.Key)); err != nil {
			return compiled{}, false
		}
		c.code = int(k)
		c.device = -1
	}
	if src == SrcMIDI && b.DeviceName != "" {
		c.device = -1
		for _, d := range devices {
			if d.Src == SrcMIDI && strings.EqualFold(d.Name, b.DeviceName) {
				c.device = d.Index
			}
		}
	}
	if kind == KindAxis {
		if c.full == c.rest {
			c.full = c.rest + 1
		}
		if c.thr <= 0 {
			c.thr = 0.5
		}
	}
	c.desc = b.Src + ":" + b.Describe()
	return c, true
}

// Mapped is an action event in clock time plus the raw source description.
type Mapped struct {
	action.Event
	Src string
}

// Mapper turns raw events into action events using a profile.
type Mapper struct {
	binds  []compiled
	state  []bool
	count  [action.Count]int
	analog [action.Count]float32
	index  map[ControlKey][]int
	any    map[ControlKey][]int // device -1 bindings keyed with Device = -1
}

// NewMapper compiles a profile.
func NewMapper(p *Profile, devices []DeviceInfo) *Mapper {
	m := &Mapper{index: map[ControlKey][]int{}, any: map[ControlKey][]int{}}
	names := make([]string, 0, len(p.Bindings))
	for n := range p.Bindings {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		a, ok := action.Parse(n)
		if !ok {
			continue
		}
		for _, b := range p.Bindings[n] {
			c, ok := compile(a, b, devices)
			if !ok {
				continue
			}
			i := len(m.binds)
			m.binds = append(m.binds, c)
			k := ControlKey{c.src, c.device, c.kind, c.code}
			if c.device == -1 {
				m.any[k] = append(m.any[k], i)
			} else {
				m.index[k] = append(m.index[k], i)
			}
		}
	}
	m.state = make([]bool, len(m.binds))
	return m
}

// Map converts one raw event into zero or more action events.
func (m *Mapper) Map(ev RawEvent, out []Mapped) []Mapped {
	k := ev.Key()
	idx := m.index[k]
	k.Device = -1
	idx = append(append([]int(nil), idx...), m.any[k]...)
	for _, i := range idx {
		c := &m.binds[i]
		switch c.kind {
		case KindAxis:
			n := (ev.Value - c.rest) / (c.full - c.rest)
			if n < 0 {
				n = 0
			}
			if n > 1 {
				n = 1
			}
			if c.act.IsAnalog() {
				if d := n - m.analog[c.act]; d > 0.01 || d < -0.01 || n == 0 || n == 1 {
					if n != m.analog[c.act] {
						m.analog[c.act] = n
						out = append(out, Mapped{action.Event{Time: ev.Time, Action: c.act, Down: n > 0.5, Value: n}, c.desc})
					}
				}
				continue
			}
			on := m.state[i]
			if !on && n >= c.thr {
				out = m.set(out, i, true, ev.Time, n)
			} else if on && n < c.thr*0.6 {
				out = m.set(out, i, false, ev.Time, n)
			}
		case KindNote:
			if c.act.IsDrum() || c.act == action.StarPower && c.src == SrcMIDI {
				// Drum hits are instantaneous: every note-on is a hit and
				// note-offs (which some kits never send) don't matter.
				if ev.Down && ev.Value >= c.minVel {
					out = append(out, Mapped{action.Event{Time: ev.Time, Action: c.act, Down: true, Value: ev.Value}, c.desc})
				}
				continue
			}
			if ev.Down && ev.Value < c.minVel {
				continue
			}
			out = m.set(out, i, ev.Down, ev.Time, ev.Value)
		case KindCC:
			out = m.set(out, i, ev.Down, ev.Time, ev.Value)
		default:
			out = m.set(out, i, ev.Down, ev.Time, ev.Value)
		}
	}
	return out
}

func (m *Mapper) set(out []Mapped, i int, on bool, t float64, v float32) []Mapped {
	if m.state[i] == on {
		return out
	}
	m.state[i] = on
	c := &m.binds[i]
	if on {
		m.count[c.act]++
		if m.count[c.act] == 1 {
			out = append(out, Mapped{action.Event{Time: t, Action: c.act, Down: true, Value: v}, c.desc})
		}
	} else {
		m.count[c.act]--
		if m.count[c.act] == 0 {
			out = append(out, Mapped{action.Event{Time: t, Action: c.act, Down: false}, c.desc})
		}
	}
	return out
}

// IsDown reports whether any binding of the action is held.
func (m *Mapper) IsDown(a action.Action) bool { return m.count[a] > 0 }

// Analog returns the last analog value of an action.
func (m *Mapper) Analog(a action.Action) float32 { return m.analog[a] }

// Reset forgets held state (e.g. when a scene starts).
func (m *Mapper) Reset() {
	for i := range m.state {
		m.state[i] = false
	}
	m.count = [action.Count]int{}
	m.analog = [action.Count]float32{}
}
