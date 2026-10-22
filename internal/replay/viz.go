package replay

import (
	"math"
	"strings"
	"time"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/engine"
	"github.com/mxygem/jam/internal/rich"
)

// Gem states in a RunView.
const (
	GemUnreached uint8 = iota // the run ended (or the player joined) without reaching it
	GemHit
	GemMissed
)

// LaneRow is one row of the chart strip and the fret/pad heat map: a guitar
// fret (open notes get their own row) or a drum pad (pro drums split toms
// and cymbals).
type LaneRow struct {
	Name string `json:"name"`
	// Color is a theme color key: green, red, yellow, blue, orange, open,
	// kick.
	Color  string `json:"color"`
	Cymbal bool   `json:"cymbal,omitempty"`
}

// GemMark is one note of the chart (guitar) or one pad hit (drums) and what
// the run did with it.
type GemMark struct {
	Note  int     `json:"n"`  // index in the track the run was played against
	Rows  []int   `json:"r"`  // rows it is drawn on (a chord spans several)
	Time  float64 `json:"t"`  // seconds
	End   float64 `json:"e"`  // sustain end, 0 for none
	State uint8   `json:"s"`  // GemUnreached, GemHit, GemMissed
	OffMs float64 `json:"o"`  // hits: early (-) or late (+)
	Kind  string  `json:"k"`  // strum, hopo, tap, open, chord strum... or the drum pad
	SP    bool    `json:"sp"` // part of a star power phrase
	// Misses: why, and the category (see MissCategories).
	Reason string `json:"why,omitempty"`
	Cat    string `json:"cat,omitempty"`
}

// ErrorMark is an overstrum (guitar) or an extra pad hit (drums).
type ErrorMark struct {
	Time  float64 `json:"t"`
	Row   int     `json:"r"`              // drums: pad row; guitar: -1
	Held  string  `json:"held,omitempty"` // guitar: frets held
	Broke bool    `json:"broke"`          // it reset the streak
}

// StreakPoint is the streak right after a judgment.
type StreakPoint struct {
	Time   float64 `json:"t"`
	Streak int     `json:"s"`
}

// Span is a stretch of song time.
type Span struct {
	Start float64 `json:"a"`
	End   float64 `json:"b"`
}

// SectionCard is a practice section's report card.
type SectionCard struct {
	SectionStat
	LongestStreak int            `json:"longest_streak"`
	MissBy        map[string]int `json:"miss_by"` // misses per category
	Early, Late   int            // hits outside ±OnTimeMs
	SpreadMs      float64        // standard deviation of the hit offsets
}

// Cell is one fret (or pad) in one section.
type Cell struct {
	Total  int `json:"n"`
	Hits   int `json:"h"`
	Misses int `json:"m"`
}

// MissRate is misses over gems reached, or -1 when none were reached.
func (c Cell) MissRate() float64 {
	if c.Hits+c.Misses == 0 {
		return -1
	}
	return float64(c.Misses) / float64(c.Hits+c.Misses)
}

// RunView is everything the run visuals draw: per-gem outcomes, mistakes,
// the streak over time, and per-section and per-fret summaries.
type RunView struct {
	Song       string    `json:"song"`
	Artist     string    `json:"artist"`
	Instrument string    `json:"instrument"`
	Difficulty string    `json:"difficulty"`
	Player     string    `json:"player"`
	Started    time.Time `json:"started"`
	Drums      bool      `json:"drums"`
	Completed  bool      `json:"completed"`
	EndTime    float64   `json:"end_time"` // where the run stopped
	Length     float64   `json:"length"`   // end of the last note
	EarlyMs    float64   `json:"early_ms"` // hit window
	LateMs     float64   `json:"late_ms"`
	Summary    Summary   `json:"summary"`
	MeanMs     float64   `json:"mean_ms"`
	StdDevMs   float64   `json:"spread_ms"`

	Rows     []LaneRow     `json:"rows"`
	Gems     []GemMark     `json:"gems"`
	Errors   []ErrorMark   `json:"errors"`
	Streak   []StreakPoint `json:"streak"`
	SPOn     []float64     `json:"sp_on"`    // star power activations
	Revives  []float64     `json:"revives"`  // band: brought back by a bandmate
	SPSpans  []Span        `json:"sp"`       // star power phrases in the chart
	Solos    []Span        `json:"solos"`    // solo sections in the chart
	Sections []SectionCard `json:"sections"` // every section, chart order
	Grid     [][]Cell      `json:"grid"`     // [row][section]
	Over     []int         `json:"over"`     // overstrums / overhits per section
	Tips     []string      `json:"tips"`
}

// rowsFor lays out the lanes of a track, top to bottom, and maps each gem
// of a note to its row.
type rowLayout struct {
	rows  []LaneRow
	drums bool
	mode  chart.DrumMode
	index map[string]int
}

