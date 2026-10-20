package app

import (
	"fmt"
	"image/color"
	"log"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/profile"
	"github.com/mxygem/jam/internal/render"
)

type wizStage int

const (
	stageWho wizStage = iota // whose controls (when there's more than one profile)
	stageDevice
	stageBind
	stageTest
)

type captured struct {
	act   action.Action
	binds []input.Binding
	desc  []string
}

type axisTrack struct {
	key     input.ControlKey
	base    float32
	extreme float32
	since   float64
	tripped bool
}

// Wizard maps a controller step by step and lets the player test it. The
// mapping is saved to one profile: the one picked on the first screen
// (which starts on whoever's controller opened the setup), or the player
// whose card opened it in the lobby.
type Wizard struct {
	fam   input.Family
	stage wizStage
	set   action.Set

	prof     *profile.Profile // whose bindings these are
	who      List             // stageWho
	back     func(a *App)     // where leaving goes
	fixedDev bool             // the controller was known up front (lobby)
	guessed  string           // why prof was picked (shown on the first screen)

	devSrc  input.Source
	devIdx  int
	devName string

	step      int
	stepStart float64
	caps      []captured
	waitUp    *input.ControlKey // waiting for this control to be released
	doneAt    float64           // when the current step got its capture
	axes      map[input.ControlKey]*axisTrack
	baseline  map[input.ControlKey]float32
	warn      string
	warnAt    float64
	lastHit   float64

	// test screen
	mapper             *input.Mapper
	log                []string
	logT               []float64
	strumUp, strumDown float64
	strums             int
	padHit             map[action.Action]float64
	padVel             map[action.Action]float32
	lastRaw            float64
}

// NewWizard starts the setup flow for an instrument family from the
// settings. It's for the profile linked to the controller that opened it
// (or the active profile), and asks first when there's more than one.
func NewWizard(a *App, fam input.Family) *Wizard {
	w := newWizard(a, fam, a.Active, func(a *App) { a.SetScene(NewSettingsAt(a, pageControls)) })
	if a.confirmSet {
		if p := a.DeviceOwner(a.confirmDev); p != nil {
			w.prof = p
			w.guessed = "The controller that opened this belongs to " + p.Name
		}
	}
	if a.Profiles != nil && len(a.Profiles.All()) > 1 {
		w.stage = stageWho
		w.buildWho(a)
	}
	return w
}

// NewWizardFor sets up controls for one profile, returning with back. With
// dev the controller is already known and the setup starts on its first
// control.
func NewWizardFor(a *App, fam input.Family, p *profile.Profile, dev *device, back func(a *App)) *Wizard {
	w := newWizard(a, fam, p, back)
	if dev != nil {
		w.fixedDev = true
		w.useDevice(a, dev.src, dev.index)
		w.beginStep(a, 0)
		w.stage = stageBind
	}
	return w
}

func newWizard(a *App, fam input.Family, p *profile.Profile, back func(a *App)) *Wizard {
	w := &Wizard{fam: fam, set: action.GuitarSet, padHit: map[action.Action]float64{}, padVel: map[action.Action]float32{}, prof: p, back: back, stage: stageDevice}
	if fam == input.FamilyDrums {
		w.set = action.DrumSet
	}
	w.stepStart = clock.Now()
	return w
}

func (w *Wizard) buildWho(a *App) {
	w.who = List{Visible: 2, Items: []Item{
		{Label: "Controls for",
			Value:  func() string { return w.prof.Name },
			Swatch: func() color.RGBA { return profileColor(w.prof) },
			Hint:   "Each profile keeps its own controller setup. Left/right picks whose this is",
			Adjust: func(d int) {
				all := a.Profiles.All()
				i := max(0, a.Profiles.Index(w.prof))
				w.prof = all[((i+sign(d))%len(all)+len(all))%len(all)]
			}},
		{Label: "Start", Hint: "Next, press a button on the controller to set up", Activate: func() {
			w.stage = stageDevice
			w.stepStart = clock.Now()
		}},
	}}
	w.who.Sel = 1
}

// useDevice picks the controller being set up.
func (w *Wizard) useDevice(a *App, src input.Source, idx int) {
	w.devSrc, w.devIdx = src, idx
	w.devName = a.Input.DeviceName(src, idx)
	if src == input.SrcKeyboard {
		w.devName = "Keyboard"
	}
	log.Printf("wizard %s: device %s (%s #%d) for %s", w.fam, w.devName, src, idx, w.prof.Name)
}

