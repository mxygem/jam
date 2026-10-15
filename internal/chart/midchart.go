package chart

import (
	"os"
	"strings"
)

// LoadMidiChart reads a notes.mid. ini may be nil.
func LoadMidiChart(path string, ini *SongIni) (*Chart, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseMidiChart(b, ini)
}

var midiTrackInstruments = map[string]Instrument{
	"PART GUITAR":      Guitar,
	"T1 GEMS":          Guitar,
	"PART GUITAR COOP": GuitarCoop,
	"PART BASS":        Bass,
	"PART RHYTHM":      Rhythm,
	"PART KEYS":        Keys,
	"PART DRUMS":       Drums,
	"PART DRUM":        Drums,
}

var midiDiffBase = map[Difficulty]int{Easy: 60, Medium: 72, Hard: 84, Expert: 96}

// psSysex decodes a Phase Shift style SysEx modifier:
// 50 53 00 00 <difficulty> <type> <value>.
func psSysex(data []byte) (diff int, typ byte, on bool, ok bool) {
	d := data
	if len(d) > 0 && d[len(d)-1] == 0xF7 {
		d = d[:len(d)-1]
	}
	if len(d) < 7 || d[0] != 0x50 || d[1] != 0x53 || d[2] != 0 || d[3] != 0 {
		return 0, 0, false, false
	}
	return int(d[4]), d[5], d[6] != 0, true
}

// sysexSpans collects ranges for a PS SysEx modifier type per difficulty
// (index 0-3) and for all difficulties (index 4).
func sysexSpans(events []smfEvent, want byte) [5][]span {
	var out [5][]span
	var start [5]int64
	for i := range start {
		start[i] = -1
	}
	for _, e := range events {
		if e.status != 0xF0 && e.status != 0xF7 {
			continue
		}
		diff, typ, on, ok := psSysex(e.data)
		if !ok || typ != want {
			continue
		}
		idx := 4
		if diff >= 0 && diff <= 3 {
			idx = diff
		}
		if on {
			start[idx] = e.tick
		} else if start[idx] >= 0 {
			out[idx] = append(out[idx], span{start[idx], e.tick})
			start[idx] = -1
		}
	}
	return out
}

func hasText(events []smfEvent, want string) bool {
	for _, e := range events {
		if e.isText() && strings.EqualFold(strings.Trim(string(e.data), "[] "), want) {
			return true
		}
	}
	return false
}

// ParseMidiChart parses notes.mid content.
func ParseMidiChart(b []byte, ini *SongIni) (*Chart, error) {
	f, err := parseSMF(b)
	if err != nil {
		return nil, err
	}
	res := f.resolution
	offset := 0.0
	if d, ok := ini.Int("delay"); ok {
		offset = float64(d) / 1000
	}

	var tempos []Tempo
	var sigs []TimeSig
	// Tempo map events normally live on track 0, but collect from all tracks
	// that aren't instrument tracks to be lenient.
	for i, t := range f.tracks {
		if _, isInst := midiTrackInstruments[strings.ToUpper(strings.TrimSpace(t.name))]; i != 0 && isInst {
			continue
		}
		for _, e := range t.events {
			if e.status != 0xFF {
				continue
			}
			switch e.meta {
			case 0x51:
				if len(e.data) >= 3 {
					us := int(e.data[0])<<16 | int(e.data[1])<<8 | int(e.data[2])
					if us > 0 {
						tempos = append(tempos, Tempo{Tick: e.tick, BPM: 60_000_000 / float64(us)})
					}
				}
			case 0x58:
				if len(e.data) >= 2 {
					sigs = append(sigs, TimeSig{Tick: e.tick, Num: int(e.data[0]), Den: 1 << e.data[1]})
				}
			}
		}
	}
	tm := NewTempoMap(res, tempos, sigs, offset)
	c := &Chart{Source: "mid", Resolution: res, Tempo: tm, Tracks: map[TrackKey]*Track{}, Meta: map[string]string{}}

	hopo := int64(res/3 + 1)
	if v, ok := ini.Int("hopo_frequency"); ok && v > 0 {
		hopo = v
	} else if v, ok := ini.Bool("eighthnote_hopo"); ok && v {
		hopo = int64(res/2 + 1)
	}
	cutoff := int64(res / 3)
	if v, ok := ini.Int("sustain_cutoff_threshold"); ok && v >= 0 {
		cutoff = v
	}
	spNote := -1
	if v, ok := ini.Int("multiplier_note"); ok {
		spNote = int(v)
	} else if v, ok := ini.Int("star_power_note"); ok {
		spNote = int(v)
	}

	for _, t := range f.tracks {
		name := strings.ToUpper(strings.TrimSpace(t.name))
		if name == "EVENTS" {
			for _, e := range t.events {
				if !e.isText() {
					continue
				}
				text := string(e.data)
				c.Events = append(c.Events, TextEvent{Tick: e.tick, Time: tm.Seconds(e.tick), Text: text})
				if sn, ok := sectionName(text); ok {
					c.Sections = append(c.Sections, Section{Tick: e.tick, Time: tm.Seconds(e.tick), Name: sn})
				}
			}
			continue
		}
		if name == "PART VOCALS" {
			c.Lyrics = lyricsFromVocals(tm, t)
			continue
		}
		inst, ok := midiTrackInstruments[name]
		if !ok {
			continue
		}
		if _, dup := c.Tracks[TrackKey{inst, Expert}]; dup {
			continue // e.g. PART DRUM after PART DRUMS
		}
		notes := pairNotes(t.events)
		if inst.IsDrums() {
			parseMidiDrums(c, tm, t, notes, ini)
		} else {
			parseMidiGuitar(c, tm, inst, t, notes, hopo, cutoff, spNote)
		}
	}
	if len(c.Lyrics) == 0 {
		c.Lyrics = lyricsFromEvents(tm, c.Events)
	}
	return c, nil
}

