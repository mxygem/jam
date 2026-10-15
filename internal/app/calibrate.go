package app

import (
	"fmt"
	"log"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/playback"
	"github.com/mxygem/jam/internal/render"
)

const (
	calInterval = 0.5 // seconds between beats (120 BPM)
	calLead     = 2.0 // count-in before the first measured beat
	calBeats    = 20
)

type calMode int

const (
	calMenu calMode = iota
	calAudio
	calVideo
	calDone
)

// Calibration measures audio and video latency by having the player tap
// along to a click track or a visual metronome.
type Calibration struct {
	mode    calMode
	which   calMode
	list    List
	start   float64
	player  *audio.Player
	offsets []float64
	result  float64
	spread  float64
	done    List
}

// NewCalibration opens the calibration screen.
func NewCalibration(a *App) *Calibration {
	c := &Calibration{}
	c.list = List{Visible: 3, Items: []Item{
		{Label: "Audio latency", Value: func() string { return fmt.Sprintf("%.0f ms", a.Settings.AudioOffsetMs) },
			Hint: "Strum (or hit anything) on each click you HEAR", Activate: func() { c.begin(a, calAudio) }},
		{Label: "Video latency", Value: func() string { return fmt.Sprintf("%.0f ms", a.Settings.VideoOffsetMs) },
			Hint: "Strum when each gem you SEE crosses the line", Activate: func() { c.begin(a, calVideo) }},
		{Label: "Back", Activate: func() { a.SetScene(NewMainMenu(a)) }},
	}}
	return c
}

func (c *Calibration) begin(a *App, m calMode) {
	c.mode, c.which = m, m
	c.offsets = nil
	if m == calAudio && a.Audio != nil {
		total := calLead + float64(calBeats)*calInterval + 1
		pcm := playback.NewPCM(total)
		for k := 0; ; k++ {
			t := float64(k) * calInterval
			if t > total-0.5 {
				break
			}
			pcm.Add(t, playback.Click(k%4 == 0), 1)
		}
		c.player = a.Audio.NewPlayerFromBytes(pcm.Bytes())
		c.player.SetVolume(a.Settings.Volume)
		c.player.Play()
	}
	c.start = clock.Now()
}

// beatTime returns the clock time of measured beat k.
func (c *Calibration) beatTime(k int) float64 { return c.start + calLead + float64(k)*calInterval }

func (c *Calibration) Update(a *App) error {
	switch c.mode {
	case calMenu:
		c.list.Update(a)
		if a.Pressed(action.MenuBack) {
			a.SetScene(NewMainMenu(a))
		}
	case calAudio, calVideo:
		for _, e := range a.Raw {
			if !e.Down || e.Kind == input.KindAxis || e.Kind == input.KindCC {
				continue
			}
			if e.Src == input.SrcKeyboard && ebiten.Key(e.Code) == ebiten.KeyEscape {
				c.stop()
				c.mode = calMenu
				return nil
			}
			rel := (e.Time - c.beatTime(0)) / calInterval
			k := int(math.Round(rel))
			if k < 0 || k >= calBeats {
				continue
			}
			off := e.Time - c.beatTime(k)
			if math.Abs(off) < 0.22 {
				c.offsets = append(c.offsets, off*1000)
			}
		}
		if clock.Now() > c.beatTime(calBeats-1)+0.6 {
			c.finish(a)
		}
	case calDone:
		c.done.Update(a)
		if a.Pressed(action.MenuBack) {
			c.mode = calMenu
		}
	}
	return nil
}

func (c *Calibration) stop() {
	if c.player != nil {
		c.player.Pause()
		c.player.Close()
		c.player = nil
	}
}

