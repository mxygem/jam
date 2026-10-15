package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A settings file from before the engine options existed keeps its values
// and gains the new defaults.
func TestLoadOldSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JAM_HOME", dir)
	old := `{"library_paths":["E:\\charts"],"engine":{"early_window":0.07,"late_window":0.07,"strum_lenience":0.06,"no_fail":true,"pro_drums":true,"bass_max_mult":4},"volume":0.5}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, first := Load()
	if first {
		t.Fatal("existing file treated as first run")
	}
	e := s.Engine
	if e.StrumLenience != 0.08 || e.ChordLenience != 0.05 || e.DrumWindow != 0.05 || !e.InfiniteFrontEnd || e.Rules < 1 {
		t.Errorf("engine settings not upgraded: %+v", e)
	}
	if s.Volume != 0.5 || len(s.LibraryPaths) != 1 {
		t.Errorf("existing values lost: volume %v paths %v", s.Volume, s.LibraryPaths)
	}
}
