package app

import (
	"fmt"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/engine"
	"github.com/mxygem/jam/internal/render"
)

// lookPreview plays a short built-in chart on a miniature screen so the
// Appearance settings can be judged as they change: every kind of note,
// sustains, star power, hit effects, the HUD and the background.

const (
	pvRes      = 192
	pvBlock    = 2 * 4 * pvRes // two bars of 4/4
	pvBlocks   = 30
	pvLeadIn   = 4 * pvRes // first notes a bar in (2 s)
	pvBPM      = 120.0
	pvBlockSec = 4.0 // two bars at 120 BPM
)

// previewChartText writes the preview chart: Expert guitar and drums,
// the same two bars over and over. Every other block's notes are a star
// power phrase.
func previewChartText() string {
	var b strings.Builder
	b.WriteString("[Song]\n{\n  Resolution = 192\n  Offset = 0\n}\n")
	b.WriteString("[SyncTrack]\n{\n  0 = TS 4\n  0 = B 120000\n}\n[Events]\n{\n}\n")

	type gn struct{ tick, lane, length int }
	guitar := []gn{
		{0, 0, 0}, {96, 1, 0}, {192, 2, 0}, {240, 3, 0}, {288, 4, 0}, // strums, then HOPOs
		{384, 0, 192}, {384, 2, 192}, // sustained chord
		{576, 7, 0}, {672, 7, 0}, // open notes
		{768, 1, 0}, {816, 2, 0}, {864, 3, 0}, {912, 4, 0}, // taps
		{960, 3, 0}, {960, 4, 0}, // chord
		{1152, 0, 288}, // sustain
		{1440, 2, 0}, {1488, 1, 0},
	}
	taps := map[int]bool{768: true, 816: true, 864: true, 912: true}
	b.WriteString("[ExpertSingle]\n{\n")
	for blk := 0; blk < pvBlocks; blk++ {
		base := pvLeadIn + blk*pvBlock
		if blk%2 == 1 {
			fmt.Fprintf(&b, "  %d = S 2 %d\n", base, pvBlock-1)
		}
		for _, n := range guitar {
			fmt.Fprintf(&b, "  %d = N %d %d\n", base+n.tick, n.lane, n.length)
			if taps[n.tick] {
				fmt.Fprintf(&b, "  %d = N 6 0\n", base+n.tick)
			}
		}
	}
	b.WriteString("}\n")

	type dn struct {
		tick, lane int
		cym        bool
	}
	var drums []dn
	for i := 0; i < 8; i++ { // hi-hat eighths through the first bar
		drums = append(drums, dn{i * 96, 2, true})
	}
	for _, t := range []int{0, 384, 768, 1056, 1152} {
		drums = append(drums, dn{t, 0, false})
	}
	drums = append(drums, dn{720, 32, false}) // Expert+ kick
	for _, t := range []int{192, 576, 960, 1344} {
		drums = append(drums, dn{t, 1, false})
	}
	drums = append(drums, dn{768, 4, true}, dn{1152, 3, true}) // crash, ride
	for i, l := range []int{2, 2, 3, 3, 4, 4} {                // tom fill
		drums = append(drums, dn{1440 + i*32, l, false})
	}
	b.WriteString("[ExpertDrums]\n{\n")
	for blk := 0; blk < pvBlocks; blk++ {
		base := pvLeadIn + blk*pvBlock
		if blk%2 == 1 {
			fmt.Fprintf(&b, "  %d = S 2 %d\n", base, pvBlock-1)
		}
		for _, n := range drums {
			fmt.Fprintf(&b, "  %d = N %d 0\n", base+n.tick, n.lane)
			if n.cym {
				fmt.Fprintf(&b, "  %d = N %d 0\n", base+n.tick, 64+n.lane)
			}
		}
	}
	b.WriteString("}\n")
	return b.String()
}

var previewChart *chart.Chart

func loadPreviewChart() *chart.Chart {
	if previewChart == nil {
		c, err := chart.ParseDotChart(strings.NewReader(previewChartText()), &chart.SongIni{})
		if err != nil {
			panic("preview chart: " + err.Error())
		}
		previewChart = c
	}
	return previewChart
}

type lookPreview struct {
	drums bool
	c     *chart.Chart
	t     *chart.Track
	eng   engine.Engine
	bot   []action.Event
	botI  int
	jI    int
	held  uint8
	start float64
	last  float64
	end   float64

	hw    render.Highway
	hud   render.HUD
	frame render.FrameState
	hs    render.HUDState
	off   *ebiten.Image
}

func newLookPreview(drums bool) *lookPreview {
	p := &lookPreview{drums: drums, c: loadPreviewChart()}
	inst := chart.Guitar
	if drums {
		inst = chart.Drums
	}
	p.t = p.c.Track(inst, chart.Expert)
	p.end = p.c.LastTime() + 2
	p.restart(clock.Now())
	return p
}