// bindings are the profile's bindings being changed.
func (w *Wizard) bindings(a *App) *input.Bindings { return a.BindingsFor(w.prof) }

func (w *Wizard) leave(a *App) { w.back(a) }

func (w *Wizard) isDrums() bool { return w.fam == input.FamilyDrums }

func (w *Wizard) fromDevice(e input.RawEvent) bool {
	if e.Src != w.devSrc {
		return false
	}
	if e.Src == input.SrcKeyboard {
		return true
	}
	return e.Device == w.devIdx
}

func (w *Wizard) Update(a *App) error {
	now := clock.Now()
	esc, back := false, false
	for _, e := range a.Raw {
		if e.Src == input.SrcKeyboard && e.Down {
			switch ebiten.Key(e.Code) {
			case ebiten.KeyEscape:
				esc = true
			case ebiten.KeyBackspace:
				back = true
			}
		}
	}
	switch w.stage {
	case stageWho:
		if w.who.Update(a) {
			return nil
		}
		if a.Pressed(action.MenuBack) {
			w.leave(a)
		}
		return nil
	case stageDevice:
		if esc {
			w.leave(a)
			return nil
		}
		for _, e := range a.Raw {
			if !e.Down || e.Kind == input.KindAxis {
				continue
			}
			if e.Src == input.SrcKeyboard && (ebiten.Key(e.Code) == ebiten.KeyEscape || ebiten.Key(e.Code) == ebiten.KeyF5) {
				continue
			}
			if w.isDrums() && e.Src != input.SrcMIDI && e.Src != input.SrcKeyboard {
				continue
			}
			w.useDevice(a, e.Src, e.Device)
			k := e.Key()
			w.waitUp = &k
			w.beginStep(a, 0)
			w.stage = stageBind
			return nil
		}
		if ebiten.IsKeyPressed(ebiten.KeyF5) {
			a.Input.RescanMIDI()
		}
	case stageBind:
		w.updateBind(a, now, esc, back)
	case stageTest:
		w.updateTest(a, now, esc, back)
	}
	if a.Opts.ShotScene == "wizard" && a.frame > 30 {
		a.TakeScreenshotNext()
	}
	return nil
}

func (w *Wizard) beginStep(a *App, i int) {
	w.step = i
	w.stepStart = clock.Now()
	w.doneAt = 0
	w.axes = map[input.ControlKey]*axisTrack{}
	w.baseline = a.Input.AxisSnapshot()
	if i < len(w.caps) {
		w.caps = w.caps[:i]
	}
}

func (w *Wizard) current() action.Action { return w.set[w.step] }

func (w *Wizard) usedBy(k input.ControlKey) (action.Action, bool) {
	for _, c := range w.caps {
		for _, b := range c.binds {
			if b.Kind == k.Kind.String() && b.Src == k.Src.String() && b.Code == k.Code && k.Kind != input.KindAxis {
				return c.act, true
			}
		}
	}
	return 0, false
}

func (w *Wizard) bindingFor(a *App, e input.RawEvent) input.Binding {
	b := input.Binding{Src: e.Src.String(), Device: -1, Kind: e.Kind.String(), Code: e.Code}
	switch e.Src {
	case input.SrcKeyboard:
		b.Key = input.KeyName(e.Code)
		b.Code = 0
	case input.SrcMIDI:
		b.DeviceName = w.devName
		b.MinVel = 0.03
	}
	return b
}

func (w *Wizard) describe(a *App, b input.Binding) string { return b.Describe() }

func (w *Wizard) finishStep(a *App, c captured) {
	log.Printf("wizard %s: %s = %v (%.2fs)", w.fam, c.act.Title(), c.desc, clock.Now()-w.stepStart)
	w.caps = append(w.caps, c)
	w.doneAt = clock.Now()
}

