package app

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/render"
)

// startDemo gets the demo song to the moment Play is on screen.
func startDemo(t *testing.T, h *harness) *Play {
	t.Helper()
	withDemoLibrary(h)
	a := h.a
	songs, _ := a.Library()
	a.scene = NewLoading(a, songs[0], 0, 3)
	for i := 0; i < 3000; i++ { // loading runs in the background
		if p, ok := a.scene.(*Play); ok {
			return p
		}
		h.frame()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("song didn't load: %T", a.scene)
	return nil
}

func TestCountInHonored(t *testing.T) {
	for _, lead := range []float64{0.5, 2.5, 5, 10} {
		h := newHarness(t)
		h.a.Settings.LeadIn = lead
		p := startDemo(t, h)
		// A slow first frame must not eat into the count.
		h.now += 1.5
		for !p.drew {
			h.frame()
		}
		begin := h.now
		for !p.started {
			h.frame()
			if h.now > begin+lead+1 {
				break
			}
		}
		if got := h.now - begin; math.Abs(got-lead) > 0.15 {
			t.Errorf("count-in set to %.1f ran %.2f", lead, got)
		}
		p.au.Close()
	}
}

func TestCountInShowsTheWholeCount(t *testing.T) {
	h := newHarness(t)
	h.a.Settings.LeadIn = 5
	p := startDemo(t, h)
	h.idle(0.3)
	if left := -p.songNow(clock.Now()); left < 4 || left > 5 {
		t.Fatalf("%.2f s left", left)
	}
	h.shot("count-in")
	p.au.Close()
}

// TestResumeCountsBackIn holds the song still for the count-in after a pause,
// and the engine finds out what was pressed meanwhile.
func TestResumeCountsBackIn(t *testing.T) {
	h := newHarness(t)
	h.a.Settings.LeadIn = 2
	p := startDemo(t, h)
	defer p.au.Close()
	for !p.started {
		h.frame()
	}
	h.idle(0.3)
	h.frame(keyDown(keyEscape))
	h.frame(input.RawEvent{Src: input.SrcKeyboard, Kind: input.KindKey, Code: int(keyEscape)})
	if !p.paused {
		t.Fatal("not paused")
	}
	at := p.songNow(p.pauseClock)
	h.idle(1)
	h.tap(bitA) // Resume
	if p.paused || !p.resuming {
		t.Fatalf("paused %v resuming %v", p.paused, p.resuming)
	}
	if p.au.Playing() {
		t.Fatal("the song should wait for the count")
	}
	// Start during the count goes back to the menu, and the time holds.
	h.tap(bitStart)
	if !p.paused || p.resuming || p.songNow(p.pauseClock) != at {
		t.Fatalf("paused %v resuming %v, at %v want %v", p.paused, p.resuming, p.songNow(p.pauseClock), at)
	}
	h.idle(0.2)
	h.tap(bitA)
	begin := p.resumeAt - 2
	h.frame(xbtn(bitB, true)) // ready on red
	for p.resuming && h.now < begin+3 {
		h.frame()
	}
	if got := h.now - begin; math.Abs(got-2) > 0.1 {
		t.Fatalf("counted %.2f s", got)
	}
	if d := p.songNow(clock.Now()) - at; d < -0.01 || d > 0.05 {
		t.Fatalf("song moved %.3f s", d)
	}
	if p.engHeld != 1<<1 {
		t.Fatalf("engine should have red held, got %b", p.engHeld)
	}
	h.frame(xbtn(bitB, false))
	h.idle(0.1)
	if p.engHeld != 0 {
		t.Fatalf("engine should see the release, got %b", p.engHeld)
	}
}

func TestBandResumeCountsBackIn(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.Settings.LeadIn = 1
	b := newBand(modeBand)
	b.join(a, device{src: input.SrcXInput, index: 0})
	a.Band = b
	a.scene = NewBandLoading(a, b, a.library[0])
	p := h.waitBandPlay()
	defer p.au.Close()
	for !p.started {
		h.frame()
	}
	h.tap(bitStart)
	if !p.paused {
		t.Fatal("Start should pause")
	}
	at := p.songNow(p.pauseClock)
	h.idle(0.5)
	h.tap(bitA) // Resume
	if !p.resuming || p.au.Playing() {
		t.Fatalf("resuming %v", p.resuming)
	}
	h.frame(xbtn(bitB, true))
	for p.resuming {
		h.frame()
	}
	if d := p.songNow(clock.Now()) - at; d < -0.01 || d > 0.05 {
		t.Fatalf("song moved %.3f s", d)
	}
	if p.seats[0].held != 1<<1 {
		t.Fatalf("engine should have red held, got %b", p.seats[0].held)
	}
	h.frame(xbtn(bitB, false))
}

// TestSongListFromTheGuitar reaches everything from the frets.
func TestSongListFromTheGuitar(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	s := NewSongSelect(a)
	a.scene = s
	h.idle(0.2)
	hold := func(bit int, then func()) {
		h.frame(xbtn(bitY, true))
		h.idle(0.3)
		if bit >= 0 {
			h.tap(bit)
		}
		if then != nil {
			then()
		}
		h.frame(xbtn(bitY, false))
		h.idle(0.05)
	}
	h.frame(xbtn(bitY, true))
	h.idle(0.3)
	h.shot("chord")
	h.frame(xbtn(bitY, false))
	h.idle(0.05)
	if s.ov != ovOptions {
		t.Fatalf("a yellow tap should open the options (ov %v)", s.ov)
	}
	h.tap(bitB)

	sort0 := a.Settings.Browse.Sort
	hold(bitB, nil) // sort
	if a.Settings.Browse.Sort == sort0 || s.ov != ovNone {
		t.Fatalf("yellow+red: sort %q ov %v", a.Settings.Browse.Sort, s.ov)
	}
	hold(bitDPadDown, nil) // order
	if !a.Settings.Browse.Desc {
		t.Fatal("yellow+strum down should reverse the order")
	}
	scale0 := a.Settings.Browse.DiffScale
	hold(bitDPadUp, nil)
	if a.Settings.Browse.DiffScale == scale0 {
		t.Fatal("yellow+strum up should change the scale")
	}
	hold(bitX, nil) // blue: filters
	if s.ov != ovFilter {
		t.Fatalf("yellow+blue: ov %v", s.ov)
	}
	h.tap(bitB)
	hold(bitLB, nil) // orange: search
	if s.ov != ovSearch || !s.box.Keyboard() {
		t.Fatalf("yellow+orange: ov %v", s.ov)
	}
	h.tap(bitDPadDown) // onto a row of letters
	h.tap(bitA)
	h.idle(0.1)
	h.shot("search-keyboard")
	if s.search == "" {
		t.Fatal("the on-screen keyboard should search as it types")
	}
	h.tap(bitStart)
	if s.ov != ovNone {
		t.Fatalf("Start should finish the search (ov %v)", s.ov)
	}
}

// TestOnScreenKeyboardTypesAFilter spells a filter rule with the guitar.
func TestOnScreenKeyboardTypesAFilter(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	s := NewSongSelect(a)
	a.scene = s
	h.idle(0.1)
	s.openFilter(a)
	h.idle(0.05)
	s.filter.list.Sel = 3 // Add a field rule
	h.tap(bitA)
	s.filter.list.Sel = 1 // Contains
	h.tap(bitA)
	if !s.filter.typing {
		t.Fatal("not typing")
	}
	h.frame()
	if !s.filter.input.Keyboard() {
		t.Fatal("a guitar should get the on-screen keyboard")
	}
	// the cursor starts on q: right to w, down to s, press both
	h.tap(bitLB)
	h.tap(bitA)
	h.tap(bitDPadDown)
	h.tap(bitA)
	h.frame(xbtn(bitY, true)) // yellow is a space
	h.frame(xbtn(bitY, false))
	h.idle(0.05)
	if got := a.Settings.Browse.Filter.Fields[0].Text; got != "ws " {
		t.Fatalf("typed %q", got)
	}
	h.tap(bitB) // red deletes
	if got := a.Settings.Browse.Filter.Fields[0].Text; got != "ws" {
		t.Fatalf("after delete %q", got)
	}
	// down to the bottom row, across to Done
	h.tap(bitDPadDown)
	h.tap(bitDPadDown)
	for range 5 {
		h.tap(bitLB)
	}
	h.tap(bitA)
	if s.filter.typing {
		t.Fatal("Done should close the keyboard")
	}
	// A key on the keyboard takes it away again.
	tx := &TextInput{}
	a.padNav = true
	a.Raw = nil
	tx.Update(a)
	if !tx.Keyboard() {
		t.Fatal("should open after a controller")
	}
	a.Raw = []input.RawEvent{keyDown(ebiten.KeyA)}
	tx.Update(a)
	if tx.Keyboard() {
		t.Fatal("typing should hide it")
	}
}

func TestControlsShowAndOverride(t *testing.T) {
	h := newHarness(t)
	a := h.a
	a.scene = NewControls(a)
	c := a.scene.(*Controls)
	h.idle(0.1)
	h.shot("controls")
	rowOf := func(act action.Action) int {
		for i, x := range c.acts {
			if x == act {
				return i
			}
		}
		t.Fatalf("no row for %s", act)
		return 0
	}
	if v := c.list.Items[rowOf(action.Green)].Value(); !strings.Contains(v, "A") || !strings.Contains(v, "Key") {
		t.Fatalf("green shows %q", v)
	}
	// Star power onto the d-pad's left.
	p := a.Bindings.Profile(input.FamilyGuitar)
	c.list.Sel = rowOf(action.StarPower)
	h.tap(bitA)
	if c.cap == nil {
		t.Fatal("green should wait for a control")
	}
	h.shot("controls-capture")
	h.tap(2) // d-pad left
	if c.cap != nil || p.Get(action.StarPower)[len(p.Get(action.StarPower))-1].Short() != "D-pad left" {
		t.Fatalf("star power: %v", p.Get(action.StarPower))
	}
	if len(p.Get(action.Select)) != 2 {
		t.Fatalf("select shouldn't move: %v", p.Get(action.Select))
	}
	saved, err := input.LoadBindings(a.Active.BindingsPath())
	if err != nil || bindsText(saved.Profile(input.FamilyGuitar).Get(action.StarPower)) != bindsText(p.Get(action.StarPower)) {
		t.Fatalf("not saved: %v %v", err, err)
	}
	m := input.NewMapper(p, a.Input.Devices())
	got := m.Map(input.RawEvent{Time: 1, Src: input.SrcXInput, Kind: input.KindButton, Code: 2, Value: 1, Down: true}, nil)
	if len(got) != 1 || got[0].Action != action.StarPower {
		t.Fatalf("play mapping: %v", got)
	}
	// One control per job: taking red's button swaps with green.
	c.list.Sel = rowOf(action.Green)
	h.tap(bitA)
	h.tap(bitB)
	if p.Get(action.Green)[1].Short() != "B" && p.Get(action.Green)[0].Short() != "B" {
		t.Fatalf("green: %v", p.Get(action.Green))
	}
	if !strings.Contains(bindsText(p.Get(action.Red)), "A") {
		t.Fatalf("red should have taken A: %v", p.Get(action.Red))
	}
	// Yellow's card puts it back, swapping red back too.
	h.tap(bitY)
	if c.card == nil {
		t.Fatal("yellow should open the card")
	}
	h.shot("controls-card")
	c.card.Sel = len(c.card.Items) - 2 // Back to the default
	h.tap(bitB)                        // green is on B now
	if c.card != nil || p.Get(action.Green)[0].Code != bitA && p.Get(action.Green)[1].Code != bitA {
		t.Fatalf("default: %v", p.Get(action.Green))
	}
	if !strings.Contains(bindsText(p.Get(action.Red)), "B") {
		t.Fatalf("red: %v", p.Get(action.Red))
	}
	// A button can't be the whammy, an axis can.
	c.list.Sel = rowOf(action.Whammy)
	h.tap(bitA)
	h.tap(bitB)
	if c.cap == nil || c.cap.warn == "" {
		t.Fatal("whammy should refuse a button")
	}
	h.sweep(5, 0, 1, 0.1) // right trigger
	if c.cap != nil || p.Get(action.Whammy)[0].Code != 5 {
		t.Fatalf("whammy: %v", p.Get(action.Whammy))
	}
	// Drum rows.
	c.list.Sel = 0
	h.frame(keyDown(keyRight))
	h.idle(0.05)
	if c.fam != input.FamilyDrums {
		t.Fatalf("family %v", c.fam)
	}
	h.idle(0.05)
	h.shot("controls-drums")
}

func TestDPadWorksInMenus(t *testing.T) {
	h := newHarness(t)
	// A file saved before menu controls existed gets the d-pad...
	path := filepath.Join(t.TempDir(), "b.json")
	b := &input.Bindings{Version: 1}
	g := input.DefaultProfile(input.FamilyGuitar)
	for _, a := range action.MenuSet {
		delete(g.Bindings, a.String())
	}
	b.Profiles = map[input.Family]*input.Profile{input.FamilyGuitar: g}
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	l, err := input.LoadBindings(path)
	if err != nil || len(l.Profile(input.FamilyGuitar).Get(action.MenuLeft)) != 1 {
		t.Fatalf("%v %v", err, l.Profile(input.FamilyGuitar).Get(action.MenuLeft))
	}
	// ...which steps a song list group, and menu bindings never reach play.
	h.a.scene = NewMainMenu(h.a)
	h.frame(xbtn(2, true))
	if !h.a.Pressed(action.MenuLeft) {
		t.Fatal("d-pad left should be menu left")
	}
	h.frame(xbtn(2, false))
	m := input.NewMapper(l.Profile(input.FamilyGuitar), nil)
	if got := m.Map(input.RawEvent{Src: input.SrcXInput, Kind: input.KindButton, Code: 3, Value: 1, Down: true}, nil); len(got) != 0 {
		t.Fatalf("play saw %v", got)
	}
}

func TestInputViewerStats(t *testing.T) {
	v := render.NewInputViewer(false, false)
	press := func(a action.Action, t0, t1 float64) {
		v.Feed(action.Event{Time: t0, Action: a, Down: true, Value: 1})
		v.Feed(action.Event{Time: t1, Action: a})
	}
	for i := 0; i < 5; i++ {
		ti := 1 + float64(i)*0.1
		press(action.StrumDown, ti, ti+0.05)
	}
	n, hold, total, rate, peak := v.Stats(action.StrumDown, 1.5)
	if n != 5 || math.Abs(hold-0.05) > 1e-9 || math.Abs(total-0.25) > 1e-9 || math.Abs(rate-10) > 1e-6 || math.Abs(peak-10) > 1e-6 {
		t.Fatalf("n %d hold %v total %v rate %v peak %v", n, hold, total, rate, peak)
	}
	if _, _, _, r, _ := v.Stats(action.StrumDown, 10); r != 0 {
		t.Fatalf("rate should fall to 0, got %v", r)
	}
	v.Feed(action.Event{Time: 2, Action: action.Green, Down: true, Value: 1})
	if _, hold, total, _, _ := v.Stats(action.Green, 2.5); hold != 0.5 || total != 0.5 {
		t.Fatalf("held press: %v %v", hold, total)
	}
	// MIDI hits never release: every hit counts.
	d := render.NewInputViewer(true, false)
	for i := 0; i < 3; i++ {
		d.Feed(action.Event{Time: float64(i) * 0.25, Action: action.Kick, Down: true, Value: 1})
	}
	if n, _, _, r, _ := d.Stats(action.Kick, 0.6); n != 3 || math.Abs(r-4) > 1e-6 {
		t.Fatalf("kick: %d %v", n, r)
	}
	// The whammy moving isn't a new press.
	v.Feed(action.Event{Time: 3, Action: action.Whammy, Down: true, Value: 0.6})
	v.Feed(action.Event{Time: 3.1, Action: action.Whammy, Down: true, Value: 0.9})
	if n, _, _, _, _ := v.Stats(action.Whammy, 3.2); n != 1 {
		t.Fatalf("whammy presses %d", n)
	}
}

func TestInputViewerInPlay(t *testing.T) {
	h := newHarness(t)
	p := startDemo(t, h)
	for !p.drew {
		h.frame()
	}
	h.tap(bitDPadDown)
	h.tap(bitA)
	h.idle(0.1)
	if n, _, _, _, _ := p.inputs.Stats(action.StrumDown, 0); n != 1 {
		t.Fatalf("strum presses %d", n)
	}
	if n, _, _, _, _ := p.inputs.Stats(action.Green, 0); n != 1 {
		t.Fatalf("green presses %d", n)
	}
	h.shot("input-viewer")
	p.au.Close()
}

func TestRemoveSongFolder(t *testing.T) {
	h := newHarness(t)
	a := h.a
	keep, gone := t.TempDir(), t.TempDir()
	marker := filepath.Join(gone, "song.ini")
	if err := os.WriteFile(marker, []byte("[song]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Settings.LibraryPaths = []string{keep, gone}
	s := NewSettingsAt(a, pageLibrary)
	a.scene = s
	h.idle(0.05)
	for i, it := range s.menu.items.Items {
		if it.Label == "Remove a song folder..." {
			s.menu.items.Sel = i
		}
	}
	h.tap(bitA)
	if s.menu.folders == nil {
		t.Fatal("the list should open")
	}
	h.shot("remove-folder")
	h.tap(bitDPadDown)
	h.tap(bitA) // first press only arms it
	if len(a.Settings.LibraryPaths) != 2 || s.menu.armed != gone {
		t.Fatalf("paths %v armed %q", a.Settings.LibraryPaths, s.menu.armed)
	}
	h.tap(bitA)
	if len(a.Settings.LibraryPaths) != 1 || a.Settings.LibraryPaths[0] != keep {
		t.Fatalf("paths %v", a.Settings.LibraryPaths)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the files should stay on disk")
	}
	saved, _ := config.Load()
	if len(saved.LibraryPaths) != 1 {
		t.Fatalf("saved %v", saved.LibraryPaths)
	}
	h.tap(bitB)
	if s.menu.folders != nil {
		t.Fatal("red should close the list")
	}
}

func TestDeleteTheme(t *testing.T) {
	h := newHarness(t)
	a := h.a
	saveThemeFile(a, "Mine")
	file := filepath.Join(ThemesDir(), "Mine.json")
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	s := NewSettingsAt(a, pageLooks)
	a.scene = s
	h.idle(0.05)
	for i, it := range s.menu.items.Items {
		if it.Label == "Delete this theme" {
			s.menu.items.Sel = i
		}
	}
	h.tap(bitA)
	if _, err := os.Stat(file); err != nil {
		t.Fatal("the first press should only ask")
	}
	h.tap(bitA)
	if _, err := os.Stat(file); err == nil {
		t.Fatal("not deleted")
	}
	if a.Settings.Look.Theme == "file:Mine" {
		t.Fatalf("still on %q", a.Settings.Look.Theme)
	}
	// Built-ins stay.
	a.Settings.Look.Theme = render.BuiltinThemes[0].Name
	n := len(themeEntries(a))
	h.tap(bitA)
	h.tap(bitA)
	if len(themeEntries(a)) != n {
		t.Fatal("built-ins can't be deleted")
	}
	// The custom look can go too.
	editLook(a, func(_ *render.Theme, l *render.Look) { l.BeatLines = 2 })
	deleteTheme(a, themeCustom)
	if a.Settings.Look.Custom != nil || a.Settings.Look.Theme == themeCustom {
		t.Fatalf("custom left: %+v", a.Settings.Look)
	}
}

func TestLineSettingsChangeTheHighway(t *testing.T) {
	h := newHarness(t)
	if os.Getenv("JAM_SHOTS") == "" {
		t.Skip("reads pixels: JAM_SHOTS only")
	}
	a := h.a
	p := startDemo(t, h)
	base := render.CurrentLook
	defer render.SetLook(base)
	for !p.drew {
		h.frame()
	}
	h.idle(0.5)
	for _, set := range []struct {
		name string
		b, l float64
	}{{"off", 0, 0}, {"normal", 1, 1}, {"max", 3, 3}} {
		l := base
		l.Texture, l.BeatLines, l.LaneLines = render.TextureNone, set.b, set.l
		render.SetLook(l)
		h.idle(0.05)
		h.shot("lines-" + set.name)
	}
	_ = a
	p.au.Close()
	fmt.Fprintln(os.Stderr, "lines shots written")
}
