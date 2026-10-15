package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// Runs lists recorded playthroughs.
type Runs struct {
	files []string
	cache map[string]*replay.Recording
	list  List
}

// NewRuns opens the runs browser.
func NewRuns(a *App) *Runs {
	r := &Runs{cache: map[string]*replay.Recording{}}
	r.files, _ = replay.List(config.RunsDir())
	for _, f := range r.files {
		f := f
		r.list.Items = append(r.list.Items, Item{
			Label: runLabel(f),
			Value: func() string { return r.summary(f) },
			Activate: func() {
				rec := r.load(f)
				if rec == nil {
					a.Toast("Couldn't read " + filepath.Base(f))
					return
				}
				res := openRun(rec, f)
				res.back = func(a *App) { a.SetScene(NewRuns(a)) }
				a.SetScene(res)
			},
		})
	}
	r.list.Visible = 12
	return r
}

func openRun(rec *replay.Recording, path string) *Results {
	var c *chart.Chart
	var t *chart.Track
	song, err := chart.LoadSongFolder(rec.Song.Dir)
	if err == nil {
		if cc, err := song.LoadChart(); err == nil {
			c = cc
			inst, _ := chart.ParseInstrument(rec.Instrument)
			diff, _ := chart.ParseDifficulty(rec.Difficulty)
			t = c.Track(inst, diff)
		}
	} else {
		song = nil
	}
	r := NewResults(nil, rec, path, c, t, song)
	return r
}

func runLabel(f string) string {
	base := strings.TrimSuffix(filepath.Base(f), ".jamrun.json.gz")
	base = strings.TrimSuffix(base, ".jamrun.json")
	if len(base) > 18 {
		date := base[:17]
		rest := base[18:]
		return truncateW(rest, render.UI, 34, 900) + "   " + date[5:16]
	}
	return base
}

func (r *Runs) load(f string) *replay.Recording {
	if rec, ok := r.cache[f]; ok {
		return rec
	}
	rec, err := replay.Load(f)
	if err != nil {
		r.cache[f] = nil
		return nil
	}
	r.cache[f] = rec
	return rec
}

func (r *Runs) summary(f string) string {
	rec := r.load(f)
	if rec == nil {
		return "unreadable"
	}
	return fmt.Sprintf("%.1f%%  %d", rec.Result.Accuracy*100, rec.Result.Score)
}

func (r *Runs) Update(a *App) error {
	r.list.Update(a)
	if a.Pressed(action.MenuBack) {
		a.SetScene(NewMainMenu(a))
	}
	return nil
}

func (r *Runs) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	Header(dst, "Past runs", fmt.Sprintf("%d recorded in %s", len(r.files), config.RunsDir()))
	if len(r.files) == 0 {
		render.Text(dst, "No runs yet. Every song you play is recorded here with all of its inputs.", render.UI, 30, 120, 280, th.TextDim, render.Left)
	}
	r.list.Draw(dst, 120, 250, 1680, 62)
	Footer(dst, "Enter open     Esc back     Command line: jam analyze <file>")
}
