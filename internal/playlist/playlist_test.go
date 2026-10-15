package playlist

import (
	"testing"
	"time"
)

func TestPlaylistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := &Playlist{Name: "Late night: bass/practice?"}
	p.Entries = []Entry{
		{Dir: `E:\charts\a`, Title: "A - One", Instrument: "bass", Difficulty: "expert"},
		{Dir: `E:\charts\b`, Title: "B - Two", Instrument: "bass", Difficulty: "hard"},
		{Dir: `E:\charts\c`, Title: "C - Three", Instrument: "guitar", Difficulty: "expert"},
	}
	if j := p.Move(2, -1); j != 1 || p.Entries[1].Title != "C - Three" {
		t.Fatalf("move: %d %+v", j, p.Entries)
	}
	if j := p.Move(0, -1); j != 0 {
		t.Fatal("moving past the top should stay put")
	}
	p.Remove(0)
	if err := p.Save(dir); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	q := &Playlist{Name: "Warmup", Entries: []Entry{{Dir: "x", Title: "X"}}}
	if err := q.Save(dir); err != nil {
		t.Fatal(err)
	}
	saved := Saved(dir)
	if len(saved) != 2 || saved[0].Name != "Warmup" || saved[1].Name != "Late night: bass/practice?" || len(saved[1].Entries) != 2 {
		t.Fatalf("saved: %+v", saved)
	}
	if err := saved[1].Rename(dir, "Bass night"); err != nil {
		t.Fatal(err)
	}
	if s := Saved(dir); len(s) != 2 || s[0].Name != "Bass night" {
		t.Fatalf("after rename: %+v", s)
	}
	if err := Delete(dir, "Warmup"); err != nil {
		t.Fatal(err)
	}
	if s := Saved(dir); len(s) != 1 {
		t.Fatalf("after delete: %d", len(s))
	}
	if err := (&Playlist{}).Save(dir); err != ErrNoName {
		t.Fatalf("unnamed save: %v", err)
	}
}
