package render

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/action"
)

// InputViewer shows what a player is pressing: a lamp per control, how long
// it's been held, how often it's being pressed and how many times it has
// been. Times are the song time the events were fed with.
type InputViewer struct {
	rows  []inputRow
	stats [action.Count]inputStat
	drums bool
	batch Batch
}

type inputRow struct {
	act   action.Action
	label string
	color func(th *Theme) color.RGBA
}

// inputStat is one control's history.
type inputStat struct {
	down     bool
	downAt   float64 // when the press started
	lastHold float64 // length of the last finished press
	total    float64 // time held in finished presses
	presses  int
	released bool // this control sends releases (MIDI drum hits don't)
	value    float32
	recent   [16]float64 // press times, newest at recent[n%16]
	peak     float64     // highest rate so far
}

// rateWindow is how far back the press rate looks, and how long a control
// has to go quiet for it to drop to zero.
const rateWindow = 1.5

func lane(i int) func(*Theme) color.RGBA { return func(t *Theme) color.RGBA { return t.Lanes[i] } }
func drum(i int) func(*Theme) color.RGBA { return func(t *Theme) color.RGBA { return t.Drums[i] } }
func textColor(t *Theme) color.RGBA      { return t.Text }
func spColor(t *Theme) color.RGBA        { return t.StarPower }
func drumOrange(t *Theme) color.RGBA     { return t.DrumOrange }

// NewInputViewer makes a viewer for an instrument: the frets, strum, star
// power and whammy, or the pads, cymbals and kick. fiveLane adds the orange
// pad of 5-lane kits.
func NewInputViewer(drums, fiveLane bool) *InputViewer {
	v := &InputViewer{drums: drums}
	if !drums {
		v.rows = []inputRow{
			{action.Green, "G", lane(0)}, {action.Red, "R", lane(1)}, {action.Yellow, "Y", lane(2)},
			{action.Blue, "B", lane(3)}, {action.Orange, "O", lane(4)},
			{action.StrumUp, "Up", textColor}, {action.StrumDown, "Dn", textColor},
			{action.StarPower, "SP", spColor}, {action.Whammy, "Wh", spColor},
		}
		return v
	}
	v.rows = []inputRow{
		{action.Kick, "K", drum(0)}, {action.DrumRed, "R", drum(1)}, {action.DrumYellow, "Y", drum(2)},
		{action.DrumBlue, "B", drum(3)}, {action.DrumGreen, "G", drum(4)},
	}
	if fiveLane {
		v.rows = append(v.rows, inputRow{action.DrumOrange, "O", drumOrange})
	}
	v.rows = append(v.rows,
		inputRow{action.CymYellow, "CY", drum(2)}, inputRow{action.CymBlue, "CB", drum(3)}, inputRow{action.CymGreen, "CG", drum(4)},
		inputRow{action.StarPower, "SP", spColor})
	return v
}

// Feed records an input event (its Time is song time).
func (v *InputViewer) Feed(ev action.Event) {
	if v == nil || ev.Action >= action.Count {
		return
	}
	s := &v.stats[ev.Action]
	s.value = ev.Value
	if !ev.Down {
		if s.down {
			s.lastHold = ev.Time - s.downAt
			s.total += s.lastHold
		}
		s.down, s.released = false, true
		return
	}
	if s.down && !s.isHit(v.drums) {
		return // still held: the whammy moving
	}
	s.down, s.downAt = true, ev.Time
	s.recent[s.presses%len(s.recent)] = ev.Time
	s.presses++
	s.peak = math.Max(s.peak, s.rate(ev.Time))
}

// rate is presses per second over the last rateWindow seconds.
func (s *inputStat) rate(now float64) float64 {
	n := min(s.presses, len(s.recent))
	first, last, count := math.Inf(1), math.Inf(-1), 0
	for _, t := range s.recent[:n] {
		if t > now-rateWindow && t <= now {
			first, last = math.Min(first, t), math.Max(last, t)
			count++
		}
	}
	if count < 2 || last <= first {
		return 0
	}
	return float64(count-1) / (last - first)
}

// isHit reports whether presses are instant hits: drum pads that have never
// sent a release (MIDI kits don't, a computer keyboard does).
func (s *inputStat) isHit(drums bool) bool { return drums && !s.released }

// lit reports whether the control shows as pressed. Hits flash briefly.
func (s *inputStat) lit(now float64, drums bool) bool {
	return s.down && (!s.isHit(drums) || now-s.downAt < 0.12)
}

// hold is the current press's length, or the last one's.
func (s *inputStat) hold(now float64, drums bool) float64 {
	if s.down && !s.isHit(drums) {
		return now - s.downAt
	}
	return s.lastHold
}