func notesToSpans(notes []midiNote, number int) []span {
	var out []span
	for _, n := range notes {
		if n.note == number {
			out = append(out, span{n.start, n.end})
		}
	}
	return out
}

func parseMidiGuitar(c *Chart, tm *TempoMap, inst Instrument, t smfTrack, notes []midiNote, hopo, cutoff int64, spNote int) {
	enhancedOpens := hasText(t.events, "ENHANCED_OPENS")
	openSys := sysexSpans(t.events, 0x01)
	tapSys := sysexSpans(t.events, 0x04)

	has116, has103 := false, false
	for _, n := range notes {
		switch n.note {
		case 116:
			has116 = true
		case 103:
			has103 = true
		}
	}
	sp, solo := 116, 103
	switch {
	case spNote == 103 || (spNote != 116 && !has116 && has103):
		sp, solo = 103, -1
	}
	spSpans := notesToSpans(notes, sp)
	var soloSpans []span
	if solo > 0 {
		soloSpans = notesToSpans(notes, solo)
	}
	tapNote := notesToSpans(notes, 104)

	for diff, base := range midiDiffBase {
		rt := newRawTrack(TrackKey{inst, diff})
		opens := append(append([]span{}, openSys[diff]...), openSys[4]...)
		for _, n := range notes {
			lane := n.note - base
			switch {
			case lane >= 0 && lane <= 4:
				if lane == LaneGreen && anySpan(opens, n.start) {
					lane = LaneOpen
				}
				rt.notes = append(rt.notes, rawNote{tick: n.start, lane: lane, length: n.end - n.start, vel: n.vel})
			case lane == -1 && enhancedOpens:
				rt.notes = append(rt.notes, rawNote{tick: n.start, lane: LaneOpen, length: n.end - n.start, vel: n.vel})
			case lane == 5:
				rt.forceHOPO = append(rt.forceHOPO, span{n.start, n.end})
			case lane == 6:
				rt.forceStrum = append(rt.forceStrum, span{n.start, n.end})
			}
		}
		if len(rt.notes) == 0 {
			continue
		}
		rt.tapSpans = append(append(append(rt.tapSpans, tapNote...), tapSys[diff]...), tapSys[4]...)
		rt.starPower = spSpans
		rt.solos = soloSpans
		tr := buildGuitar(rt, tm, buildOpts{hopoThreshold: hopo, sustainCutoff: cutoff, midForcing: true})
		if len(tr.Notes) > 0 {
			c.Tracks[rt.key] = tr
		}
	}
}

func parseMidiDrums(c *Chart, tm *TempoMap, t smfTrack, notes []midiNote, ini *SongIni) {
	dynamics := hasText(t.events, "ENABLE_CHART_DYNAMICS")
	toms := [NumLanes][]span{}
	toms[DrumYellow] = notesToSpans(notes, 110)
	toms[DrumBlue] = notesToSpans(notes, 111)
	toms[DrumGreen4] = notesToSpans(notes, 112)
	tomMarkers := len(toms[DrumYellow])+len(toms[DrumBlue])+len(toms[DrumGreen4]) > 0
	fiveLane := false
	for _, n := range notes {
		for _, base := range midiDiffBase {
			if n.note == base+5 {
				fiveLane = true
			}
		}
	}
	mode := detectDrumMode(ini, tomMarkers, fiveLane)
	spSpans := notesToSpans(notes, 116)
	soloSpans := notesToSpans(notes, 103)
	fillSpans := notesToSpans(notes, 120)
	rollSpans := append(notesToSpans(notes, 126), notesToSpans(notes, 127)...)

	for diff, base := range midiDiffBase {
		rt := newRawTrack(TrackKey{Drums, diff})
		rt.midDrums = true
		rt.dynamics = dynamics
		rt.tomSpans = toms
		for _, n := range notes {
			lane := n.note - base
			switch {
			case lane >= 0 && lane <= 5:
				rt.notes = append(rt.notes, rawNote{tick: n.start, lane: lane, vel: n.vel})
			case diff == Expert && n.note == 95:
				rt.notes = append(rt.notes, rawNote{tick: n.start, lane: DrumKick, vel: n.vel, twoX: true})
			}
		}
		if len(rt.notes) == 0 {
			continue
		}
		rt.starPower = spSpans
		rt.solos = soloSpans
		rt.fills = fillSpans
		rt.rolls = rollSpans
		tr := buildDrums(rt, tm, buildOpts{drumMode: mode})
		if len(tr.Notes) > 0 {
			c.Tracks[rt.key] = tr
		}
	}
}