func (w *Wizard) updateBind(a *App, now float64, esc, back bool) {
	if esc {
		if w.step == 0 {
			if w.fixedDev {
				w.leave(a)
				return
			}
			w.stage = stageDevice
			return
		}
		w.beginStep(a, w.step-1)
		return
	}
	// Release gating: wait until the previous control is let go.
	if w.waitUp != nil {
		up := *w.waitUp
		released := false
		if v, ok := a.Input.Value(up); !ok || v < 0.5 {
			released = true
		}
		for _, e := range a.Raw {
			if e.Key() == up && !e.Down && e.Kind != input.KindAxis {
				released = true
			}
		}
		if !released && !w.isDrums() {
			return
		}
		w.waitUp = nil
	}
	act := w.current()
	// A finished step advances after a short confirmation pause; drum pads
	// keep collecting extra notes until the kit goes quiet.
	if w.doneAt > 0 {
		hold := 0.45
		if w.isDrums() {
			hold = 1.6
			for _, e := range a.Raw {
				if e.Down && w.fromDevice(e) && (e.Kind == input.KindNote || e.Kind == input.KindKey) {
					k := e.Key()
					if other, used := w.usedBy(k); used && other != act {
						w.warnf(fmt.Sprintf("That's already %s", other.Title()))
						continue
					}
					c := &w.caps[len(w.caps)-1]
					dup := false
					for _, b := range c.binds {
						if b.Code == e.Code && b.Kind == e.Kind.String() && (e.Src != input.SrcKeyboard || b.Key == input.KeyName(e.Code)) {
							dup = true
						}
					}
					if !dup {
						b := w.bindingFor(a, e)
						c.binds = append(c.binds, b)
						c.desc = append(c.desc, b.Describe())
					}
					w.doneAt = now
				}
			}
		}
		if back {
			w.beginStep(a, w.step)
			return
		}
		if now-w.doneAt >= hold {
			w.next(a)
		}
		return
	}
	if back {
		log.Printf("wizard %s: %s skipped", w.fam, act.Title())
		w.caps = append(w.caps, captured{act: act}) // skip
		w.next(a)
		return
	}
	if action.Optional(act) && now-w.stepStart > 12 {
		log.Printf("wizard %s: %s timed out (optional)", w.fam, act.Title())
		w.caps = append(w.caps, captured{act: act})
		w.next(a)
		return
	}
	for _, e := range a.Raw {
		if !w.fromDevice(e) {
			continue
		}
		switch e.Kind {
		case input.KindButton, input.KindKey, input.KindNote:
			if !e.Down {
				continue
			}
			if w.isDrums() && act == action.Kick && e.Src == input.SrcMIDI && e.Code == 44 {
				w.warnf("That's the hi-hat pedal; press the kick pedal")
				continue
			}
			k := e.Key()
			if other, used := w.usedBy(k); used && !(sharedOK(act, other)) {
				w.warnf(fmt.Sprintf("That's already %s", other.Title()))
				continue
			}
			b := w.bindingFor(a, e)
			w.finishStep(a, captured{act: act, binds: []input.Binding{b}, desc: []string{b.Describe()}})
			if !w.isDrums() {
				w.waitUp = &k
			}
			return
		case input.KindAxis, input.KindCC:
			k := e.Key()
			t, ok := w.axes[k]
			if !ok {
				base, has := w.baseline[k]
				if !has {
					base = e.Value
				}
				t = &axisTrack{key: k, base: base, extreme: base, since: now}
				w.axes[k] = t
			}
			dev := e.Value - t.base
			if abs32(dev) > abs32(t.extreme-t.base) {
				t.extreme = e.Value
			}
			span := float32(2)
			if k.Kind == input.KindCC || (k.Src == input.SrcXInput && k.Code >= 4) {
				span = 1
			}
			if !t.tripped && abs32(dev) >= 0.45*span {
				t.tripped = true
				t.since = now
			}
			// Finish once the control has come back toward rest, or after
			// being held for a moment (tilt sensors may not return fully).
			if t.tripped && (abs32(dev) < 0.2*span || now-t.since > 1.5) {
				b := input.Binding{Src: k.Src.String(), Device: -1, Kind: k.Kind.String(), Code: k.Code, Rest: t.base, Full: t.extreme}
				if act != action.Whammy {
					b.Threshold = 0.55
				}
				if k.Src == input.SrcMIDI {
					b.DeviceName = w.devName
				}
				w.finishStep(a, captured{act: act, binds: []input.Binding{b}, desc: []string{b.Describe()}})
				return
			}
		}
	}
}