func newRowLayout(t *chart.Track) *rowLayout {
	l := &rowLayout{drums: t.Key.Inst.IsDrums(), mode: t.DrumMode, index: map[string]int{}}
	if !l.drums {
		for i, n := range []string{"green", "red", "yellow", "blue", "orange"} {
			l.add(LaneRow{Name: n, Color: n}, string(rune('0'+i)))
		}
		for i := range t.Notes {
			if t.Notes[i].IsOpen() {
				l.add(LaneRow{Name: "open", Color: "open"}, "open")
				break
			}
		}
		return l
	}
	// Drums: one row per pad (and per cymbal/tom on pro drums) the track
	// uses, in kit order.
	seen := map[string]bool{}
	for i := range t.Notes {
		n := &t.Notes[i]
		for lane := 0; lane < chart.NumLanes; lane++ {
			if n.Lanes&(1<<lane) != 0 {
				seen[drumLaneName(l.mode, lane, n.Cymbals&(1<<lane) != 0)] = true
			}
		}
	}
	order := []struct{ name, color string }{
		{"yellow cymbal", "yellow"}, {"blue cymbal", "blue"}, {"green cymbal", "green"},
		{"red", "red"}, {"yellow", "yellow"}, {"yellow tom", "yellow"}, {"blue", "blue"}, {"blue tom", "blue"},
		{"orange", "orange"}, {"green", "green"}, {"green tom", "green"}, {"kick", "kick"},
	}
	for _, o := range order {
		if seen[o.name] {
			l.add(LaneRow{Name: o.name, Color: o.color, Cymbal: strings.HasSuffix(o.name, "cymbal")}, o.name)
		}
	}
	return l
}

func (l *rowLayout) add(r LaneRow, key string) {
	l.index[key] = len(l.rows)
	l.rows = append(l.rows, r)
}

// guitarRows are the rows of a guitar note's frets.
func (l *rowLayout) guitarRows(n *chart.Note) []int {
	if n.IsOpen() && n.Lanes&chart.FretMask == 0 {
		return []int{l.index["open"]}
	}
	var out []int
	for i := 0; i < 5; i++ {
		if n.Lanes&(1<<i) != 0 {
			out = append(out, i)
		}
	}
	return out
}

func (l *rowLayout) drumRow(n *chart.Note, lane int) int {
	cym := n != nil && n.Cymbals&(1<<lane) != 0
	if r, ok := l.index[drumLaneName(l.mode, lane, cym)]; ok {
		return r
	}
	return -1
}

func noteKind(n *chart.Note) string {
	switch {
	case n.IsOpen():
		return "open"
	case n.IsChord():
		return "chord " + n.Kind.String()
	}
	return n.Kind.String()
}

