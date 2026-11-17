package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

// openLibraryItem opens Settings on the library page with item label selected.
func openLibraryItem(t *testing.T, h *harness, label string) *Settings {
	t.Helper()
	s := NewSettingsAt(h.a, pageLibrary)
	h.a.scene = s
	h.idle(0.1)
	for i, it := range s.menu.items.Items {
		if it.Label == label {
			s.menu.items.Sel = i
			return s
		}
	}
	t.Fatalf("no %q item", label)
	return nil
}

// TestPasteSongFolder pastes a path into the add-a-folder box with Ctrl+V.
func TestPasteSongFolder(t *testing.T) {
	h := newHarness(t)
	a := h.a
	dir := filepath.Join(t.TempDir(), "my songs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	clip, clipErr := "\""+dir+"\"\r\n", error(nil) // Explorer's "Copy as path" quotes it
	old := clipboardText
	clipboardText = func() (string, error) { return clip, clipErr }
	t.Cleanup(func() { clipboardText = old })

	s := openLibraryItem(t, h, "Add a song folder...")
	h.key(ebiten.KeyEnter)
	if s.menu.typing == nil {
		t.Fatal("not typing")
	}
	h.shot("add-folder-typing")
	// A plain v types; with Ctrl held it pastes.
	a.Chars = []rune{'v'}
	h.frame(keyDown(ebiten.KeyV))
	a.Chars = nil
	h.frame(keyUp(ebiten.KeyV))
	if got := s.menu.typing.Value; got != "v" {
		t.Fatalf("typed %q", got)
	}
	s.menu.typing.Value = ""
	h.frame(keyDown(ebiten.KeyControlLeft))
	a.Chars = []rune{'v'} // some systems still report the letter
	h.frame(keyDown(ebiten.KeyV))
	a.Chars = nil
	h.frame(keyUp(ebiten.KeyV))
	h.frame(keyUp(ebiten.KeyControlLeft))
	if got := s.menu.typing.Value; got != "\""+dir+"\"" {
		t.Fatalf("pasted %q", got)
	}
	h.shot("add-folder-pasted")
	// Enter is read straight from the keyboard, which tests don't have.
	s.menu.onTyped(s.menu.typing.Value)
	s.menu.typing = nil
	if n := len(a.Settings.LibraryPaths); n == 0 || a.Settings.LibraryPaths[n-1] != dir {
		t.Fatalf("library paths %v", a.Settings.LibraryPaths)
	}
	// The same folder again isn't added twice.
	n := len(a.Settings.LibraryPaths)
	addSongFolder(a, dir+string(filepath.Separator))
	if len(a.Settings.LibraryPaths) != n {
		t.Fatalf("added twice: %v", a.Settings.LibraryPaths)
	}

	// Shift+Insert pastes too; an empty or unreadable clipboard changes nothing.
	tx := &TextInput{Value: "x", Max: 5}
	clip = "abcdefgh"
	h.frame(keyDown(ebiten.KeyShiftLeft))
	a.Raw = append(a.Raw[:0], keyDown(ebiten.KeyInsert))
	tx.Update(a)
	if tx.Value != "xabcd" {
		t.Fatalf("shift+insert gave %q", tx.Value)
	}
	clip = ""
	tx.Update(a)
	clipErr = errors.New("busy")
	tx.Update(a)
	if tx.Value != "xabcd" {
		t.Fatalf("empty paste changed it to %q", tx.Value)
	}
	h.frame(keyUp(ebiten.KeyShiftLeft))
}

// TestBrowseForSongFolder walks the folder browser to a folder and adds it.
func TestBrowseForSongFolder(t *testing.T) {
	h := newHarness(t)
	a := h.a
	root := t.TempDir()
	for _, d := range []string{"charts/complete/Song A", "charts/staging", "charts/.hidden", "music", "$RECYCLE.BIN"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "charts", "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	charts := filepath.Join(root, "charts")
	a.Settings.LibraryPaths = []string{filepath.Join(charts, "staging")}

	s := openLibraryItem(t, h, "Browse for a song folder...")
	h.key(ebiten.KeyEnter)
	b := s.menu.browser
	if b == nil {
		t.Fatal("no browser")
	}
	// It opens beside the last folder added, on the first folder inside.
	if b.dir != charts {
		t.Fatalf("opened at %q, want %q", b.dir, charts)
	}
	var labels []string
	for _, it := range b.list.Items {
		labels = append(labels, it.Label)
	}
	want := []string{"Add this folder", "Up one level", "complete", "staging"}
	if len(labels) != len(want) {
		t.Fatalf("rows %q", labels)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("rows %q, want %q", labels, want)
		}
	}
	if b.list.Sel != 2 {
		t.Fatalf("selection starts on %d", b.list.Sel)
	}
	if b.list.Items[3].Value == nil || b.list.Items[3].Value() != "In the library" {
		t.Fatal("staging should be marked as in the library")
	}
	h.shot("browse-folder")

	// Left goes up (landing on the folder just left), the places are above the root.
	h.key(ebiten.KeyPageUp)
	if b.dir != root || b.paths[b.list.Sel] != charts {
		t.Fatalf("up: at %q on %q", b.dir, b.paths[b.list.Sel])
	}
	b.places = func() []place { return []place{{"Home", root}, {"Music", filepath.Join(root, "music")}} }
	b.open("", "")
	h.idle(0.05)
	h.shot("browse-places")
	a.Chars = []rune{'m'} // typing jumps to a row
	h.frame()
	a.Chars = nil
	if b.list.Sel != 1 {
		t.Fatalf("typing m selected %d", b.list.Sel)
	}
	h.key(ebiten.KeyUp)
	h.key(ebiten.KeyEnter) // Home
	if b.dir != root {
		t.Fatalf("at %q", b.dir)
	}
	// Right opens the selected folder: charts, then complete.
	h.key(ebiten.KeyPageDown)
	h.key(ebiten.KeyPageDown)
	complete := filepath.Join(charts, "complete")
	if b.dir != complete {
		t.Fatalf("at %q, want %q", b.dir, complete)
	}
	// A folder already in the library can't be added again.
	b.open(filepath.Join(charts, "staging"), "")
	if !b.list.Items[0].Disabled {
		t.Fatal("staging is already in the library")
	}
	b.open(complete, "")
	h.idle(0.05)
	h.key(ebiten.KeyUp) // Up one level
	h.key(ebiten.KeyUp) // Add this folder
	h.key(ebiten.KeyEnter)
	if s.menu.browser != nil {
		t.Fatal("the browser should close after adding")
	}
	if n := len(a.Settings.LibraryPaths); a.Settings.LibraryPaths[n-1] != complete {
		t.Fatalf("library paths %v", a.Settings.LibraryPaths)
	}

	// Esc cancels without adding; a folder that's gone opens at the places.
	n := len(a.Settings.LibraryPaths)
	s.menu.openBrowser(a)
	h.idle(0.05)
	h.key(ebiten.KeyEscape)
	if s.menu.browser != nil || len(a.Settings.LibraryPaths) != n {
		t.Fatal("Esc should cancel")
	}
	if b := newFolderBrowser(filepath.Join(root, "gone"), nil, func(string) {}); b.dir != "" {
		t.Fatalf("missing start opened %q", b.dir)
	}
}