func (p *lookPreview) restart(now float64) {
	set := engine.DefaultSettings()
	set.NoFail = true
	set.ExpertPlus = true
	p.eng = engine.New(p.c, p.t, set)
	p.bot = botEvents(p.t, true)
	p.botI, p.jI, p.held = 0, 0, 0
	p.start = now
	p.last = 0
	p.frame = render.FrameState{Chart: p.c, Track: p.t, Eng: p.eng, ExpertPlus: true}
	for i := range p.frame.Flash {
		p.frame.Flash[i] = -100
		p.frame.PadHit[i] = -100
	}
	p.frame.MissFlash = -100
	p.hs = render.HUDState{
		Title: "Preview", Artist: "jam", ShowTiming: true, BaseMax: 4, NoFail: true,
		Instrument: p.t.Key.Inst.Title(), Difficulty: "Expert", LastHitAt: -100, SectionAt: -100, Length: p.end,
	}
}

// update runs the bot up to now.
func (p *lookPreview) update(a *App, now float64) {
	t := now - p.start
	if t > p.end {
		p.restart(now)
		t = 0
	}
	for p.botI < len(p.bot) && p.bot[p.botI].Time <= t {
		ev := p.bot[p.botI]
		p.botI++
		if l, ok := ev.Action.FretLane(); ok {
			if ev.Down {
				p.held |= 1 << l
			} else {
				p.held &^= 1 << l
			}
		}
		p.eng.Process(ev)
		if p.drums && ev.Down {
			if l := drumLaneOf(p.t.DrumMode, ev.Action); l >= 0 {
				p.frame.PadHit[l] = ev.Time
			}
		}
	}
	p.eng.Advance(t - engineLag)
	js := p.eng.Judgments()
	st := p.eng.Stats()
	for ; p.jI < len(js); p.jI++ {
		j := js[p.jI]
		if j.Result != engine.Hit {
			continue
		}
		sp := p.spShown(j.Time)
		if p.drums {
			p.frame.Flash[j.Lane] = j.Time
			p.frame.Hits = append(p.frame.Hits, render.HitFx{At: j.Time, Lane: j.Lane, SP: sp})
		} else if j.Note >= 0 {
			n := &p.t.Notes[j.Note]
			for l := 0; l < chart.NumLanes; l++ {
				if n.Lanes&(1<<l) != 0 {
					p.frame.Flash[l] = j.Time
					p.frame.Hits = append(p.frame.Hits, render.HitFx{At: j.Time, Lane: l, SP: sp})
				}
			}
		}
		// Alternate early and late so both timing colors show.
		off := 18.0
		if p.jI%2 == 0 {
			off = -14
		}
		p.hs.LastOffset, p.hs.LastHitAt = off, j.Time
	}
	keep := p.frame.Hits[:0]
	for _, h := range p.frame.Hits {
		if t-h.At < render.FxLife {
			keep = append(keep, h)
		}
	}
	p.frame.Hits = keep
	p.last = t
	set := a.Settings
	p.hw.Cfg = set.Highway
	p.frame.Now = t
	p.frame.Held = p.held
	p.frame.Fx = render.ParseFxLevel(set.HitFx)
	p.frame.Bounce = set.HighwayBounce
	p.frame.SPActive = p.spShown(t)
	h := &p.hs
	h.Now = t
	h.Score = st.Score
	h.Streak = st.Streak
	h.Mult = p.eng.Multiplier()
	if p.frame.SPActive {
		h.Mult *= 2
	}
	h.SPActive = p.frame.SPActive
	h.SPEnergy = 0.75
	h.Rock = 0.8
	h.SongTime = t
	h.Progress = t / p.end
	h.TitleAlpha = 1
	h.Hits, h.Total = st.NotesHit, st.NotesHit+st.NotesMissed
}

// spShown reports whether the preview shows star power as active at t:
// for the second half of every fourth block.
func (p *lookPreview) spShown(t float64) bool {
	blk := (t - float64(pvLeadIn)/pvRes*60/pvBPM) / pvBlockSec
	if blk < 0 {
		return false
	}
	return int(blk)%4 == 2 || int(blk)%4 == 3 && blk-float64(int(blk)) < 0.5
}

// draw shows the preview in the box x, y, w (height follows the screen
// shape).
func (p *lookPreview) draw(dst *ebiten.Image, x, y, w float64) float64 {
	if p.off == nil {
		p.off = ebiten.NewImage(render.ScreenW, render.ScreenH)
	}
	p.off.Clear()
	render.Offscreen(func() {
		render.Background(p.off, p.frame.Now, p.frame.SPActive)
		p.hw.Draw(p.off, &p.frame)
		p.hud.Highway = &p.hw
		p.hud.Draw(p.off, &p.hs)
	})
	k := w / render.ScreenW
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(k, k)
	op.GeoM.Translate(x, y)
	dst.DrawImage(p.off, op)
	return render.ScreenH * k
}
