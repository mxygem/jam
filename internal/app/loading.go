package app

import (
	"log"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/playback"
	"github.com/mxygem/jam/internal/render"
)

// Loading reads the chart and decodes audio off the main thread.
type Loading struct {
	song  *chart.Song
	inst  chart.Instrument
	diff  chart.Difficulty
	done  chan loadResult
	err   string
	start float64
}

type loadResult struct {
	c     *chart.Chart
	audio *playback.Song
	err   error
}

// NewLoading starts loading a song.
func NewLoading(a *App, s *chart.Song, inst chart.Instrument, diff chart.Difficulty) *Loading {
	l := &Loading{song: s, inst: inst, diff: diff, done: make(chan loadResult, 1), start: clock.Now()}
	go func() {
		c, err := s.LoadChart()
		if err != nil {
			l.done <- loadResult{err: err}
			return
		}
		if a.NoAudio {
			l.done <- loadResult{c: c, audio: playback.Silent(c.LastTime() + 3)}
			return
		}
		au, err := playback.Load(a.Audio, s.Stems, a.FFmpeg)
		l.done <- loadResult{c: c, audio: au, err: err}
	}()
	return l
}

func (l *Loading) Update(a *App) error {
	if l.err != "" {
		if a.Pressed(action.MenuBack) || a.Pressed(action.MenuConfirm) {
			a.SetScene(NewSongSelect(a))
		}
		return nil
	}
	select {
	case r := <-l.done:
		if r.err != nil {
			l.err = r.err.Error()
			log.Printf("load %s: %v", l.song.Dir, r.err)
			return nil
		}
		t := r.c.Track(l.inst, l.diff)
		if t == nil {
			// fall back to the hardest available difficulty
			if ds := r.c.Available(l.inst); len(ds) > 0 {
				t = r.c.Track(l.inst, ds[0])
			}
		}
		if t == nil {
			r.audio.Close()
			l.err = "This chart has no " + l.inst.Title() + " part."
			return nil
		}
		a.SetScene(NewPlay(a, l.song, r.c, t, r.audio))
	default:
	}
	return nil
}

func (l *Loading) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	render.Text(dst, l.song.Name, render.UIBold, 60, render.ScreenW/2, 380, th.Text, render.Center)
	render.Text(dst, l.song.Artist, render.UI, 36, render.ScreenW/2, 460, th.TextDim, render.Center)
	if l.err != "" {
		render.Text(dst, "Couldn't load this song", render.UIBold, 34, render.ScreenW/2, 580, th.Bad, render.Center)
		render.Text(dst, truncateW(l.err, render.UI, 26, 1600), render.UI, 26, render.ScreenW/2, 630, th.Text, render.Center)
		Footer(dst, "Enter / Esc to go back")
		return
	}
	t := clock.Now() - l.start
	var b render.Batch
	for i := 0; i < 5; i++ {
		ph := t*5 - float64(i)*0.6
		s := 0.5 + 0.5*math.Max(0, math.Sin(ph))
		c := th.Lanes[i]
		b.Ellipse(dst, render.ScreenW/2-120+float64(i)*60, 600, 18*s+8, (18*s+8)*0.6, render.Lighten(c, 0.3), c)
	}
	b.Flush(dst)
	render.Text(dst, "Loading "+l.diff.Title()+" "+l.inst.Title()+"...", render.Cond, 30, render.ScreenW/2, 660, th.TextDim, render.Center)
}