func (c *Calibration) finish(a *App) {
	c.stop()
	c.mode = calDone
	if len(c.offsets) < 6 {
		c.result = math.NaN()
	} else {
		s := append([]float64(nil), c.offsets...)
		sort.Float64s(s)
		// trimmed mean of the middle half is robust to stray hits
		lo, hi := len(s)/4, len(s)-len(s)/4
		sum := 0.0
		for _, v := range s[lo:hi] {
			sum += v
		}
		c.result = sum / float64(hi-lo)
		v := 0.0
		for _, x := range s[lo:hi] {
			v += (x - c.result) * (x - c.result)
		}
		c.spread = math.Sqrt(v / float64(hi-lo))
	}
	items := []Item{}
	if !math.IsNaN(c.result) {
		items = append(items, Item{Label: fmt.Sprintf("Use %.0f ms", c.result), Activate: func() {
			if c.which == calAudio {
				a.Settings.AudioOffsetMs = math.Round(c.result)
			} else {
				a.Settings.VideoOffsetMs = math.Round(c.result)
			}
			_ = a.Settings.Save()
			log.Printf("calibration: %s offset set to %.0f ms (spread %.0f ms, %d taps)", map[calMode]string{calAudio: "audio", calVideo: "video"}[c.which], math.Round(c.result), c.spread, len(c.offsets))
			a.Toast("Calibration saved")
			c.mode = calMenu
		}})
	}
	items = append(items,
		Item{Label: "Try again", Activate: func() { c.begin(a, c.which) }},
		Item{Label: "Back", Activate: func() { c.mode = calMenu }},
	)
	c.done = List{Visible: 3, Items: items}
}

func (c *Calibration) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	Header(dst, "Calibrate latency", "Audio offset delays judging; video offset shifts what you see")
	switch c.mode {
	case calMenu:
		c.list.Draw(dst, 120, 260, 900, 80)
		render.Text(dst, c.list.Hint(), render.UI, 28, 120, 520, th.TextDim, render.Left)
		render.Text(dst, "Do audio first. Tap steadily with the beat rather than reacting to it.", render.UI, 26, 120, 580, th.TextDim, render.Left)
		Footer(dst, "Enter select     Esc back")
	case calAudio, calVideo:
		now := clock.Now()
		rel := (now - c.beatTime(0)) / calInterval
		if c.mode == calAudio {
			render.Text(dst, "Strum on every click", render.Display, 72, render.ScreenW/2, 340, th.Text, render.Center)
		} else {
			render.Text(dst, "Strum as each gem crosses the line", render.Display, 64, render.ScreenW/2, 240, th.Text, render.Center)
			c.drawVisual(dst, now)
		}
		if rel < 0 {
			render.Text(dst, fmt.Sprintf("starting in %d", int(math.Ceil(-rel*calInterval))), render.UI, 34, render.ScreenW/2, 460, th.TextDim, render.Center)
		} else {
			render.Text(dst, fmt.Sprintf("%d / %d", min(calBeats, int(rel)+1), calBeats), render.UIBold, 40, render.ScreenW/2, 460, th.TextDim, render.Center)
		}
		if n := len(c.offsets); n > 0 {
			render.Text(dst, fmt.Sprintf("last %+.0f ms", c.offsets[n-1]), render.Cond, 30, render.ScreenW/2, 900, th.TextDim, render.Center)
		}
		Footer(dst, "Esc cancel")
	case calDone:
		if math.IsNaN(c.result) {
			render.Text(dst, "Not enough taps to measure. Try again?", render.UIBold, 40, 120, 260, th.Late, render.Left)
		} else {
			render.Text(dst, fmt.Sprintf("Measured %.0f ms", c.result), render.Display, 80, 120, 240, th.Text, render.Left)
			render.Text(dst, fmt.Sprintf("from %d taps, spread ±%.0f ms", len(c.offsets), c.spread), render.UI, 30, 124, 340, th.TextDim, render.Left)
		}
		c.done.Draw(dst, 120, 440, 700, 80)
		Footer(dst, "Enter select     Esc back")
	}
}

func (c *Calibration) drawVisual(dst *ebiten.Image, now float64) {
	lineY := 760.0
	var b render.Batch
	b.Rect(dst, 760, 320, 400, 560, render.WithAlpha(th.Highway, 0.9), render.WithAlpha(th.Highway, 0.9))
	b.Rect(dst, 740, lineY-3, 440, 6, th.Rail, th.Rail)
	for k := 0; k < calBeats; k++ {
		dt := c.beatTime(k) - now
		if dt < -0.3 || dt > 1.0 {
			continue
		}
		y := lineY - dt*440
		a := 1.0
		if dt < 0 {
			a = math.Max(0, 1+dt/0.3)
		}
		col := th.Lanes[k%5]
		b.Ellipse(dst, 960, y, 70, 38, render.WithAlpha(render.Lighten(col, 0.3), a), render.WithAlpha(col, a))
	}
	b.Flush(dst)
}
