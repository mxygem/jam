package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mxygem/jam/internal/render"
)

// TestAppearancePage walks the themes, edits a color into a custom look,
// saves it as a theme file and picks it again.
func TestAppearancePage(t *testing.T) {
	h := newHarness(t)
	a := h.a
	s := NewSettingsAt(a, pageLooks)
	a.scene = s
	h.idle(2.3) // let the preview play into the first notes
	h.shot("looks")
	if s.look == nil {
		t.Fatal("no preview on the Appearance page")
	}
	if s.look.eng.Stats().NotesHit == 0 {
		t.Error("the preview didn't hit any notes")
	}
	for i, spec := range render.BuiltinThemes {
		if got := currentTheme(a).spec.Name; got != spec.Name {
			t.Fatalf("theme %d is %q, want %q", i, got, spec.Name)
		}
		h.shot("looks-theme-" + strings.ReplaceAll(strings.ToLower(spec.Name), " ", "-"))
		h.key(keyRight)
	}
	if a.Settings.Look.Theme != render.BuiltinThemes[0].Name {
		t.Fatalf("cycling all the way round ended on %q", a.Settings.Look.Theme)
	}

	// Neon, then change the green fret's color: a custom look from Neon.
	h.key(keyRight)
	green, _ := render.Slot("green")
	neon := green.Get(&render.DefaultTheme)
	for s.menu.items.Items[s.menu.items.Sel].Label != "Green" {
		h.key(keyArrowDown)
	}
	h.shot("looks-colors")
	h.key(keyEnter)
	if s.menu.picker == nil {
		t.Fatal("Enter on a color should open the color editor")
	}
	h.key(keyEnter) // edit hue
	for i := 0; i < 8; i++ {
		h.key(keyArrowUp)
	}
	h.key(keyEscape) // stop editing the hue
	h.shot("looks-picker")
	if got := green.Get(&render.DefaultTheme); got == neon {
		t.Fatal("hue change didn't change the color")
	}
	if a.Settings.Look.Theme != themeCustom || a.Settings.Look.Custom == nil || a.Settings.Look.Custom.Base != "Neon" {
		t.Fatalf("look %q, custom %+v", a.Settings.Look.Theme, a.Settings.Look.Custom)
	}
	if render.CurrentLook.Material != render.MatNeon {
		t.Error("the custom look lost Neon's material")
	}
	// Use the theme's color goes back to Neon's green.
	for s.menu.picker.list.Items[s.menu.picker.list.Sel].Label != "Use the theme's color" {
		h.key(keyArrowDown)
	}
	h.key(keyEnter)
	if got := green.Get(&render.DefaultTheme); got != neon {
		t.Fatalf("theme's color gave %v, want %v", got, neon)
	}
	h.key(keyEscape)
	if s.menu.picker != nil || s.menu.pageID() != pageLooks {
		t.Fatal("Esc should close the color editor and stay on the page")
	}

	// Material: left/right steps it.
	s.menu.items.Sel = 0
	for s.menu.items.Items[s.menu.items.Sel].Label != "Note material" {
		h.key(keyArrowDown)
	}
	h.key(keyRight)
	if render.CurrentLook.Material != render.MatFlat {
		t.Fatalf("material after Neon is %v", render.CurrentLook.Material)
	}
	h.shot("looks-flat")

	// Save as a file and find it in the theme list.
	saveThemeFile(a, "My Neon")
	if _, err := os.Stat(filepath.Join(ThemesDir(), "My Neon.json")); err != nil {
		t.Fatal(err)
	}
	if a.Settings.Look.Theme != "file:My Neon" || currentTheme(a).spec.Material != "flat" {
		t.Fatalf("after saving: %q %+v", a.Settings.Look.Theme, currentTheme(a).spec)
	}
	// Restarting with these settings brings the look back.
	render.DefaultTheme = render.BaseTheme()
	render.SetLook(render.DefaultLook())
	applyLook(a)
	if render.CurrentLook.Material != render.MatFlat || green.Get(&render.DefaultTheme) != neon {
		t.Fatal("the saved theme didn't come back")
	}

	// Drums in the preview.
	s.menu.previewDrums = true
	h.idle(2.3)
	h.shot("looks-drums")
	if !s.look.drums || s.look.eng.Stats().NotesHit == 0 {
		t.Fatal("drum preview isn't playing")
	}
}

// TestEveryMaterialDraws draws a song in each material and rim.
func TestEveryMaterialDraws(t *testing.T) {
	h := newHarness(t)
	a := h.a
	s := NewSettingsAt(a, pageLooks)
	a.scene = s
	h.idle(2.3)
	for m := render.MatClassic; m <= render.MatFlat; m++ {
		for _, hw := range []render.HighwayStyle{render.HwSolid, render.HwLanes, render.HwGlass, render.HwGradient} {
			editLook(a, func(_ *render.Theme, l *render.Look) {
				l.Material, l.Highway, l.Rim = m, hw, render.Rim(int(m)%5)
			})
			h.idle(0.05)
		}
		h.shot("material-" + m.String())
	}
}

