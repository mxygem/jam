package app

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/render"
)

// folderBrowser walks the disk to pick a folder, with the mouse, the
// keyboard or a guitar. It starts at dir; "" is the list of places (drives
// and the home folder).
type folderBrowser struct {
	dir   string
	list  List
	paths []string          // the folder each row opens ("" for the other rows)
	names []string          // what typing matches a row by, lower case
	pick  func(string)      // called with the chosen folder
	taken func(string) bool // folders that can't be picked again
	note  string            // why the folder couldn't be listed

	typed   string // letters typed to jump to a folder
	typedAt float64

	places func() []place // tests swap this
}

// place is somewhere to start browsing from.
type place struct{ label, path string }

func newFolderBrowser(start string, taken func(string) bool, pick func(string)) *folderBrowser {
	b := &folderBrowser{pick: pick, taken: taken, places: browsePlaces}
	if st, err := os.Stat(start); start == "" || err != nil || !st.IsDir() {
		start = ""
	}
	b.open(start, "")
	return b
}

// browsePlaces lists where browsing can start: the home folder, then every
// drive (Windows) or the root and anything mounted (elsewhere).
func browsePlaces() []place {
	var out []place
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = append(out, place{"Home (" + home + ")", home})
	}
	if runtime.GOOS == "windows" {
		for _, d := range drives() {
			out = append(out, place{d, d})
		}
		return out
	}
	out = append(out, place{"/", "/"})
	mounts := []string{"/Volumes", "/media", "/mnt"}
	if u := os.Getenv("USER"); u != "" {
		mounts = append(mounts, filepath.Join("/media", u), filepath.Join("/run/media", u))
	}
	for _, m := range mounts {
		for _, name := range subfolders(m) {
			out = append(out, place{filepath.Join(m, name), filepath.Join(m, name)})
		}
	}
	return out
}

// subfolders lists the folders in dir by name, leaving out hidden and
// system ones. Links to folders count.
func subfolders(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") || name == "System Volume Information" {
			continue
		}
		if e.IsDir() {
			out = append(out, name)
		} else if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil && st.IsDir() {
				out = append(out, name)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// parentDir is the folder above dir: "" (the places) above a drive or the root.
func parentDir(dir string) string {
	if dir == "" {
		return ""
	}
	if p := filepath.Dir(dir); p != dir {
		return p
	}
	return ""
}

// open lists dir. sel, if set, is the folder to put the selection on (the
// one just left, when going up).
func (b *folderBrowser) open(dir, sel string) {
	b.dir, b.note, b.typed = dir, "", ""
	var items []Item
	b.paths, b.names = nil, nil
	row := func(it Item, path, name string) {
		items = append(items, it)
		b.paths = append(b.paths, path)
		b.names = append(b.names, strings.ToLower(name))
	}
	if dir == "" {
		for _, p := range b.places() {
			row(Item{Label: truncateW(p.label, render.UI, 34, 740), Hint: "Open " + p.path,
				Activate: func() { b.open(p.path, "") }}, p.path, p.label)
		}
		row(Item{Label: "Cancel", Activate: func() { b.pick("") }, Gap: 10}, "", "")
	} else {
		add := Item{Label: "Add this folder", Button: true, Hint: "Add " + dir + " and every song under it to the library",
			Activate: func() { b.pick(dir) }}
		if b.taken != nil && b.taken(dir) {
			add = Item{Label: "Already in the library", Button: true, Disabled: true}
		}
		up := "Drives and places"
		if p := parentDir(dir); p != "" {
			up = p
		}
		row(add, "", "")
		row(Item{Label: "Up one level", Gap: 10, Hint: "Back to " + up,
			Value:    func() string { return truncateW(up, render.UI, 32, 420) },
			Activate: func() { b.up() }}, "", "")
		if _, err := os.ReadDir(dir); err != nil {
			b.note = "Can't open this folder: " + shortErr(err)
		}
		for _, name := range subfolders(dir) {
			full := filepath.Join(dir, name)
			it := Item{Label: truncateW(name, render.UI, 34, 740), Hint: "Open " + full, Activate: func() { b.open(full, "") }}
			if b.taken != nil && b.taken(full) {
				it.Value = func() string { return "In the library" }
				it.Label = truncateW(name, render.UI, 34, 500)
			}
			row(it, full, name)
		}
	}
	b.list = List{Items: items, Visible: 9}
	// Start on the first folder when there is one, so green keeps going
	// deeper; on "Add this folder" when there's nowhere further to go.
	if dir != "" && len(items) > 2 {
		b.list.Sel = 2
	}
	for i, p := range b.paths {
		if sel != "" && p != "" && samePath(p, sel) {
			b.list.Sel = i
		}
	}
	if b.list.Sel >= b.list.Visible {
		b.list.Top = min(b.list.Sel, len(items)-b.list.Visible)
	}
}

func shortErr(err error) string {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err.Error()
	}
	return err.Error()
}

func (b *folderBrowser) up() {
	if b.dir != "" {
		b.open(parentDir(b.dir), b.dir)
	}
}

// update handles input. It returns true when the player backs out.
func (b *folderBrowser) update(a *App) bool {
	// Left goes up a level and right opens, like a file tree.
	for _, act := range a.MenuActs {
		switch act {
		case action.MenuLeft:
			b.up()
			return false
		case action.MenuRight:
			if p := b.paths[b.list.Sel]; p != "" {
				b.open(p, "")
				return false
			}
		}
	}
	b.jump(a)
	if b.list.Update(a) {
		return false
	}
	return a.Pressed(action.MenuBack) || a.Mouse.Right
}

// jump moves to the first folder starting with the letters just typed.
func (b *folderBrowser) jump(a *App) {
	now := clock.Now()
	for _, r := range a.Chars {
		if r < 32 || r == 127 {
			continue
		}
		if now-b.typedAt > 1 {
			b.typed = ""
		}
		b.typed += strings.ToLower(string(r))
		b.typedAt = now
		for i, name := range b.names {
			if b.paths[i] != "" && strings.HasPrefix(name, b.typed) {
				b.list.Sel = i
				break
			}
		}
	}
}

// where is the line under the title: the folder being shown.
func (b *folderBrowser) where() string {
	if b.dir == "" {
		return "Pick a drive or place to look in"
	}
	return b.dir
}

func (b *folderBrowser) hint() string {
	if b.note != "" {
		return b.note
	}
	h := b.list.Hint()
	if b.dir != "" && len(b.list.Items) == 2 && b.list.Sel != 0 {
		h = "No folders in here. " + h
	}
	return h
}

// samePath compares folders the way the system does: ignoring case on
// Windows, and a trailing slash anywhere.
func samePath(x, y string) bool {
	x, y = filepath.Clean(x), filepath.Clean(y)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(x, y)
	}
	return x == y
}
