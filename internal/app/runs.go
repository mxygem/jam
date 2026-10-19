package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/replay"
)

// Runs lists recorded playthroughs: everyone's, or only the active
// profile's (Tab / yellow switches).
type Runs struct {
	all   []string // every recording, newest first
	files []string // the ones listed
	cache map[string]*replay.Recording
	list  List
	mine  bool
}

// runOwners maps recording paths to the profile that played them, read
// once per session in the background (see PeekPlayer).
var runOwners sync.Map

func loadRunOwners(files []string) {
	go func() {
		for _, f := range files {
			if _, ok := runOwners.Load(f); ok {
				continue
			}
			id, _, err := replay.PeekPlayer(f)
			if err != nil {
				id = "?"
			}
			runOwners.Store(f, id)
		}
	}()
}

// NewRuns opens the runs browser.
func NewRuns(a *App) *Runs {
	r := &Runs{cache: map[string]*replay.Recording{}}
	r.all, _ = replay.List(config.RunsDir())
	loadRunOwners(r.all)
	r.build(a)
	return r
}

// ownedKnown reports whether f belongs to the active profile, and whether
// that's known yet.
func ownedKnown(a *App, f string) (owned, known bool) {
	v, ok := runOwners.Load(f)
	if !ok {
		return false, false
	}
	return a.Active == nil || a.Active.Owns(v.(string)), true
}

func (r *Runs) build(a *App) {
	r.files = r.files[:0]
	for _, f := range r.all {
		if r.mine {
			if owned, _ := ownedKnown(a, f); !owned {
				continue
			}
		}
		r.files = append(r.files, f)
	}
	sel := r.list.Sel
	r.list = List{Visible: 12}
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
	r.list.Sel = min(sel, max(0, len(r.list.Items)-1))
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
			t = replay.TrackFor(rec, c.Track(inst, diff))
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
	who := ""
	if rec.Player != "" {
		who = rec.Player + "   "
	}
	return fmt.Sprintf("%s%.1f%%  %d", who, rec.Result.HitRate()*100, rec.Result.Score)
}

func (r *Runs) loading() bool {
	for _, f := range r.all {
		if _, ok := runOwners.Load(f); !ok {
			return true
		}
	}
	return false
}

func (r *Runs) Update(a *App) error {
	if a.Pressed(action.MenuOptions) {
		r.mine = !r.mine
		r.build(a)
		return nil
	}
	if r.mine && a.frame%30 == 0 && len(r.files) < len(r.all) {
		r.build(a) // more owners known
	}
	r.list.Update(a)
	if a.Pressed(action.MenuBack) {
		a.SetScene(NewMainMenu(a))
	}
	return nil
}

func (r *Runs) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	sub := fmt.Sprintf("%d recorded in %s", len(r.files), config.RunsDir())
	if r.mine && a.Active != nil {
		sub = fmt.Sprintf("%d by %s", len(r.files), a.Active.Name)
		if r.loading() {
			sub += " (still looking...)"
		}
	}
	Header(dst, "Past runs", sub)
	if len(r.files) == 0 && r.mine {
		render.Text(dst, "No runs by this profile yet.", render.UI, 30, 120, 280, th.TextDim, render.Left)
	} else if len(r.files) == 0 {
		render.Text(dst, "No runs yet. Every song you play is recorded here with all of its inputs.", render.UI, 30, 120, 280, th.TextDim, render.Left)
	}
	r.list.Draw(dst, 120, 250, 1680, 62)
	who := "Tab / yellow: only mine"
	if r.mine {
		who = "Tab / yellow: everyone's"
	}
	Footer(dst, "Enter open     "+who+"     Esc back     Command line: jam analyze <file>")
}