// TestPauseAppearance changes colors from the pause menu: they show on
// the song in progress straight away.
func TestPauseAppearance(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.scene = NewSongSelect(a)
	h.tap(bitA)
	h.tap(bitA)
	h.until("play", 10, sceneIs[*Play](h), nil)
	h.until("2 s in", 10, func() bool { return a.scene.(*Play).songNow(h.now) > 2 }, nil)
	p := a.scene.(*Play)
	h.tap(bitStart)
	h.tap(bitDPadDown)
	h.tap(bitDPadDown) // Settings
	h.tap(bitA)
	h.tap(bitDPadDown)
	h.tap(bitDPadDown) // Appearance
	h.tap(bitA)
	if p.pset.pageID() != pageLooks {
		t.Fatalf("page %q", p.pset.pageID())
	}
	for _, it := range p.pset.items.Items {
		if it.Label == "Save as a theme file..." {
			t.Error("the pause menu shouldn't save files")
		}
	}
	h.key(keyRight) // next theme
	if a.Settings.Look.Theme != "Neon" || render.CurrentLook.Material != render.MatNeon {
		t.Fatalf("theme %q material %v", a.Settings.Look.Theme, render.CurrentLook.Material)
	}
	for p.pset.items.Items[p.pset.items.Sel].Label != "Red" {
		h.tap(bitDPadDown)
	}
	h.tap(bitA)
	if p.pset.picker == nil {
		t.Fatal("no color editor")
	}
	h.tap(bitDPadDown) // saturation
	h.tap(bitA)
	for i := 0; i < 30; i++ {
		h.tap(bitDPadDown)
	}
	h.tap(bitB)
	h.shot("pause-picker")
	red, _ := render.Slot("red")
	if _, s, _ := render.HSV(red.Get(&render.DefaultTheme)); s > 0.75 {
		t.Errorf("saturation still %.2f", s)
	}
	h.tap(bitB) // close the editor
	h.tap(bitB) // back to the pages
	h.tap(bitB) // back to the pause menu
	if p.pset != nil {
		t.Fatal("settings still open")
	}
	h.tap(bitB) // resume
	if p.paused {
		t.Fatal("still paused")
	}
	if a.Settings.Look.Theme != themeCustom {
		t.Errorf("theme %q after editing", a.Settings.Look.Theme)
	}
}

// TestHighwayTextures steps through every highway texture on the
// Appearance page, including an image the player added, which stays out
// of the background list.
func TestHighwayTextures(t *testing.T) {
	h := newHarness(t)
	a := h.a
	dir := filepath.Join(CustomsDir(), "highways")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 256, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y / 2), 0x80, 0xFF})
		}
	}
	f, err := os.Create(filepath.Join(dir, "My board.png"))
	if err != nil {
		t.Fatal(err)
	}
	_ = png.Encode(f, img)
	f.Close()
	if bg := scanBackgrounds(CustomsDir()); len(bg) != 0 {
		t.Errorf("highway images listed as backgrounds: %v", bg)
	}

	s := NewSettingsAt(a, pageLooks)
	a.scene = s
	h.idle(2.3)
	for s.menu.items.Items[s.menu.items.Sel].Label != "Highway texture" {
		h.key(keyArrowDown)
	}
	h.key(keyEnter) // look for new images
	want := render.HighwayTextures()
	if want[len(want)-1] != "custom:highways/My board.png" {
		t.Fatalf("texture list %v doesn't end with the player's image", want)
	}
	start := render.CurrentLook.Texture
	i := 0
	for k, id := range want {
		if id == start {
			i = k
		}
	}
	for n := 1; n <= len(want); n++ {
		h.key(keyRight)
		got := render.CurrentLook.Texture
		if exp := want[(i+n)%len(want)]; got != exp {
			t.Fatalf("step %d: texture %q, want %q", n, got, exp)
		}
		h.idle(0.1)
		h.shot("texture-" + strings.ToLower(strings.ReplaceAll(render.TextureLabel(got), " ", "-")))
	}
	if a.Settings.Look.Theme != themeCustom || a.Settings.Look.Custom.Texture != start {
		t.Fatalf("custom look texture %q, want %q", a.Settings.Look.Custom.Texture, start)
	}
	// Strength goes into the custom look too.
	h.key(keyArrowDown)
	if l := s.menu.items.Items[s.menu.items.Sel].Label; l != "Texture strength" {
		t.Fatalf("after Highway texture comes %q", l)
	}
	before := render.CurrentLook.TextureStrength
	h.key(keyLeft)
	if render.CurrentLook.TextureStrength >= before || *a.Settings.Look.Custom.TextureStrength != render.CurrentLook.TextureStrength {
		t.Fatalf("strength %.2f -> %.2f (saved %v)", before, render.CurrentLook.TextureStrength, *a.Settings.Look.Custom.TextureStrength)
	}
}