func sharedOK(a, b action.Action) bool {
	pair := func(x, y action.Action) bool { return (a == x && b == y) || (a == y && b == x) }
	return pair(action.StarPower, action.Select) || pair(action.Start, action.StarPower)
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

func (w *Wizard) warnf(s string) {
	w.warn = s
	w.warnAt = clock.Now()
}

func (w *Wizard) next(a *App) {
	if w.step+1 < len(w.set) {
		w.beginStep(a, w.step+1)
		w.caps = w.caps[:w.step] // beginStep trims; keep captures before this step
		return
	}
	w.enterTest(a)
}

// pending builds the profile that would be saved.
func (w *Wizard) pending(a *App) *input.Profile {
	base := w.bindings(a).Profile(w.fam)
	p := &input.Profile{Family: w.fam, Bindings: map[string][]input.Binding{}}
	for k, v := range base.Bindings {
		p.Bindings[k] = append([]input.Binding(nil), v...)
	}
	for _, c := range w.caps {
		// A skipped step means this device doesn't have that control, so
		// its old or default binding for it goes too (a CRKD's knob sits
		// where other guitars report tilt).
		p.SetSource(c.act, w.devSrc, c.binds)
	}
	return p
}

func (w *Wizard) enterTest(a *App) {
	w.stage = stageTest
	w.mapper = input.NewMapper(w.pending(a), a.Input.Devices())
	w.log = nil
}

func (w *Wizard) save(a *App) {
	p := w.pending(a)
	b := w.bindings(a)
	b.Profiles[w.fam] = p
	var err error
	if w.prof == a.Active {
		err = a.SaveBindings() // also refreshes menu navigation
	} else {
		err = b.Save(w.prof.BindingsPath())
	}
	if err != nil {
		a.Toast("Couldn't save bindings: " + err.Error())
		return
	}
	for _, c := range w.caps {
		log.Printf("wizard %s saved for %s: %s = %v", w.fam, w.prof.Name, c.act.Title(), c.desc)
	}
	a.LinkDevice(w.prof, device{src: w.devSrc, index: w.devIdx})
	a.Toast(fmt.Sprintf("Saved %s's %s controls (%s)", w.prof.Name, w.fam, w.devName))
	w.leave(a)
}

func (w *Wizard) updateTest(a *App, now float64, esc, back bool) {
	if esc {
		w.leave(a)
		return
	}
	if back {
		w.stage = stageBind
		w.caps = nil
		w.beginStep(a, 0)
		return
	}
	for _, e := range a.Raw {
		if e.Src == input.SrcKeyboard && ebiten.Key(e.Code) == ebiten.KeyEnter && e.Down {
			w.save(a)
			return
		}
		if e.Kind != input.KindAxis || abs32(e.Value) > 0 {
			if e.Kind != input.KindAxis || len(w.logT) == 0 || now-w.lastRaw > 0.05 {
				w.addLog(a, e, now)
			}
		}
		for _, m := range w.mapper.Map(e, nil) {
			switch m.Action {
			case action.StrumUp:
				if m.Down {
					w.strumUp = now
					w.strums++
				}
			case action.StrumDown:
				if m.Down {
					w.strumDown = now
					w.strums++
				}
			case action.Start:
				if m.Down && !w.isDrums() {
					w.save(a)
					return
				}
			}
			if m.Down && m.Action.IsDrum() {
				w.padHit[m.Action] = now
				w.padVel[m.Action] = m.Value
			}
		}
	}
}

func (w *Wizard) addLog(a *App, e input.RawEvent, now float64) {
	delta := ""
	if w.lastRaw > 0 {
		delta = fmt.Sprintf("+%.1f ms", (e.Time-w.lastRaw)*1000)
	}
	w.lastRaw = e.Time
	w.log = append(w.log, fmt.Sprintf("%-10s %s", delta, a.Input.Describe(e)))
	w.logT = append(w.logT, now)
	if len(w.log) > 12 {
		w.log = w.log[len(w.log)-12:]
		w.logT = w.logT[len(w.logT)-12:]
	}
}

// --- drawing ---

func (w *Wizard) actionColor(act action.Action) color.RGBA {
	switch act {
	case action.Green, action.DrumGreen, action.CymGreen:
		return th.Lanes[0]
	case action.Red, action.DrumRed:
		return th.Lanes[1]
	case action.Yellow, action.DrumYellow, action.CymYellow:
		return th.Lanes[2]
	case action.Blue, action.DrumBlue, action.CymBlue:
		return th.Lanes[3]
	case action.Orange, action.Kick, action.DrumOrange:
		return th.Lanes[4]
	case action.StarPower, action.Tilt, action.Whammy:
		return th.StarPower
	}
	return th.Text
}

func prompt(act action.Action) (string, string) {
	switch act {
	case action.Green, action.Red, action.Yellow, action.Blue, action.Orange:
		return "Press the " + strings.ToUpper(strings.TrimSuffix(act.Title(), " fret")) + " fret", "Press and release it."
	case action.StrumDown:
		return "Strum DOWN", "Push the strum bar down once."
	case action.StrumUp:
		return "Strum UP", "Push the strum bar up once."
	case action.StarPower:
		return "Press your Star Power button", "Usually Select / Back. Tilt is mapped separately next."
	case action.Tilt:
		return "Tilt the guitar up", "Raise the neck toward the ceiling and hold a second, then lower it."
	case action.Whammy:
		return "Push the whammy all the way", "Press it down fully, then let it go."
	case action.Start:
		return "Press START", "Used to pause during songs and to save on the test screen."
	case action.Select:
		return "Press SELECT", "Used to go back in menus."
	case action.Kick:
		return "Press the KICK pedal", "Hit it a couple of times. Moves on when it goes quiet."
	case action.DrumRed:
		return "Hit the RED pad (snare)", "Hit the head and the rim: each zone sends its own note."
	case action.DrumYellow, action.DrumBlue, action.DrumGreen:
		return "Hit the " + strings.ToUpper(strings.Fields(act.Title())[0]) + " tom", "Head and rim if it has zones."
	case action.CymYellow:
		return "Hit the HI-HAT (yellow cymbal)", "Open and closed, bow and edge. Skip if you don't have pro cymbals."
	case action.CymBlue:
		return "Hit the RIDE (blue cymbal)", "Bow, edge and bell. Skip if you don't have it."
	case action.CymGreen:
		return "Hit the CRASH (green cymbal)", "Skip if you don't have it."
	case action.DrumOrange:
		return "Hit the ORANGE pad (5-lane kits only)", "Press Backspace to skip."
	}
	return act.Title(), ""
}

func (w *Wizard) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	now := clock.Now()
	what := "Guitar / bass setup"
	if w.isDrums() {
		what = "Drum setup"
	}
	for_ := ""
	if w.prof != nil && a.Profiles != nil && len(a.Profiles.All()) > 1 {
		for_ = " for " + w.prof.Name
	}
	switch w.stage {
	case stageWho:
		Header(dst, what, "Whose controls are these?")
		Panel(dst, 560, 330, 800, 210)
		render.FillRect(dst, 560, 330, 12, 210, profileColor(w.prof))
		w.who.Draw(dst, 610, 370, 720, 80)
		drawWrapped(dst, w.who.Hint(), render.UI, 28, 560, 570, 800, th.TextDim)
		if w.guessed != "" {
			drawWrapped(dst, w.guessed, render.UI, 26, 560, 640, 800, th.TextDim)
		}
		Footer(dst, "Left/right pick a profile     Enter / green choose     Esc / red back")
	case stageDevice:
		Header(dst, what+for_, "Step 1: pick the device")
		msg := "Press any button on the guitar you want to use"
		if w.isDrums() {
			msg = "Hit any pad on your drum kit"
		}
		render.Text(dst, msg, render.UIBold, 54, render.ScreenW/2, 330, th.Text, render.Center)
		render.Text(dst, "(or press a key to set up the keyboard)", render.UI, 30, render.ScreenW/2, 410, th.TextDim, render.Center)
		devs := a.Input.Devices()
		Panel(dst, 560, 500, 800, 80+float64(len(devs))*48)
		render.Text(dst, "CONNECTED DEVICES", render.CondBold, 24, 600, 520, th.TextDim, render.Left)
		for i, d := range devs {
			render.Text(dst, d.String(), render.UI, 28, 600, 566+float64(i)*48, th.Text, render.Left)
		}
		if w.isDrums() {
			hasMIDI := false
			for _, d := range devs {
				if d.Src == input.SrcMIDI {
					hasMIDI = true
				}
			}
			if !hasMIDI {
				render.Text(dst, "No MIDI devices found. Plug in your kit and hold F5 to rescan.", render.UI, 26, render.ScreenW/2, 620+float64(len(devs))*48, th.Late, render.Center)
			}
		}
		Footer(dst, "Esc back")
	case stageBind:
		act := w.current()
		Header(dst, what+for_, fmt.Sprintf("Step %d of %d  ·  %s", w.step+1, len(w.set), w.devName))
		title, sub := prompt(act)
		c := w.actionColor(act)
		pulse := 0.85 + 0.15*math.Sin(now*6)
		var b render.Batch
		b.Ellipse(dst, render.ScreenW/2, 400, 90*pulse, 90*pulse, render.Lighten(c, 0.3), c)
		b.Ring(dst, render.ScreenW/2, 400, 96, 96, 10, render.RGBA(0xE6, 0xEA, 0xF2, 0xFF), render.RGBA(0x7A, 0x80, 0x90, 0xFF))
		b.Flush(dst)
		render.TextShadow(dst, title, render.Display, 72, render.ScreenW/2, 530, c, render.Center)
		render.Text(dst, sub, render.UI, 30, render.ScreenW/2, 620, th.TextDim, render.Center)
		if w.doneAt > 0 && len(w.caps) > 0 {
			got := w.caps[len(w.caps)-1]
			render.Text(dst, "Got it: "+strings.Join(got.desc, ", "), render.UIBold, 32, render.ScreenW/2, 690, th.Good, render.Center)
		} else if w.waitUp != nil && !w.isDrums() {
			render.Text(dst, "(release the previous button)", render.UI, 26, render.ScreenW/2, 690, th.TextDim, render.Center)
		}
		if action.Optional(act) && w.doneAt == 0 {
			left := 12 - (now - w.stepStart)
			render.Text(dst, fmt.Sprintf("Optional: skipping in %.0fs, or press Backspace", math.Max(0, left)), render.Cond, 26, render.ScreenW/2, 740, th.TextDim, render.Center)
		}
		if w.warn != "" && now-w.warnAt < 2 {
			render.Text(dst, w.warn, render.UIBold, 30, render.ScreenW/2, 790, th.Bad, render.Center)
		}
		// progress dots
		for i := range w.set {
			x := render.ScreenW/2 - float64(len(w.set))*14 + float64(i)*28
			cc := render.WithAlpha(th.TextDim, 0.3)
			if i < w.step {
				cc = th.Good
			} else if i == w.step {
				cc = th.Accent
			}
			render.FillRect(dst, x, 860, 18, 6, cc)
		}
		Footer(dst, "Backspace skip / redo     Esc previous step")
	case stageTest:
		Header(dst, what+for_, "Test it: everything you press should light up below")
		if w.isDrums() {
			w.drawDrumTest(dst, now)
		} else {
			w.drawGuitarTest(dst, now)
		}
		// raw event log
		lx, ly := 1140.0, 230.0
		Panel(dst, lx-24, ly-20, 730, 620)
		render.Text(dst, "RAW EVENTS", render.CondBold, 24, lx, ly, th.TextDim, render.Left)
		for i, s := range w.log {
			age := now - w.logT[i]
			render.Text(dst, truncateW(s, render.Mono, 20, 680), render.Mono, 20, lx, ly+44+float64(i)*44, render.WithAlpha(th.Text, math.Max(0.35, 1-age/6)), render.Left)
		}
		foot := "Enter or START: save     Backspace: redo mapping     Esc: cancel"
		if w.isDrums() {
			foot = "Enter: save     Backspace: redo mapping     Esc: cancel"
		}
		Footer(dst, foot)
	}
}

