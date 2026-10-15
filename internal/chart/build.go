package chart

import (
	"sort"
)

// span is a tick range used for modifiers and phrases. A zero-length span
// covers exactly its start tick.
type span struct{ start, end int64 }

func (s span) contains(t int64) bool {
	if s.end <= s.start {
		return t == s.start
	}
	return t >= s.start && t < s.end
}

// containsInclusive includes the end tick (drum fills).
func (s span) containsInclusive(t int64) bool { return t >= s.start && t <= s.end }

func anySpan(spans []span, t int64) bool {
	for _, s := range spans {
		if s.contains(t) {
			return true
		}
	}
	return false
}

type rawNote struct {
	tick   int64
	lane   int
	length int64
	vel    int
	twoX   bool // drums: Expert+ kick
}

// rawTrack is the format-neutral intermediate form produced by both parsers.
type rawTrack struct {
	key   TrackKey
	notes []rawNote

	// guitar modifiers
	flip       map[int64]bool // .chart strum/HOPO flip
	tapTick    map[int64]bool // .chart tap flag
	forceHOPO  []span         // .mid
	forceStrum []span         // .mid
	tapSpans   []span         // .mid

	// drums modifiers
	cymbalTick map[int64]uint8 // .chart cymbal markers (lane bits)
	accentTick map[int64]uint8 // .chart accent markers (lane bits)
	ghostTick  map[int64]uint8 // .chart ghost markers (lane bits)
	tomSpans   [NumLanes][]span
	midDrums   bool // .mid semantics: cymbals by default on pro tracks
	dynamics   bool // .mid accent/ghost via velocity enabled

	starPower []span
	solos     []span
	fills     []span
	rolls     []span
}

func newRawTrack(key TrackKey) *rawTrack {
	return &rawTrack{
		key:        key,
		flip:       map[int64]bool{},
		tapTick:    map[int64]bool{},
		cymbalTick: map[int64]uint8{},
		accentTick: map[int64]uint8{},
		ghostTick:  map[int64]uint8{},
	}
}

type buildOpts struct {
	hopoThreshold int64 // ticks; notes this close or closer may be natural HOPOs
	sustainCutoff int64 // ticks; sustains shorter than this are dropped
	midForcing    bool  // .mid force markers set the type instead of flipping it
	drumMode      DrumMode
}

func (rt *rawTrack) phrases(tm *TempoMap, spans []span) []Phrase {
	out := make([]Phrase, 0, len(spans))
	for _, s := range spans {
		out = append(out, Phrase{StartTick: s.start, EndTick: s.end, Start: tm.Seconds(s.start), End: tm.Seconds(s.end)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartTick < out[j].StartTick })
	return out
}

// groupByTick sorts raw notes and returns the index ranges of equal ticks.
func groupByTick(notes []rawNote) [][]rawNote {
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].tick < notes[j].tick })
	var groups [][]rawNote
	for i := 0; i < len(notes); {
		j := i
		for j < len(notes) && notes[j].tick == notes[i].tick {
			j++
		}
		groups = append(groups, notes[i:j])
		i = j
	}
	return groups
}

func buildGuitar(rt *rawTrack, tm *TempoMap, o buildOpts) *Track {
	t := &Track{Key: rt.key}
	groups := groupByTick(rt.notes)
	t.Notes = make([]Note, 0, len(groups))
	for _, g := range groups {
		n := Note{Tick: g[0].tick, Time: tm.Seconds(g[0].tick)}
		var length [NumLanes]int64
		for _, rn := range g {
			if rn.lane < 0 || rn.lane >= NumLanes {
				continue
			}
			n.Lanes |= 1 << rn.lane
			if rn.length > length[rn.lane] {
				length[rn.lane] = rn.length
			}
		}
		if n.Lanes&FretMask != 0 && n.Lanes&OpenBit != 0 {
			// An open note can't be combined with frets on 5-fret; keep frets.
			n.Lanes &^= OpenBit
			length[LaneOpen] = 0
		}
		if n.Lanes == 0 {
			continue
		}
		for l := 0; l < NumLanes; l++ {
			if n.Lanes&(1<<l) == 0 || length[l] <= 0 || length[l] < o.sustainCutoff {
				continue
			}
			n.EndTick[l] = n.Tick + length[l]
			n.End[l] = tm.Seconds(n.EndTick[l])
		}
		t.Notes = append(t.Notes, n)
	}

	for i := range t.Notes {
		n := &t.Notes[i]
		n.Index = i
		natural := false
		if i > 0 && !n.IsChord() {
			p := &t.Notes[i-1]
			if n.Tick-p.Tick <= o.hopoThreshold && n.Lanes&p.Lanes == 0 {
				natural = true
			}
		}
		kind := Strum
		if natural {
			kind = HOPO
		}
		if o.midForcing {
			switch {
			case anySpan(rt.forceStrum, n.Tick):
				kind = Strum
			case anySpan(rt.forceHOPO, n.Tick):
				kind = HOPO
			}
		} else if rt.flip[n.Tick] {
			if kind == HOPO {
				kind = Strum
			} else {
				kind = HOPO
			}
		}
		if rt.tapTick[n.Tick] || anySpan(rt.tapSpans, n.Tick) {
			kind = Tap
		}
		n.Kind = kind
	}
	finishCommon(t, rt, tm)
	return t
}