// BuildRunView lays out a recording for the visuals. t is the part the run
// was played against (see TrackFor); an may be nil (it is built then).
func BuildRunView(rec *Recording, c *chart.Chart, t *chart.Track, an *Analysis) *RunView {
	if an == nil {
		an = Analyze(rec, c, t)
	}
	l := newRowLayout(t)
	v := &RunView{
		Song: rich.Plain(rec.Song.Name), Artist: rich.Plain(rec.Song.Artist),
		Instrument: rec.Instrument, Difficulty: rec.Difficulty, Player: rec.Player,
		Started: rec.Started, Drums: l.drums, Completed: rec.Completed, EndTime: rec.EndTime,
		Summary: rec.Result, MeanMs: an.MeanMs, StdDevMs: an.StdDevMs, Rows: l.rows,
		Revives: rec.Revives, Tips: an.Tips,
	}
	early, late := rec.Settings.Windows(l.drums)
	v.EarlyMs, v.LateMs = early*1000, late*1000
	if v.EarlyMs <= 0 {
		v.EarlyMs = 70
	}
	if v.LateMs <= 0 {
		v.LateMs = 70
	}
	for i := range t.Notes {
		if e := t.Notes[i].SustainEnd(); e > v.Length {
			v.Length = e
		}
	}
	if rec.EndTime > v.Length && !rec.Completed {
		v.Length = rec.EndTime
	}
	for _, p := range t.StarPower {
		v.SPSpans = append(v.SPSpans, Span{p.Start, p.End})
	}
	for _, p := range t.Solos {
		v.Solos = append(v.Solos, Span{p.Start, p.End})
	}

	// Sections: the analysis' stats plus the extras.
	for _, s := range an.Sections {
		card := SectionCard{SectionStat: s, MissBy: map[string]int{}}
		if math.IsInf(card.Start, -1) {
			card.Start = 0
		}
		if math.IsInf(card.End, 1) {
			card.End = math.Max(v.Length, card.Start)
		}
		v.Sections = append(v.Sections, card)
	}
	secIdx := func(tm float64) int {
		for i := len(v.Sections) - 1; i >= 0; i-- {
			if tm >= v.Sections[i].Start {
				return i
			}
		}
		return 0
	}
	v.Grid = make([][]Cell, len(l.rows))
	for i := range v.Grid {
		v.Grid[i] = make([]Cell, len(v.Sections))
	}
	v.Over = make([]int, len(v.Sections))

	// One gem per guitar note, one per pad hit on drums.
	type key struct{ note, lane int }
	gemAt := map[key]int{}
	for i := range t.Notes {
		n := &t.Notes[i]
		g := GemMark{Note: i, Time: n.Time, SP: n.StarPower}
		if n.HasSustain() {
			g.End = n.SustainEnd()
		}
		si := secIdx(n.Time)
		if !l.drums {
			g.Rows = l.guitarRows(n)
			g.Kind = noteKind(n)
			gemAt[key{i, -1}] = len(v.Gems)
			v.Gems = append(v.Gems, g)
			for _, r := range g.Rows {
				v.Grid[r][si].Total++
			}
			continue
		}
		for lane := 0; lane < chart.NumLanes; lane++ {
			if n.Lanes&(1<<lane) == 0 {
				continue
			}
			if lane == chart.DrumKick && n.DoubleKick && !rec.Settings.ExpertPlus {
				continue
			}
			r := l.drumRow(n, lane)
			if r < 0 {
				continue
			}
			dg := g
			dg.Rows = []int{r}
			dg.Kind = l.rows[r].Name
			gemAt[key{i, lane}] = len(v.Gems)
			v.Gems = append(v.Gems, dg)
			v.Grid[r][si].Total++
		}
	}
	misses := map[int]MissDetail{} // guitar, by note
	if !l.drums {
		for _, m := range an.Misses {
			misses[m.Note] = m
		}
	}
	missFor := func(note, lane int) (MissDetail, bool) {
		if !l.drums {
			m, ok := misses[note]
			return m, ok
		}
		// Drum misses are per pad; MissDetail names the pad.
		for _, m := range an.Misses {
			if m.Note == note && m.Lanes == drumLaneName(l.mode, lane, t.Notes[note].Cymbals&(1<<lane) != 0) {
				return m, true
			}
		}
		return MissDetail{}, false
	}

	sectionRun := make([]int, len(v.Sections))
	curSec, run := -1, 0
	for _, j := range rec.Judgments {
		lane := -1
		if l.drums {
			lane = j.Lane
		}
		switch j.Result {
		case engine.Hit, engine.Miss:
			gi, ok := gemAt[key{j.Note, lane}]
			if !ok {
				break
			}
			g := &v.Gems[gi]
			si := secIdx(g.Time)
			if j.Result == engine.Hit {
				g.State = GemHit
				g.OffMs = math.Round(j.Offset*1e5) / 100
				for _, r := range g.Rows {
					v.Grid[r][si].Hits++
				}
				c := &v.Sections[si]
				switch {
				case g.OffMs < -OnTimeMs:
					c.Early++
				case g.OffMs > OnTimeMs:
					c.Late++
				}
				c.SpreadMs += (g.OffMs - c.MeanMs) * (g.OffMs - c.MeanMs)
			} else {
				g.State = GemMissed
				if m, ok := missFor(j.Note, j.Lane); ok {
					g.Reason, g.Cat = m.Reason, m.Category
					v.Sections[si].MissBy[m.Category]++
				}
				for _, r := range g.Rows {
					v.Grid[r][si].Misses++
				}
			}
		case engine.Overstrum, engine.Overhit:
			e := ErrorMark{Time: j.Time, Row: -1, Broke: j.Streak == 0}
			if j.Result == engine.Overhit {
				e.Row = l.drumRow(nil, j.Lane)
				if e.Row < 0 { // a cymbal or tom of that pad
					for name, r := range l.index {
						if drumLaneName(l.mode, j.Lane, true) == name || drumLaneName(l.mode, j.Lane, false) == name {
							e.Row = r
							break
						}
					}
				}
			} else {
				e.Held = chart.FretString(j.Held)
			}
			v.Errors = append(v.Errors, e)
			v.Over[secIdx(j.Time)]++
		case engine.SPActivate:
			v.SPOn = append(v.SPOn, j.Time)
		}
		switch j.Result {
		case engine.Hit, engine.Miss, engine.Overstrum, engine.Overhit:
			v.Streak = append(v.Streak, StreakPoint{j.Time, j.Streak})
			si := secIdx(j.Time)
			if si != curSec {
				curSec, run = si, 0
			}
			switch {
			case j.Result == engine.Hit:
				run++
			case j.Streak == 0:
				run = 0
			}
			sectionRun[si] = max(sectionRun[si], run)
		}
	}
	for i := range v.Sections {
		c := &v.Sections[i]
		c.LongestStreak = sectionRun[i]
		if c.Hits > 1 {
			c.SpreadMs = math.Sqrt(c.SpreadMs / float64(c.Hits))
		} else {
			c.SpreadMs = 0
		}
	}
	return v
}
