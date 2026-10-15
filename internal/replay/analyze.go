package replay

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/engine"
)

// SectionStat summarises one practice section.
type SectionStat struct {
	Name       string
	Start, End float64
	Total      int
	Hits       int
	Misses     int
	Overstrums int
	MeanMs     float64
}

// Judged is how many of the section's notes were reached (hit or missed).
func (s SectionStat) Judged() int { return s.Hits + s.Misses }

// Accuracy is hits / notes reached in the section.
func (s SectionStat) Accuracy() float64 {
	if s.Judged() == 0 {
		return 1
	}
	return float64(s.Hits) / float64(s.Judged())
}

// KindStat summarises hits by note type (guitar) or lane (drums).
type KindStat struct {
	Kind   string
	Total  int
	Hits   int
	MeanMs float64
}

// MissDetail explains one miss.
type MissDetail struct {
	Note    int
	Time    float64
	Section string
	Lanes   string
	Kind    string
	Reason  string
}

// StreakBreak is a moment the streak reset.
type StreakBreak struct {
	Time    float64
	Lost    int
	Cause   string
	Section string
}

// Analysis is the post-play breakdown of a recording.
type Analysis struct {
	Rec      *Recording
	Drums    bool
	Hits     int
	Offsets  []float64 // ms, hits only, in note order
	MeanMs   float64
	MedianMs float64
	StdDevMs float64
	Early    int // hits more than OnTimeMs early
	Late     int
	OnTime   int

	HistStartMs float64
	BucketMs    float64
	Hist        []int

	Sections     []SectionStat
	Kinds        []KindStat
	Misses       []MissDetail
	Breaks       []StreakBreak
	SustainTotal int
	SustainDone  int
	SustainAvg   float64 // average fraction held of dropped+completed
	FretLeadMs   float64 // guitar: median time frets landed before the strum
	Tips         []string
}

// OnTimeMs is the band considered "on time" for the early/late split.
const OnTimeMs = 15.0

