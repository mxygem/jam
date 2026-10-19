package profile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mxygem/jam/internal/config"
)

func TestStore(t *testing.T) {
	t.Setenv("JAM_HOME", t.TempDir())
	dir := config.ProfilesDir()
	s, errs := Load(dir)
	if len(errs) > 0 || len(s.All()) != 0 {
		t.Fatalf("empty store: %v %d", errs, len(s.All()))
	}
	st := config.Default()
	st.AudioOffsetMs = 55
	first, err := s.Migrate(st)
	if err != nil || !first.Legacy || first.Player.AudioOffsetMs != 55 {
		t.Fatalf("migrate: %v %+v", err, first)
	}
	if first.BindingsPath() != config.BindingsPath() {
		t.Fatal("the first profile keeps bindings.json")
	}
	sam, _ := s.Create("  Sam   the   drummer  ", config.DefaultPlayer())
	if sam.Name != "Sam the drummer" || sam.Color == first.Color {
		t.Fatalf("create: %q %s", sam.Name, sam.Color)
	}
	long, _ := s.Create("A name that is far too long for a highway", config.DefaultPlayer())
	if len([]rune(long.Name)) > MaxName {
		t.Fatalf("name not trimmed: %q", long.Name)
	}
	anon, _ := s.Create("", config.DefaultPlayer())
	if anon.Name != "Player 4" {
		t.Fatalf("unnamed profile %q", anon.Name)
	}
	_ = os.WriteFile(sam.BindingsPath(), []byte("{}"), 0o644)
	// junk in the folder is skipped
	_ = os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644)
	s2, errs := Load(dir)
	if len(s2.All()) != 4 || len(errs) != 1 {
		t.Fatalf("reload: %d profiles, %d errors", len(s2.All()), len(errs))
	}
	if s2.All()[0].ID != first.ID {
		t.Fatal("the first profile should list first")
	}
	if err := s2.Delete(s2.Get(first.ID)); err == nil {
		t.Fatal("the first profile can't be deleted")
	}
	if err := s2.Delete(s2.Get(sam.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sam.BindingsPath()); !os.IsNotExist(err) {
		t.Fatal("bindings file should go with the profile")
	}
	if !first.Owns("") || !first.Owns(first.ID) || sam.Owns("") {
		t.Fatal("ownership of untagged runs")
	}
}