func buildDrums(rt *rawTrack, tm *TempoMap, o buildOpts) *Track {
	t := &Track{Key: rt.key, DrumMode: o.drumMode}
	groups := groupByTick(rt.notes)
	t.Notes = make([]Note, 0, len(groups))
	for _, g := range groups {
		n := Note{Tick: g[0].tick, Time: tm.Seconds(g[0].tick)}
		normalKick, twoXKick := false, false
		for _, rn := range g {
			if rn.lane < 0 || rn.lane >= NumLanes {
				continue
			}
			if o.drumMode != DrumsFiveLane && rn.lane == DrumGreen5 {
				continue
			}
			n.Lanes |= 1 << rn.lane
			if rn.lane == DrumKick {
				if rn.twoX {
					twoXKick = true
				} else {
					normalKick = true
				}
			}
			if rt.dynamics && rn.lane != DrumKick {
				switch rn.vel {
				case 127:
					n.Accents |= 1 << rn.lane
				case 1:
					n.Ghosts |= 1 << rn.lane
				}
			}
		}
		if n.Lanes == 0 {
			continue
		}
		n.DoubleKick = twoXKick && !normalKick
		n.Accents |= rt.accentTick[n.Tick] & n.Lanes
		n.Ghosts |= rt.ghostTick[n.Tick] & n.Lanes
		switch o.drumMode {
		case DrumsFiveLane:
			n.Cymbals = n.Lanes & (1<<DrumYellow | 1<<DrumOrange)
		case DrumsPro:
			for _, l := range []int{DrumYellow, DrumBlue, DrumGreen4} {
				if n.Lanes&(1<<l) == 0 {
					continue
				}
				if rt.midDrums {
					if !anySpan(rt.tomSpans[l], n.Tick) {
						n.Cymbals |= 1 << l
					}
				} else if rt.cymbalTick[n.Tick]&(1<<l) != 0 {
					n.Cymbals |= 1 << l
				}
			}
		}
		t.Notes = append(t.Notes, n)
	}
	for i := range t.Notes {
		t.Notes[i].Index = i
	}
	finishCommon(t, rt, tm)

	// Activation notes: the note closest to the end of each fill phrase.
	t.Fills = rt.phrases(tm, rt.fills)
	for _, f := range rt.fills {
		best := -1
		for i := range t.Notes {
			if t.Notes[i].Tick > f.end {
				break
			}
			if span(f).containsInclusive(t.Notes[i].Tick) {
				best = i
			}
		}
		if best >= 0 {
			t.Notes[best].Activation = true
		}
	}
	t.Rolls = rt.phrases(tm, rt.rolls)
	return t
}

func finishCommon(t *Track, rt *rawTrack, tm *TempoMap) {
	t.StarPower = rt.phrases(tm, rt.starPower)
	t.Solos = rt.phrases(tm, rt.solos)
	for _, sp := range rt.starPower {
		last := -1
		for i := range t.Notes {
			if sp.contains(t.Notes[i].Tick) {
				t.Notes[i].StarPower = true
				last = i
			}
		}
		if last >= 0 {
			t.Notes[last].SPEnd = true
		}
	}
	for _, s := range rt.solos {
		for i := range t.Notes {
			if s.containsInclusive(t.Notes[i].Tick) {
				t.Notes[i].Solo = true
			}
		}
	}
}

// detectDrumMode applies the song.ini overrides and the note heuristics from
// the community chart format docs.
func detectDrumMode(ini *SongIni, cymbalsOrToms, fiveLane bool) DrumMode {
	if ini != nil {
		if v, ok := ini.Bool("pro_drums"); ok && v {
			return DrumsPro
		}
		if v, ok := ini.Bool("five_lane_drums"); ok && v {
			return DrumsFiveLane
		}
	}
	switch {
	case cymbalsOrToms:
		return DrumsPro
	case fiveLane:
		return DrumsFiveLane
	}
	return DrumsFourLane
}