// Analyze builds the breakdown for rec played against t.
func Analyze(rec *Recording, c *chart.Chart, t *chart.Track) *Analysis {
	a := &Analysis{Rec: rec, Drums: t.Key.Inst.IsDrums(), BucketMs: 5}
	notes := t.Notes
	early, late := rec.Settings.Windows(a.Drums)
	early, late = early*1000, late*1000
	if early <= 0 {
		early = 70
	}
	if late <= 0 {
		late = 70
	}
	a.HistStartMs = -math.Ceil(early/a.BucketMs) * a.BucketMs
	nb := int(math.Ceil((late-a.HistStartMs)/a.BucketMs)) + 1
	a.Hist = make([]int, nb)

	// Sections
	secs := c.Sections
	if len(secs) == 0 {
		secs = []chart.Section{{Name: "Song", Time: math.Inf(-1)}}
	}
	for i, s := range secs {
		end := math.Inf(1)
		if i+1 < len(secs) {
			end = secs[i+1].Time
		}
		a.Sections = append(a.Sections, SectionStat{Name: s.Name, Start: s.Time, End: end})
	}
	secIdx := func(tm float64) int {
		for i := len(a.Sections) - 1; i >= 0; i-- {
			if tm >= a.Sections[i].Start {
				return i
			}
		}
		return 0
	}
	secOffSum := make([]float64, len(a.Sections))

	kinds := map[string]*KindStat{}
	kindOrder := []string{}
	kindOf := func(n *chart.Note, lane int) string {
		if a.Drums {
			return drumLaneName(t.DrumMode, lane, n.Cymbals&(1<<lane) != 0)
		}
		switch {
		case n.IsOpen():
			return "open"
		case n.IsChord():
			return "chord " + n.Kind.String()
		}
		return n.Kind.String()
	}
	addKind := func(k string, hit bool, off float64) {
		ks, ok := kinds[k]
		if !ok {
			ks = &KindStat{Kind: k}
			kinds[k] = ks
			kindOrder = append(kindOrder, k)
		}
		ks.Total++
		if hit {
			ks.Hits++
			ks.MeanMs += off
		}
	}

	// Totals per section from the chart itself.
	for i := range notes {
		n := &notes[i]
		cnt := 1
		if a.Drums {
			cnt = n.Count()
			if n.DoubleKick && !rec.Settings.ExpertPlus {
				cnt--
			}
		}
		a.Sections[secIdx(n.Time)].Total += cnt
	}

	prevStreak := 0
	sustainFrac := 0.0
	sustainCount := 0
	for _, j := range rec.Judgments {
		switch j.Result {
		case engine.Hit:
			off := math.Round(j.Offset*1e6) / 1e3 // ms, rounded to the microsecond
			a.Hits++
			a.Offsets = append(a.Offsets, off)
			b := int(math.Floor((off - a.HistStartMs) / a.BucketMs))
			if b < 0 {
				b = 0
			}
			if b >= len(a.Hist) {
				b = len(a.Hist) - 1
			}
			a.Hist[b]++
			switch {
			case off < -OnTimeMs:
				a.Early++
			case off > OnTimeMs:
				a.Late++
			default:
				a.OnTime++
			}
			if j.Note >= 0 && j.Note < len(notes) {
				n := &notes[j.Note]
				si := secIdx(n.Time)
				a.Sections[si].Hits++
				secOffSum[si] += off
				addKind(kindOf(n, j.Lane), true, off)
			}
		case engine.Miss:
			if j.Note >= 0 && j.Note < len(notes) {
				n := &notes[j.Note]
				si := secIdx(n.Time)
				a.Sections[si].Misses++
				addKind(kindOf(n, j.Lane), false, 0)
				lanes := n.LaneString()
				if a.Drums {
					lanes = drumLaneName(t.DrumMode, j.Lane, n.Cymbals&(1<<j.Lane) != 0)
				}
				a.Misses = append(a.Misses, MissDetail{
					Note: j.Note, Time: n.Time, Section: a.Sections[si].Name, Lanes: lanes,
					Kind: kindOf(n, j.Lane), Reason: a.missReason(rec, t, j),
				})
			}
		case engine.Overstrum, engine.Overhit:
			a.Sections[secIdx(j.Time)].Overstrums++
		case engine.SustainDone:
			a.SustainTotal++
			a.SustainDone++
			sustainFrac += 1
			sustainCount++
		case engine.SustainDrop:
			a.SustainTotal++
			sustainFrac += j.Value
			sustainCount++
		}
		if (j.Result == engine.Miss || j.Result == engine.Overstrum || (j.Result == engine.Overhit && j.Streak == 0)) && prevStreak >= 5 && j.Streak == 0 {
			cause := j.Result.String()
			a.Breaks = append(a.Breaks, StreakBreak{Time: j.Time, Lost: prevStreak, Cause: cause, Section: a.Sections[secIdx(j.Time)].Name})
		}
		if j.Result == engine.Hit || j.Result == engine.Miss || j.Result == engine.Overstrum || j.Result == engine.Overhit {
			prevStreak = j.Streak
		}
	}
	if sustainCount > 0 {
		a.SustainAvg = sustainFrac / float64(sustainCount)
	}
	for i := range a.Sections {
		if a.Sections[i].Hits > 0 {
			a.Sections[i].MeanMs = secOffSum[i] / float64(a.Sections[i].Hits)
		}
	}
	for _, k := range kindOrder {
		ks := kinds[k]
		if ks.Hits > 0 {
			ks.MeanMs /= float64(ks.Hits)
		}
		a.Kinds = append(a.Kinds, *ks)
	}
	a.stats()
	if !a.Drums {
		a.FretLeadMs = fretLead(rec)
	}
	a.tips()
	return a
}

func (a *Analysis) stats() {
	n := len(a.Offsets)
	if n == 0 {
		return
	}
	sum := 0.0
	for _, o := range a.Offsets {
		sum += o
	}
	a.MeanMs = sum / float64(n)
	v := 0.0
	for _, o := range a.Offsets {
		v += (o - a.MeanMs) * (o - a.MeanMs)
	}
	a.StdDevMs = math.Sqrt(v / float64(n))
	s := append([]float64(nil), a.Offsets...)
	sort.Float64s(s)
	if n%2 == 1 {
		a.MedianMs = s[n/2]
	} else {
		a.MedianMs = (s[n/2-1] + s[n/2]) / 2
	}
}

