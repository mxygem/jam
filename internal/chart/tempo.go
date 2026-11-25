package chart

import "sort"

// Tempo is a tempo change. Sec is filled in by TempoMap.finalize and does not
// include the chart offset.
type Tempo struct {
	Tick int64
	BPM  float64
	sec  float64
}

// TimeSig is a time signature change.
type TimeSig struct {
	Tick int64
	Num  int
	Den  int
}

// TempoMap converts between ticks and seconds on the song clock.
type TempoMap struct {
	Resolution int
	Tempos     []Tempo
	Sigs       []TimeSig
	// Offset in seconds added to every tick time so that times line up with
	// the audio: .chart Offset plus song.ini delay.
	Offset float64
	// Anchors pin ticks to song times (sync points), sorted by tick with
	// song times rising. Empty: the tempo changes alone decide.
	Anchors []Anchor
}

// Anchor pins a tick to a song time in seconds (the same clock as Seconds).
type Anchor struct {
	Tick int64
	Sec  float64
}

// WithAnchors returns a copy of tm with the sync points set. Points are sorted
// by tick; a point whose time doesn't rise with its tick is dropped, so the
// result is always a valid mapping. With no points, tm itself is returned.
func (tm *TempoMap) WithAnchors(points []Anchor) *TempoMap {
	valid := ValidAnchors(points)
	if len(valid) == 0 && len(tm.Anchors) == 0 {
		return tm
	}
	c := *tm
	c.Anchors = valid
	return &c
}

// ValidAnchors sorts sync points by tick, keeps the last of any two at the
// same tick, and drops points whose time doesn't rise with the tick.
func ValidAnchors(points []Anchor) []Anchor {
	in := append([]Anchor(nil), points...)
	sort.SliceStable(in, func(i, j int) bool { return in[i].Tick < in[j].Tick })
	var out []Anchor
	for _, p := range in {
		if n := len(out); n > 0 && out[n-1].Tick == p.Tick {
			out = out[:n-1]
		}
		if n := len(out); n > 0 && p.Sec <= out[n-1].Sec {
			continue
		}
		out = append(out, p)
	}
	return out
}

// NewTempoMap builds a map from unsorted tempo and signature lists, applying
// the format defaults (120 BPM, 4/4) when tick 0 has none.
func NewTempoMap(res int, tempos []Tempo, sigs []TimeSig, offset float64) *TempoMap {
	if res <= 0 {
		res = 192
	}
	sort.SliceStable(tempos, func(i, j int) bool { return tempos[i].Tick < tempos[j].Tick })
	sort.SliceStable(sigs, func(i, j int) bool { return sigs[i].Tick < sigs[j].Tick })
	if len(tempos) == 0 || tempos[0].Tick != 0 {
		tempos = append([]Tempo{{Tick: 0, BPM: 120}}, tempos...)
	}
	if len(sigs) == 0 || sigs[0].Tick != 0 {
		sigs = append([]TimeSig{{Tick: 0, Num: 4, Den: 4}}, sigs...)
	}
	// Collapse duplicates at the same tick, last one wins.
	tempos = dedupeTempos(tempos)
	sigs = dedupeSigs(sigs)
	for i := range tempos {
		if tempos[i].BPM <= 0 {
			tempos[i].BPM = 120
		}
	}
	for i := range sigs {
		if sigs[i].Num <= 0 {
			sigs[i].Num = 4
		}
		if sigs[i].Den <= 0 {
			sigs[i].Den = 4
		}
	}
	tm := &TempoMap{Resolution: res, Tempos: tempos, Sigs: sigs, Offset: offset}
	for i := 1; i < len(tm.Tempos); i++ {
		p := tm.Tempos[i-1]
		tm.Tempos[i].sec = p.sec + tm.span(tm.Tempos[i].Tick-p.Tick, p.BPM)
	}
	return tm
}

func dedupeTempos(in []Tempo) []Tempo {
	out := in[:0]
	for _, t := range in {
		if n := len(out); n > 0 && out[n-1].Tick == t.Tick {
			out[n-1] = t
			continue
		}
		out = append(out, t)
	}
	return out
}

func dedupeSigs(in []TimeSig) []TimeSig {
	out := in[:0]
	for _, s := range in {
		if n := len(out); n > 0 && out[n-1].Tick == s.Tick {
			out[n-1] = s
			continue
		}
		out = append(out, s)
	}
	return out
}

func (tm *TempoMap) span(ticks int64, bpm float64) float64 {
	return float64(ticks) / float64(tm.Resolution) * 60.0 / bpm
}

func (tm *TempoMap) tempoIndexForTick(tick int64) int {
	i := sort.Search(len(tm.Tempos), func(i int) bool { return tm.Tempos[i].Tick > tick })
	if i == 0 {
		return 0
	}
	return i - 1
}

// Seconds returns the song time of tick.
func (tm *TempoMap) Seconds(tick int64) float64 {
	return tm.SecondsF(float64(tick))
}

// SecondsF is Seconds for fractional ticks.
func (tm *TempoMap) SecondsF(tick float64) float64 {
	if len(tm.Anchors) == 0 {
		return tm.tempoSeconds(tick)
	}
	return tm.anchoredSeconds(tick)
}

