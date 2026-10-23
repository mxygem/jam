package app

import (
	"fmt"
	"image"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/engine"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// sloppyBot is the autoplay bot with human mistakes: strums drift by up to
// spread, and a share of notes is strummed far too late (a miss plus an
// overstrum).
func sloppyBot(tr *chart.Track, seed int64, spread, fumble float64) []action.Event {
	rng := rand.New(rand.NewSource(seed))
	var out []action.Event
	for _, ev := range botEvents(tr, false) {
		if ev.Action == action.StrumDown {
			if ev.Down && rng.Float64() < fumble {
				ev.Time += 0.12
			} else {
				ev.Time += (rng.Float64()*2 - 1) * spread
			}
		}
		out = append(out, ev)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Time < out[j-1].Time; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// recordAttempts plays the demo song's expert guitar n times, each a bit
// better, as the active profile.
func recordAttempts(h *harness, song *chart.Song, n int) (*chart.Chart, *chart.Track, []string) {
	h.t.Helper()
	c, err := song.LoadChart()
	if err != nil {
		h.t.Fatal(err)
	}
	tr := c.Track(chart.Guitar, chart.Expert)
	var paths []string
	for k := 0; k < n; k++ {
		f := float64(k) / float64(max(1, n-1))
		set := engine.DefaultSettings()
		e := engine.New(c, tr, set)
		rec := replay.NewRecorder(song, chart.Guitar, chart.Expert, set)
		rec.Rec.Started = time.Date(2026, 10, 21, 20, k*6, 0, 0, time.Local)
		rec.Rec.Profile, rec.Rec.Player = h.a.Active.ID, h.a.Active.Name
		end := tr.Notes[len(tr.Notes)-1].SustainEnd() + 1
		for _, ev := range sloppyBot(tr, int64(k+3), 0.05-0.03*f, 0.25-0.2*f) {
			e.Process(ev)
			rec.Input(ev, "test")
		}
		e.Advance(end)
		r := rec.Finish(e, end, true)
		p, err := r.Save(config.RunsDir())
		if err != nil {
			h.t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return c, tr, paths
}

// TestResultsPages opens a run's results, waits for its history and draws
// every page (screenshots with JAM_SHOTS), then writes the HTML report.
func TestResultsPages(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	song := h.a.library[0]
	c, tr, paths := recordAttempts(h, song, 5)
	last := paths[len(paths)-1]
	rec, err := replay.Load(last)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResults(h.a, rec, last, c, tr, song)
	h.a.scene = r
	if r.view == nil || len(r.view.Gems) != len(tr.Notes) {
		t.Fatal("results built no run view")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		h.idle(0.05)
		if hist, done := r.history(); done {
			if len(hist.Attempts) != 5 || !hist.Attempts[4].Current {
				t.Fatalf("history has %d attempts", len(hist.Attempts))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("history never finished loading")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for i, name := range resultTabs {
		if r.tab != i {
			t.Fatalf("on page %d, want %d (%s)", r.tab, i, name)
		}
		h.idle(0.1)
		h.shot(fmt.Sprintf("results-%02d-%s", i, strings.ReplaceAll(strings.ToLower(name), " ", "-")))
		h.tap(bitDPadDown) // scrolling never panics, even on pages that don't scroll
		h.tap(bitDPadDown)
		h.tap(bitDPadUp)
		h.tap(bitLB) // orange: next page
	}
	if r.tab != 0 {
		t.Fatalf("pages wrap around: on %d", r.tab)
	}

	// The browser report for the same run.
	hist, _ := r.history()
	out := filepath.Join(t.TempDir(), "report.html")
	if err := replay.SaveReport(out, r.view, hist); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() < 10000 {
		t.Fatalf("report: %v", err)
	}
	if dir := os.Getenv("JAM_SHOTS"); dir != "" {
		data, _ := os.ReadFile(out)
		_ = os.WriteFile(filepath.Join(dir, "report.html"), data, 0o644)
	}
}

// TestResultsSingleRun: a first play's history pages say so instead of
// drawing empty charts.
func TestResultsSingleRun(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	song := h.a.library[0]
	c, tr, paths := recordAttempts(h, song, 1)
	rec, _ := replay.Load(paths[0])
	r := NewResults(h.a, rec, paths[0], c, tr, song)
	h.a.scene = r
	for i := 0; i < 400; i++ {
		h.idle(0.02)
		if _, done := r.history(); done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	hist, done := r.history()
	if !done || len(hist.Attempts) != 1 {
		t.Fatalf("history done %v", done)
	}
	r.tab = tabProgress
	h.idle(0.1)
	h.shot("results-single-progress")
}

// TestBreakdownWrapsAndScrolls: long Breakdown text wraps to the panel and
// up/down scroll through it.
func TestBreakdownWrapsAndScrolls(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	song := h.a.library[0]
	c, tr, paths := recordAttempts(h, song, 1)
	rec, _ := replay.Load(paths[0])
	r := NewResults(h.a, rec, paths[0], c, tr, song)
	r.an.Tips = append(r.an.Tips, strings.Repeat("This tip is long enough that it has to wrap onto more lines in the panel. ", 4))
	for i := 0; i < 30; i++ {
		r.an.Breaks = append(r.an.Breaks, replay.StreakBreak{Time: float64(i), Lost: 10 + i, Cause: "miss", Section: "A section with a fairly long name"})
	}
	h.a.scene = r
	r.tab = 2
	const w = 814.0
	lines := r.breakdownLines(w - 20)
	if len(lines) <= breakdownVisible {
		t.Fatalf("only %d lines; the test needs more than fit", len(lines))
	}
	for _, l := range lines {
		if tw, _ := render.Measure(l.s, render.UI, breakdownSize); tw > w-20 {
			t.Fatalf("line wider than the panel (%.0f px): %q", tw, l.s)
		}
	}
	h.idle(0.1)
	h.shot("breakdown-top")
	for i := 0; i < len(lines)+5; i++ {
		h.tap(bitDPadDown)
	}
	h.idle(0.1)
	if want := len(lines) - breakdownVisible; r.scroll != want {
		t.Fatalf("scrolled to %d, want the last page at %d", r.scroll, want)
	}
	h.shot("breakdown-bottom")
	h.tap(bitDPadUp)
	if r.scroll != len(lines)-breakdownVisible-1 {
		t.Fatalf("up didn't scroll back: %d", r.scroll)
	}
}

// TestResultsLargeCharts: F (here toggleLarge) draws the charts across the
// whole window, is remembered, and switches back. With JAM_SHOTS it also
// draws every visual page large on a 1920 and a 2580 pixel wide window.
func TestResultsLargeCharts(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	song := h.a.library[0]
	c, tr, paths := recordAttempts(h, song, 3)
	last := paths[len(paths)-1]
	rec, _ := replay.Load(last)
	r := NewResults(h.a, rec, last, c, tr, song)
	h.a.scene = r
	for i := 0; i < 400; i++ {
		h.idle(0.02)
		if _, done := r.history(); done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.toggleLarge(h.a) // from the overview: opens the charts large
	if !h.a.Settings.LargeCharts || r.tab != tabTiming || !r.large(h.a) {
		t.Fatalf("large %v on tab %d", h.a.Settings.LargeCharts, r.tab)
	}
	if st, _ := config.Load(); !st.LargeCharts {
		t.Fatal("large charts not saved")
	}
	r.tab = 0
	if r.large(h.a) {
		t.Fatal("the overview pages always draw normally")
	}
	for i := tabTiming; i < len(resultTabs); i++ {
		r.tab, r.scroll = i, 0
		h.idle(0.05)
		name := strings.ReplaceAll(strings.ToLower(resultTabs[i]), " ", "-")
		h.shot("large-" + name)
		shotWide(h, r, 2580, "large-wide-"+name)
		if vizW != vizNormalW || vizH != vizNormalH {
			t.Fatal("the large area leaked into normal drawing")
		}
	}
	r.tab = tabTiming
	r.toggleLarge(h.a)
	if h.a.Settings.LargeCharts || r.large(h.a) {
		t.Fatal("F should switch back to the normal view")
	}
}

// shotWide draws the scene on a window w pixels wide (the canvas in the
// middle, as App.Draw does) and saves it when JAM_SHOTS is set.
func shotWide(h *harness, r *Results, w int, name string) {
	dir := os.Getenv("JAM_SHOTS")
	if dir == "" {
		return
	}
	full := ebiten.NewImage(w, render.ScreenH)
	canvas := ebiten.NewImage(render.ScreenW, render.ScreenH)
	ox := float64(w-render.ScreenW) / 2
	h.a.viewW, h.a.viewH = w, render.ScreenH
	defer func() { h.a.viewW, h.a.viewH = 0, 0 }()
	render.SetStage(full, ox, 0)
	r.Draw(canvas, h.a)
	render.SetStage(nil, 0, 0)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(ox, 0)
	full.DrawImage(canvas, op)
	img := image.NewRGBA(full.Bounds())
	full.ReadPixels(img.Pix)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	_ = png.Encode(f, img)
}
