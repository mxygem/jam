package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	for in, want := range map[string]string{
		"~":                                    home,
		"~/":                                   home,
		"~/.clonehero/Songs":                   filepath.Join(home, ".clonehero", "Songs"),
		"~" + string(filepath.Separator) + "x": filepath.Join(home, "x"),
		"~other/x":                             "~other/x",
		"/abs/~/x":                             "/abs/~/x",
		"songs":                                "songs",
	} {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAddSongFolderTilde(t *testing.T) {
	h := newHarness(t)
	a := h.a
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".clonehero", "Songs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	addSongFolder(a, "~/.clonehero/Songs")
	if n := len(a.Settings.LibraryPaths); n == 0 || a.Settings.LibraryPaths[n-1] != dir {
		t.Fatalf("library paths %v, want %s added", a.Settings.LibraryPaths, dir)
	}
}