// held is the whole time the control has been held.
func (s *inputStat) held(now float64, drums bool) float64 {
	if s.down && !s.isHit(drums) {
		return s.total + now - s.downAt
	}
	return s.total
}

// Stats is a control's numbers at a song time, for tests and other screens.
func (v *InputViewer) Stats(a action.Action, now float64) (presses int, hold, total, rate, peak float64) {
	s := &v.stats[a]
	return s.presses, s.hold(now, v.drums), s.held(now, v.drums), s.rate(now), s.peak
}

const (
	inputRowH  = 30.0
	inputHeadH = 28.0
	inputPad   = 10.0
)

// width is the panel's width at scale 1. Compact panels drop the total and
// count.
func (v *InputViewer) width(compact bool) float64 {
	if compact {
		return 176
	}
	return 332
}

// Size is the panel's size at scale k.
func (v *InputViewer) Size(k float64, compact bool) (w, h float64) {
	return v.width(compact) * k, (inputHeadH + float64(len(v.rows))*inputRowH + 2*inputPad) * k
}

// Draw draws the panel with its top left at x, y. now is song time.
func (v *InputViewer) Draw(dst *ebiten.Image, x, y, k float64, compact bool, now float64) {
	if v == nil {
		return
	}
	th := &DefaultTheme
	w, h := v.Size(k, compact)
	RoundRect(dst, x, y, w, h, 12*k, WithAlpha(th.Panel, 0.72))
	size := 21 * k
	head := Cond
	right := func(s string, cx, ry float64, c color.RGBA, f Font) {
		Text(dst, s, f, size, x+cx*k, ry, c, Right)
	}
	type col struct {
		x    float64
		name string
	}
	cols := []col{{112, "HOLD"}, {170, "/SEC"}}
	if !compact {
		cols = []col{{112, "HOLD"}, {176, "TOTAL"}, {252, "/SEC"}, {322, "HITS"}}
		if !v.drums {
			cols[3].name = "COUNT"
		}
	}
	top := y + inputPad*k
	for _, c := range cols {
		right(c.name, c.x, top-2*k, WithAlpha(th.TextDim, 0.85), head)
	}
	b := &v.batch
	for i, r := range v.rows {
		s := &v.stats[r.act]
		ry := top + (inputHeadH+float64(i)*inputRowH)*k
		c := r.color(th)
		lit := s.lit(now, v.drums)
		fill := Darken(c, 0.72)
		if lit {
			fill = Lighten(c, 0.3)
		}
		cx, cy := x+(inputPad+10)*k, ry+inputRowH*0.5*k
		b.Ellipse(dst, cx, cy, 10*k, 10*k, fill, fill)
		b.Flush(dst)
		if r.act == action.Whammy {
			// how far the bar is pushed, under the numbers
			bw := (w/k - 44 - inputPad) * k
			FillRect(dst, x+44*k, ry+inputRowH*k-5*k, bw, 3*k, WithAlpha(th.TextDim, 0.25))
			FillRect(dst, x+44*k, ry+inputRowH*k-5*k, bw*float64(s.value), 3*k, c)
		}
		lc := th.TextDim
		if lit {
			lc = th.Text
		}
		ty := ry + 2*k
		Text(dst, r.label, CondBold, size, x+(inputPad+25)*k, ty, lc, Left)
		tc := WithAlpha(th.Text, 0.9)
		hold := "-"
		if s.isHit(v.drums) {
			hold = ""
		} else if hd := s.hold(now, v.drums); hd > 0 {
			hold = fmtSec(hd)
		}
		rate := "-"
		if rt := s.rate(now); rt > 0 && now-s.lastPress() < rateWindow {
			rate = fmt.Sprintf("%.1f", rt)
		}
		right(hold, cols[0].x, ty, tc, Cond)
		if compact {
			right(rate, cols[1].x, ty, tc, Cond)
			continue
		}
		total := ""
		if !s.isHit(v.drums) {
			total = fmtSec(s.held(now, v.drums))
		}
		right(total, cols[1].x, ty, WithAlpha(th.TextDim, 0.95), Cond)
		right(rate, cols[2].x, ty, tc, Cond)
		right(fmt.Sprint(s.presses), cols[3].x, ty, WithAlpha(th.TextDim, 0.95), Cond)
	}
}

// lastPress is the newest press's time.
func (s *inputStat) lastPress() float64 {
	if s.presses == 0 {
		return math.Inf(-1)
	}
	return s.recent[(s.presses-1)%len(s.recent)]
}

// fmtSec shows a length of time: hundredths under a minute, then minutes.
func fmtSec(t float64) string {
	if t < 60 {
		return fmt.Sprintf("%.2f", t)
	}
	return fmt.Sprintf("%d:%02d", int(t)/60, int(t)%60)
}