// Ticks returns the (fractional) tick at song time sec.
func (tm *TempoMap) Ticks(sec float64) float64 {
	if len(tm.Anchors) == 0 {
		return tm.tempoTicks(sec)
	}
	return tm.anchoredTicks(sec)
}

// tempoSeconds and tempoTicks convert with the tempo changes alone, ignoring
// sync points.
func (tm *TempoMap) tempoSeconds(tick float64) float64 {
	i := tm.tempoIndexForTick(int64(tick))
	t := tm.Tempos[i]
	return t.sec + (tick-float64(t.Tick))/float64(tm.Resolution)*60.0/t.BPM + tm.Offset
}

func (tm *TempoMap) tempoTicks(sec float64) float64 {
	raw := sec - tm.Offset
	i := sort.Search(len(tm.Tempos), func(i int) bool { return tm.Tempos[i].sec > raw })
	if i > 0 {
		i--
	}
	t := tm.Tempos[i]
	return float64(t.Tick) + (raw-t.sec)*t.BPM/60.0*float64(tm.Resolution)
}

// anchoredSeconds is linear between sync points. Before the first and after
// the last, it follows the tempo changes, shifted to meet the nearest point.
func (tm *TempoMap) anchoredSeconds(tick float64) float64 {
	a := tm.Anchors
	if tick <= float64(a[0].Tick) {
		return a[0].Sec + tm.tempoSeconds(tick) - tm.tempoSeconds(float64(a[0].Tick))
	}
	last := a[len(a)-1]
	if tick >= float64(last.Tick) {
		return last.Sec + tm.tempoSeconds(tick) - tm.tempoSeconds(float64(last.Tick))
	}
	i := sort.Search(len(a), func(i int) bool { return float64(a[i].Tick) > tick }) - 1
	p, q := a[i], a[i+1]
	return p.Sec + (tick-float64(p.Tick))/float64(q.Tick-p.Tick)*(q.Sec-p.Sec)
}

// anchoredTicks is the inverse of anchoredSeconds.
func (tm *TempoMap) anchoredTicks(sec float64) float64 {
	a := tm.Anchors
	if sec <= a[0].Sec {
		return tm.tempoTicks(sec - a[0].Sec + tm.tempoSeconds(float64(a[0].Tick)))
	}
	last := a[len(a)-1]
	if sec >= last.Sec {
		return tm.tempoTicks(sec - last.Sec + tm.tempoSeconds(float64(last.Tick)))
	}
	i := sort.Search(len(a), func(i int) bool { return a[i].Sec > sec }) - 1
	p, q := a[i], a[i+1]
	return float64(p.Tick) + (sec-p.Sec)/(q.Sec-p.Sec)*float64(q.Tick-p.Tick)
}

// BPMAt returns the tempo in effect at tick.
func (tm *TempoMap) BPMAt(tick int64) float64 {
	return tm.Tempos[tm.tempoIndexForTick(tick)].BPM
}

// Beats returns the number of quarter-note beats between two song times.
func (tm *TempoMap) Beats(from, to float64) float64 {
	return (tm.Ticks(to) - tm.Ticks(from)) / float64(tm.Resolution)
}

// SigAt returns the time signature in effect at tick.
func (tm *TempoMap) SigAt(tick int64) TimeSig {
	i := sort.Search(len(tm.Sigs), func(i int) bool { return tm.Sigs[i].Tick > tick })
	if i == 0 {
		return tm.Sigs[0]
	}
	return tm.Sigs[i-1]
}

// BeatLineKind distinguishes highway lines.
type BeatLineKind uint8

const (
	BeatMeasure BeatLineKind = iota
	BeatStrong
	BeatWeak
)

// BeatLine is a line to draw across the highway.
type BeatLine struct {
	Time float64
	Kind BeatLineKind
}

// BeatLines appends the beat lines whose times fall in [from, to].
func (tm *TempoMap) BeatLines(dst []BeatLine, from, to float64) []BeatLine {
	if to < from {
		return dst
	}
	startTick := tm.Ticks(from)
	if startTick < 0 {
		startTick = 0
	}
	endTick := tm.Ticks(to)
	for si, sig := range tm.Sigs {
		segEnd := int64(1 << 62)
		if si+1 < len(tm.Sigs) {
			segEnd = tm.Sigs[si+1].Tick
		}
		if float64(segEnd) < startTick {
			continue
		}
		if float64(sig.Tick) > endTick {
			break
		}
		beatTicks := int64(tm.Resolution) * 4 / int64(sig.Den)
		if beatTicks <= 0 {
			beatTicks = int64(tm.Resolution)
		}
		// first beat in this segment at or after startTick
		k := int64(0)
		if float64(sig.Tick) < startTick {
			k = int64((startTick - float64(sig.Tick)) / float64(beatTicks))
		}
		for {
			tick := sig.Tick + k*beatTicks
			if tick >= segEnd || float64(tick) > endTick {
				break
			}
			kind := BeatWeak
			switch {
			case k%int64(sig.Num) == 0:
				kind = BeatMeasure
			case sig.Den <= 4 || k%2 == 0:
				kind = BeatStrong
			}
			sec := tm.Seconds(tick)
			if sec >= from && sec <= to {
				dst = append(dst, BeatLine{Time: sec, Kind: kind})
			}
			k++
		}
	}
	return dst
}
