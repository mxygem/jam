package app

import (
	"fmt"
	"testing"

	"github.com/mxygem/jam/internal/chart"
)

// Random picks only from the rows on screen (filters and search are
// already applied to them), never the current song, and works through the
// list before repeating.
func TestRandomSong(t *testing.T) {
	s := &SongSelect{}
	s.rows = append(s.rows, row{header: "A"})
	for i := 0; i < 6; i++ {
		s.rows = append(s.rows, row{song: &chart.Song{Dir: fmt.Sprint("song", i)}})
	}
	s.rows = append(s.rows, row{header: "B"})
	s.rows = append(s.rows, row{song: &chart.Song{Dir: "song6"}})
	s.fixSel(1)

	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		prev := s.current()
		if !s.random() {
			t.Fatal("no pick")
		}
		cur := s.current()
		if cur == nil || cur == prev {
			t.Fatalf("pick %d: %v after %v", i, cur, prev)
		}
		if seen[cur.Dir] {
			t.Fatalf("pick %d repeated %s before using the rest", i, cur.Dir)
		}
		seen[cur.Dir] = true
	}

	one := &SongSelect{rows: []row{{song: &chart.Song{Dir: "only"}}}}
	if one.random() {
		t.Fatal("picked with nothing else to pick")
	}
}
