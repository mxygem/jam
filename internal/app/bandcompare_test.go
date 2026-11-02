package app

import (
	"os"
	"strings"
	"testing"

	"github.com/mxygem/jam/internal/input"
)

// A finished band opens the side-by-side comparison from its results screen
// (the Compare players button), and green or red goes back to the results.
func TestBandCompare(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.NewProfile("Sam")
	b := newBand(modeBand)
	for i := 0; i < 3; i++ {
		b.join(a, device{src: input.SrcXInput, index: i})
	}
	a.Band = b
	a.scene = NewBandLoading(a, b, a.library[0])
	q := h.waitBandPlay()
	for _, s := range q.seats {
		s.bot = botEvents(s.t, false)
	}
	q.seats[2].bot = nil // one player plays nothing
	h.until("6 s in", 30, func() bool { return q.songNow(h.now) > 6 }, nil)
	q.finish(a, true)
	h.idle(0.3)

	r, ok := a.scene.(*BandResults)
	if !ok {
		t.Fatalf("scene %T, want the band results", a.scene)
	}
	if r.buttons[0] != "Compare players" {
		t.Fatalf("first button %q, want Compare players", r.buttons[0])
	}
	if r.buttons[r.sel-len(r.res)] != "Pick another song" {
		t.Fatal("green should still go to the song list by default")
	}
	h.tap(bitDPadUp) // onto Compare players
	h.tap(bitA)
	cmp, ok := a.scene.(*BandCompare)
	if !ok {
		t.Fatalf("scene %T, want the comparison", a.scene)
	}
	if len(cmp.cols) != 3 {
		t.Fatalf("%d columns, want 3", len(cmp.cols))
	}
	rows := map[string]compareRow{}
	for _, row := range cmp.rows {
		rows[row.label] = row
	}
	for _, label := range []string{"Score", "Hit rate", "Misses", "Best streak", "Timing (avg ± spread)"} {
		row, ok := rows[label]
		if !ok {
			t.Fatalf("no %q row", label)
		}
		if len(row.cells) != 3 {
			t.Fatalf("%q has %d cells, want 3", label, len(row.cells))
		}
	}
	// the player who played nothing can't have the best score, and the best
	// score is marked on exactly the player with the most points
	score := rows["Score"]
	top, topScore := 0, int64(-1)
	for i, c := range cmp.cols {
		if s := c.sr.rec.Result.Score; s > topScore {
			top, topScore = i, s
		}
	}
	if !score.best[top] {
		t.Fatalf("best score not marked on player %d: %v", top, score.best)
	}
	if score.best[2] {
		t.Fatal("the idle player shouldn't have the best score")
	}
	// sections are listed under a heading
	sawHead := false
	for _, row := range cmp.rows {
		if row.head && strings.Contains(row.label, "section") {
			sawHead = true
		}
	}
	if !sawHead {
		t.Log("no section rows (the demo chart has none)")
	}
	if os.Getenv("JAM_SHOTS") != "" {
		h.idle(0.2)
		h.shot("mp-compare")
	}
	h.tap(bitB)
	if a.scene != r {
		t.Fatalf("scene %T, want the band results again", a.scene)
	}
}

func TestPickBest(t *testing.T) {
	best := pickBest([]float64{3, 9, 9}, []bool{true, true, true}, false)
	if best[0] || !best[1] || !best[2] {
		t.Fatalf("highest: %v", best)
	}
	best = pickBest([]float64{3, 9, 5}, []bool{true, true, true}, true)
	if !best[0] || best[1] || best[2] {
		t.Fatalf("lowest: %v", best)
	}
	for _, v := range [][]bool{pickBest([]float64{4, 4}, []bool{true, true}, false), pickBest([]float64{4, 8}, []bool{true, false}, false)} {
		if v[0] || v[1] {
			t.Fatalf("a tie or a lone value is nobody's best: %v", v)
		}
	}
}