func (w *Wizard) drawGuitarTest(dst *ebiten.Image, now float64) {
	var b render.Batch
	x0, y0 := 160.0, 330.0
	for l, act := range fretActions {
		c := th.Lanes[l]
		cx := x0 + float64(l)*170
		if w.mapper.IsDown(act) {
			b.Ellipse(dst, cx, y0, 70, 70, render.Lighten(c, 0.45), c)
		} else {
			b.Ellipse(dst, cx, y0, 70, 70, render.Darken(c, 0.6), render.Darken(c, 0.8))
		}
		b.Ring(dst, cx, y0, 74, 74, 8, render.RGBA(0xE6, 0xEA, 0xF2, 0xFF), render.RGBA(0x7A, 0x80, 0x90, 0xFF))
	}
	// strum bar
	sy := 520.0
	up := math.Max(0, 1-(now-w.strumUp)/0.15)
	dn := math.Max(0, 1-(now-w.strumDown)/0.15)
	b.Rect(dst, 160, sy-up*14+dn*14, 680, 40, render.Mix(th.Panel, th.Accent, math.Max(up, dn)), render.Mix(th.Panel, th.Accent, math.Max(up, dn)))
	b.Flush(dst)
	render.Text(dst, fmt.Sprintf("STRUM  (%d)", w.strums), render.CondBold, 26, 500, sy+4, th.Text, render.Center)
	// whammy & tilt meters
	wv := float64(w.mapper.Analog(action.Whammy))
	render.Text(dst, "WHAMMY", render.CondBold, 24, 160, 610, th.TextDim, render.Left)
	render.FillRect(dst, 300, 614, 540, 20, render.WithAlpha(th.TextDim, 0.2))
	render.FillRect(dst, 300, 614, 540*wv, 20, th.StarPower)
	pill := func(x float64, label string, on bool) {
		c := render.WithAlpha(th.TextDim, 0.3)
		tc := th.TextDim
		if on {
			c, tc = th.Accent, th.Text
		}
		render.RoundRect(dst, x, 670, 160, 50, 25, c)
		render.Text(dst, label, render.CondBold, 26, x+80, 680, tc, render.Center)
	}
	pill(160, "TILT", w.mapper.IsDown(action.Tilt))
	pill(340, "STAR PWR", w.mapper.IsDown(action.StarPower))
	pill(520, "START", w.mapper.IsDown(action.Start))
	pill(700, "SELECT", w.mapper.IsDown(action.Select))
	// mapping summary
	y := 760.0
	var parts []string
	for _, c := range w.caps {
		if len(c.desc) > 0 {
			parts = append(parts, fmt.Sprintf("%s: %s", c.act.Title(), strings.Join(c.desc, "/")))
		}
	}
	for i := 0; i < len(parts); i += 3 {
		end := min(len(parts), i+3)
		render.Text(dst, strings.Join(parts[i:end], "     "), render.Cond, 22, 160, y, th.TextDim, render.Left)
		y += 30
	}
}