// missReason looks at the inputs around a missed note to explain it.
func (a *Analysis) missReason(rec *Recording, t *chart.Track, j engine.Judgment) string {
	n := &t.Notes[j.Note]
	window := 0.25
	early, late := rec.Settings.Windows(a.Drums)
	if a.Drums {
		bestDt := math.Inf(1)
		wrongType := false
		otherLane := ""
		for _, o := range rec.Judgments {
			if o.Result != engine.Overhit || math.Abs(o.Time-n.Time) > window {
				continue
			}
			dt := o.Time - n.Time
			if o.Lane == j.Lane {
				if math.Abs(dt) < math.Abs(bestDt) {
					bestDt = dt
				}
				if math.Abs(dt) <= math.Max(early, late) {
					wrongType = true
				}
			} else if math.Abs(dt) <= math.Max(early, late) && otherLane == "" {
				otherLane = drumLaneName(t.DrumMode, o.Lane, false)
			}
		}
		switch {
		case wrongType:
			if n.Cymbals&(1<<j.Lane) != 0 {
				return "hit the tom, chart has a cymbal"
			}
			return "hit the cymbal, chart has a tom"
		case !math.IsInf(bestDt, 1):
			return earlyLate(bestDt)
		case otherLane != "":
			return "hit " + otherLane + " instead"
		}
		return "no hit"
	}

	// Guitar: overstrums that targeted this note mean wrong frets.
	for _, o := range rec.Judgments {
		if o.Result == engine.Overstrum && o.Note == j.Note {
			return fmt.Sprintf("wrong frets: held %s, needed %s", chart.FretString(o.Held), n.LaneString())
		}
	}
	bestDt := math.Inf(1)
	for _, in := range rec.Inputs {
		if (in.A != action.StrumUp.String() && in.A != action.StrumDown.String()) || !in.D {
			continue
		}
		if dt := in.T - n.Time; math.Abs(dt) < math.Abs(bestDt) {
			bestDt = dt
		}
	}
	if math.Abs(bestDt) <= window {
		if bestDt < -early || bestDt > late {
			return earlyLate(bestDt)
		}
		return "strummed in time, frets didn't match"
	}
	if n.Kind == chart.HOPO || n.Kind == chart.Tap {
		return "no strum and no hammer-on"
	}
	return "no strum"
}

func earlyLate(dt float64) string {
	ms := math.Abs(dt * 1000)
	if dt < 0 {
		return fmt.Sprintf("%.0f ms too early", ms)
	}
	return fmt.Sprintf("%.0f ms too late", ms)
}

// fretLead measures how long before each strum the held frets last changed.
func fretLead(rec *Recording) float64 {
	var leads []float64
	lastFret := math.Inf(-1)
	for _, in := range rec.Inputs {
		a, _ := action.Parse(in.A)
		if _, ok := a.FretLane(); ok {
			lastFret = in.T
			continue
		}
		if a.IsStrum() && in.D && !math.IsInf(lastFret, -1) {
			if d := in.T - lastFret; d >= 0 && d < 0.5 {
				leads = append(leads, d*1000)
			}
		}
	}
	if len(leads) == 0 {
		return 0
	}
	sort.Float64s(leads)
	return leads[len(leads)/2]
}

func (a *Analysis) tips() {
	if a.Hits >= 20 && math.Abs(a.MeanMs) >= 8 {
		dir := "late"
		if a.MeanMs < 0 {
			dir = "early"
		}
		a.Tips = append(a.Tips, fmt.Sprintf(
			"You hit %.0f ms %s on average. If that's consistent across songs, change audio calibration by %+.0f ms.",
			math.Abs(a.MeanMs), dir, a.MeanMs))
	}
	if a.Hits >= 20 && a.StdDevMs > 30 {
		a.Tips = append(a.Tips, fmt.Sprintf("Timing spread is wide (±%.0f ms). The worst sections below are the ones to slow down and practice.", a.StdDevMs))
	}
	worst := -1
	for i, s := range a.Sections {
		if s.Judged() < 8 {
			continue
		}
		if worst < 0 || s.Accuracy() < a.Sections[worst].Accuracy() {
			worst = i
		}
	}
	if worst >= 0 && a.Sections[worst].Accuracy() < 0.9 {
		s := a.Sections[worst]
		a.Tips = append(a.Tips, fmt.Sprintf("Toughest section: %s (%.0f%%, %d missed).", s.Name, s.Accuracy()*100, s.Misses))
	}
	for _, k := range a.Kinds {
		if k.Total >= 10 && float64(k.Hits)/float64(k.Total) < 0.75 {
			a.Tips = append(a.Tips, fmt.Sprintf("%s notes are the weak spot: %d of %d hit.", strings.Title(k.Kind), k.Hits, k.Total))
		}
	}
	reasons := map[string]int{}
	for _, m := range a.Misses {
		key := m.Reason
		if strings.Contains(key, "wrong frets") {
			key = "wrong frets"
		} else if strings.Contains(key, "too early") {
			key = "too early"
		} else if strings.Contains(key, "too late") {
			key = "too late"
		}
		reasons[key]++
	}
	if len(a.Misses) >= 5 {
		best, bn := "", 0
		for k, v := range reasons {
			if v > bn {
				best, bn = k, v
			}
		}
		a.Tips = append(a.Tips, fmt.Sprintf("Most common miss: %s (%d of %d).", best, bn, len(a.Misses)))
	}
}

