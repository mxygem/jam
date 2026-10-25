package app

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/engine"
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

	// Video calibration plays a tiny chart on the real highway.
	vis *calVisual
}

// NewCalibration opens the calibration screen.
func NewCalibration(a *App) *Calibration {
	c := &Calibration{}
	c.list = List{Visible: 3, Items: []Item{
		{Label: "Audio latency", Value: func() string { return fmt.Sprintf("%.0f ms", a.Settings.AudioOffsetMs) },
			Hint: "Strum (or hit anything) on each click you HEAR", Activate: func() { c.begin(a, calAudio) }},
		{Label: "Video latency", Value: func() string { return fmt.Sprintf("%.0f ms", a.Settings.VideoOffsetMs) },
			Hint: "Strum when each gem you SEE crosses the line", Activate: func() { c.begin(a, calVideo) }},
		{Label: "Back", Activate: func() { a.SetScene(NewSettingsAt(a, pageAudio)) }},
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
	c.vis = nil
	if m == calVideo {
		c.vis = newCalVisual(a)
	}
}

// beatTime returns the clock time of measured beat k.
func (c *Calibration) beatTime(k int) float64 { return c.start + calLead + float64(k)*calInterval }

func (c *Calibration) Update(a *App) error {
	switch c.mode {
	case calMenu:
		c.list.Update(a)
		if a.Pressed(action.MenuBack) {
			a.SetScene(NewSettingsAt(a, pageAudio))
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
			if c.vis != nil {
				c.vis.tap(k, e.Time-c.start, math.Abs(off) < 0.22)
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
		if c.mode == calVideo && c.vis != nil {
			c.drawVideo(dst, now, rel)
			break
		}
		render.Text(dst, "Strum on every click", render.Display, 72, render.ScreenW/2, 340, th.Text, render.Center)
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

// drawVideo draws the video calibration the way a song looks: the
// background, the player's highway and look, gems coming down to the
// strike line. Instructions and the count sit left of the highway. The
// current video offset is not applied, so the measurement is raw.
func (c *Calibration) drawVideo(dst *ebiten.Image, now, rel float64) {
	t := now - c.start // chart time; beat k crosses the line at calLead + k*calInterval
	render.Background(dst, t, false)
	c.vis.draw(dst, t)

	x := 120.0
	render.Text(dst, "Video latency", render.Display, 56, x, 150, th.Text, render.Left)
	render.Text(dst, "Strum as each gem", render.UI, 32, x, 240, th.TextDim, render.Left)
	render.Text(dst, "crosses the line.", render.UI, 32, x, 282, th.TextDim, render.Left)
	render.Text(dst, "Go by what you see,", render.UI, 28, x, 352, th.TextDim, render.Left)
	render.Text(dst, "not by sound.", render.UI, 28, x, 390, th.TextDim, render.Left)
	if rel < 0 {
		render.Text(dst, fmt.Sprintf("starting in %d", int(math.Ceil(-rel*calInterval))), render.UIBold, 40, x, 500, th.Text, render.Left)
	} else {
		render.Text(dst, fmt.Sprintf("%d / %d", min(calBeats, int(rel)+1), calBeats), render.UIBold, 40, x, 500, th.Text, render.Left)
	}
	if n := len(c.offsets); n > 0 {
		render.Text(dst, fmt.Sprintf("last %+.0f ms", c.offsets[n-1]), render.Cond, 32, x, 560, th.TextDim, render.Left)
	}
	Footer(dst, "Esc cancel")
}

// calVisual is the video calibration's chart: one guitar gem per measured
// beat, every one an open note (a bar across the highway), drawn by the game's own highway
// with the player's look. A tap clears the gem it was aimed at and flames
// the strike line like a hit in a song, but nothing is judged: late gems
// scroll past without turning into misses, so no red flashes pull the
// player's timing.
type calVisual struct {
	t     *chart.Track
	eng   *calEngine
	hw    render.Highway
	frame render.FrameState
}

// calOpen is the .chart lane number for an open note.
const calOpen = 7

// calChartText writes the calibration chart: 120 BPM, so one beat (192
// ticks) is calInterval, with the first gem after calLead.
func calChartText() string {
	const res = 192
	lead := int(math.Round(calLead / calInterval * res))
	var b strings.Builder
	b.WriteString("[Song]\n{\n  Resolution = 192\n  Offset = 0\n}\n")
	b.WriteString("[SyncTrack]\n{\n  0 = TS 4\n  0 = B 120000\n}\n[Events]\n{\n}\n[ExpertSingle]\n{\n")
	for k := 0; k < calBeats; k++ {
		fmt.Fprintf(&b, "  %d = N %d 0\n", lead+k*res, calOpen)
	}
	b.WriteString("}\n")
	return b.String()
}

func newCalVisual(a *App) *calVisual {
	c, err := chart.ParseDotChart(strings.NewReader(calChartText()), &chart.SongIni{})
	if err != nil {
		panic("calibration chart: " + err.Error())
	}
	v := &calVisual{t: c.Track(chart.Guitar, chart.Expert)}
	set := engine.DefaultSettings()
	set.NoFail = true
	v.eng = &calEngine{Engine: engine.New(c, v.t, set), hit: make([]bool, len(v.t.Notes))}
	hc := a.Settings.Highway
	if hc.NearWidth == 0 {
		hc = render.DefaultHighway()
	}
	v.hw.Cfg = hc
	v.frame = render.FrameState{Chart: c, Track: v.t, Eng: v.eng,
		Fx: render.ParseFxLevel(a.Settings.HitFx), Bounce: a.Settings.HighwayBounce}
	for i := range v.frame.Flash {
		v.frame.Flash[i] = -100
		v.frame.PadHit[i] = -100
	}
	v.frame.MissFlash = -100
	return v
}

// tap shows a tap at chart time t aimed at beat k. Only taps close enough
// to count clear the gem.
func (v *calVisual) tap(k int, t float64, counted bool) {
	if k < 0 || k >= len(v.eng.hit) {
		return
	}
	lane := chart.LaneOpen
	v.frame.Flash[lane] = t
	if counted && !v.eng.hit[k] {
		v.eng.hit[k] = true
		v.frame.Hits = append(v.frame.Hits, render.HitFx{At: t, Lane: lane})
	}
}

func (v *calVisual) draw(dst *ebiten.Image, t float64) {
	f := &v.frame
	f.Now = t
	keep := f.Hits[:0]
	for _, h := range f.Hits {
		if t-h.At < render.FxLife {
			keep = append(keep, h)
		}
	}
	f.Hits = keep
	v.hw.Draw(dst, f)
}

// calEngine shows tapped gems as hit and every other gem as still to
// play.
type calEngine struct {
	engine.Engine
	hit []bool
}

func (e *calEngine) Status(i int) engine.NoteStatus {
	if i >= 0 && i < len(e.hit) && e.hit[i] {
		return engine.NoteHit
	}
	return engine.Pending
}

func (e *calEngine) LaneHit(i int) uint8 {
	if e.Status(i) == engine.NoteHit {
		return e.Track().Notes[i].Lanes
	}
	return 0
}

func (e *calEngine) SPAlive(int) bool { return false }