func (w *Wizard) drawDrumTest(dst *ebiten.Image, now float64) {
	type pad struct {
		act  action.Action
		x, y float64
		cym  bool
	}
	pads := []pad{
		{action.CymYellow, 260, 330, true}, {action.CymBlue, 700, 330, true}, {action.CymGreen, 920, 330, true},
		{action.DrumRed, 200, 520, false}, {action.DrumYellow, 420, 470, false}, {action.DrumBlue, 640, 470, false}, {action.DrumGreen, 860, 520, false},
	}
	var b render.Batch
	for _, p := range pads {
		c := w.actionColor(p.act)
		k := math.Max(0, 1-(now-w.padHit[p.act])/0.25)
		ry := 70.0
		if p.cym {
			ry = 40
		}
		b.Ellipse(dst, p.x, p.y, 100, ry, render.Mix(render.Darken(c, 0.7), render.Lighten(c, 0.4), k), render.Mix(render.Darken(c, 0.8), c, k))
		b.Ring(dst, p.x, p.y, 104, ry+4, 6, c, render.Darken(c, 0.3))
	}
	k := math.Max(0, 1-(now-w.padHit[action.Kick])/0.25)
	b.Rect(dst, 300, 660, 460, 50, render.Mix(render.Darken(th.Lanes[4], 0.7), th.Lanes[4], k), render.Mix(render.Darken(th.Lanes[4], 0.8), th.Lanes[4], k))
	b.Flush(dst)
	for _, p := range pads {
		if now-w.padHit[p.act] < 1.5 {
			render.Text(dst, fmt.Sprintf("%d", int(w.padVel[p.act]*127+0.5)), render.CondBold, 28, p.x, p.y-16, th.Text, render.Center)
		}
	}
	render.Text(dst, "KICK", render.CondBold, 28, 530, 668, th.Text, render.Center)
	render.Text(dst, "Numbers show hit velocity (0-127).", render.Cond, 24, 160, 760, th.TextDim, render.Left)
}