func drumLaneName(mode chart.DrumMode, lane int, cym bool) string {
	switch lane {
	case chart.DrumKick:
		return "kick"
	case chart.DrumRed:
		return "red"
	case chart.DrumYellow:
		if cym {
			return "yellow cymbal"
		}
		if mode == chart.DrumsPro {
			return "yellow tom"
		}
		return "yellow"
	case chart.DrumBlue:
		if cym {
			return "blue cymbal"
		}
		if mode == chart.DrumsPro {
			return "blue tom"
		}
		return "blue"
	case 4:
		if mode == chart.DrumsFiveLane {
			return "orange"
		}
		if cym {
			return "green cymbal"
		}
		if mode == chart.DrumsPro {
			return "green tom"
		}
		return "green"
	case chart.DrumGreen5:
		return "green"
	}
	return fmt.Sprintf("lane %d", lane)
}

// Text renders the analysis as a plain-text report.
func (a *Analysis) Text() string {
	var b strings.Builder
	r := a.Rec
	fmt.Fprintf(&b, "%s - %s  [%s %s]  %s\n", r.Song.Artist, r.Song.Name, r.Difficulty, r.Instrument, r.Started.Format("2006-01-02 15:04"))
	res := r.Result
	acc := res.HitRate()
	reached := ""
	if res.Reached() < res.NotesTotal {
		reached = fmt.Sprintf(" of %d reached", res.Reached())
	}
	fmt.Fprintf(&b, "Score %d   %d/%d notes%s (%.1f%%)   max streak %d   overstrums %d   %s\n",
		res.Score, res.NotesHit, res.NotesTotal, reached, acc*100, res.MaxStreak, res.Overstrums, strings.Repeat("*", res.Stars))
	if !r.Completed {
		fmt.Fprintf(&b, "(quit at %s)\n", fmtTime(r.EndTime))
	}
	if a.Hits > 0 {
		fmt.Fprintf(&b, "\nTiming (ms, negative = early): mean %+.1f  median %+.1f  spread ±%.1f   early %d / on time %d / late %d\n",
			a.MeanMs, a.MedianMs, a.StdDevMs, a.Early, a.OnTime, a.Late)
		max := 0
		for _, h := range a.Hist {
			if h > max {
				max = h
			}
		}
		for i, h := range a.Hist {
			lo := a.HistStartMs + float64(i)*a.BucketMs
			bar := 0
			if max > 0 {
				bar = h * 40 / max
			}
			fmt.Fprintf(&b, "  %+4.0f %s %d\n", lo, strings.Repeat("#", bar), h)
		}
	}
	if !a.Drums && a.FretLeadMs > 0 {
		fmt.Fprintf(&b, "Frets landed a median %.0f ms before each strum.\n", a.FretLeadMs)
	}
	if a.SustainTotal > 0 {
		fmt.Fprintf(&b, "Sustains: %d/%d held to the end, %.0f%% held on average\n", a.SustainDone, a.SustainTotal, a.SustainAvg*100)
	}
	b.WriteString("\nSections:\n")
	for _, s := range a.Sections {
		if s.Total == 0 && s.Overstrums == 0 {
			continue
		}
		if s.Judged() == 0 && s.Overstrums == 0 {
			fmt.Fprintf(&b, "  %-24s %6s  not reached\n", truncate(s.Name, 24), fmtTime(s.Start))
			continue
		}
		fmt.Fprintf(&b, "  %-24s %6s  %3d/%-3d  miss %-3d over %-3d  mean %+5.1f ms\n",
			truncate(s.Name, 24), fmtTime(s.Start), s.Hits, s.Total, s.Misses, s.Overstrums, s.MeanMs)
	}
	if len(a.Kinds) > 0 {
		b.WriteString("\nBy note type:\n")
		for _, k := range a.Kinds {
			fmt.Fprintf(&b, "  %-16s %4d/%-4d  mean %+5.1f ms\n", k.Kind, k.Hits, k.Total, k.MeanMs)
		}
	}
	if len(a.Misses) > 0 {
		b.WriteString("\nMisses:\n")
		for _, m := range a.Misses {
			fmt.Fprintf(&b, "  %s  %-14s %-12s %s\n", fmtTime(m.Time), truncate(m.Section, 14), m.Lanes, m.Reason)
		}
	}
	if len(a.Tips) > 0 {
		b.WriteString("\nNotes:\n")
		for _, t := range a.Tips {
			b.WriteString("  - " + t + "\n")
		}
	}
	return b.String()
}

func fmtTime(t float64) string {
	if math.IsInf(t, 0) || t < 0 {
		t = 0
	}
	m := int(t) / 60
	s := t - float64(m*60)
	return fmt.Sprintf("%d:%04.1f", m, s)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
