package chart

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// ProjectFormat is the format string jam writes into chart.jam.json.
const ProjectFormat = "jam-project/1"

// ProjNote is one authored note: a chord (several lanes) at one tick. Lanes
// are 0-4 for the frets and 5 for open. Length is the sustain in ticks, shared
// by every lane of the note (zero: no sustain). Kind is "strum", "hopo" or
// "tap"; empty means the builder works it out from the spacing, as for a new note.
//
// For drum tracks the lanes are the drum lanes (0 kick, 1 red, 2 yellow, 3 blue,
// 4 green or orange, 5 green on five-lane kits). Cymbals, Accents and Ghosts
// name the lanes they apply to, TwoX marks a double kick, and Kind and Length
// aren't used.
type ProjNote struct {
	Tick    int64  `json:"tick"`
	Lanes   []int  `json:"lanes"`
	Length  int64  `json:"length,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Cymbals []int  `json:"cymbals,omitempty"`
	Accents []int  `json:"accents,omitempty"`
	Ghosts  []int  `json:"ghosts,omitempty"`
	TwoX    bool   `json:"two_x,omitempty"`
}

// ProjTrack is the authored notes for one part and difficulty. Resolution is
// the ticks-per-quarter-note the tick values use.
type ProjTrack struct {
	Instrument string     `json:"instrument"`
	Difficulty string     `json:"difficulty"`
	Resolution int        `json:"resolution"`
	Mode       string     `json:"mode,omitempty"` // drums only: "4-lane", "pro" or "5-lane"
	Notes      []ProjNote `json:"notes"`
}

// parseDrumMode reads a drum mode name as DrumMode.String writes it.
func parseDrumMode(s string) (DrumMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "4-lane":
		return DrumsFourLane, true
	case "pro":
		return DrumsPro, true
	case "5-lane":
		return DrumsFiveLane, true
	}
	return DrumsFourLane, false
}

// projTracksFile is the part of chart.jam.json that holds authored notes.
type projTracksFile struct {
	Format string      `json:"format"`
	Tracks []ProjTrack `json:"tracks"`
}

// kindFromName turns a stored kind into a NoteKind; ok is false for "" or an
// unknown name, which leaves the builder's choice in place.
func kindFromName(s string) (NoteKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "strum":
		return Strum, true
	case "hopo":
		return HOPO, true
	case "tap":
		return Tap, true
	}
	return Strum, false
}

// hopoThresholdFor is the natural HOPO spacing the builder uses for a chart
// or MIDI source (the same rules the loaders apply).
func hopoThresholdFor(source string, res int, ini *SongIni) int64 {
	if source == "mid" {
		hopo := int64(res/3 + 1)
		if v, ok := ini.Int("hopo_frequency"); ok && v > 0 {
			hopo = v
		} else if v, ok := ini.Bool("eighthnote_hopo"); ok && v {
			hopo = int64(res/2 + 1)
		}
		return hopo
	}
	hopo := int64(65 * res / 192)
	if v, ok := ini.Int("hopo_frequency"); ok && v > 0 {
		hopo = v
	} else if v, ok := ini.Bool("eighthnote_hopo"); ok && v {
		hopo = int64(res / 2)
	}
	return hopo
}

// scaleTick moves a tick from one resolution to another.
func scaleTick(t int64, from, to int) int64 {
	if from <= 0 || from == to {
		return t
	}
	return int64(math.Round(float64(t) * float64(to) / float64(from)))
}

// applyProjectTracks replaces the notes of each part the project file has
// authored. A part whose project entry has no notes is removed. Drum parts
// are not authored yet, so they are skipped. The chart's star power, solo and
// roll phrases are kept (matched by tick).
func applyProjectTracks(c *Chart, dir string, ini *SongIni) error {
	path := filepath.Join(dir, ProjectFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var pf projTracksFile
	if err := json.Unmarshal(b, &pf); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if !strings.HasPrefix(pf.Format, "jam-project/") {
		return fmt.Errorf("%s: not a jam project (format %q)", filepath.Base(path), pf.Format)
	}
	if pf.Tracks == nil || c.Resolution <= 0 {
		return nil
	}
	hopo := hopoThresholdFor(c.Source, c.Resolution, ini)
	cutoff := int64(0)
	if v, ok := ini.Int("sustain_cutoff_threshold"); ok && v > 0 {
		cutoff = v
	}
	var firstErr error
	for _, pt := range pf.Tracks {
		inst, ok := ParseInstrument(pt.Instrument)
		diff, ok2 := ParseDifficulty(pt.Difficulty)
		if !ok || !ok2 {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: unknown track %q %q", filepath.Base(path), pt.Instrument, pt.Difficulty)
			}
			continue
		}
		key := TrackKey{inst, diff}
		if inst.IsDrums() {
			applyProjectDrums(c, key, pt)
			continue
		}
		rt := newRawTrack(key)
		if old := c.Tracks[key]; old != nil {
			for _, p := range old.StarPower {
				rt.starPower = append(rt.starPower, span{p.StartTick, p.EndTick})
			}
			for _, p := range old.Solos {
				rt.solos = append(rt.solos, span{p.StartTick, p.EndTick})
			}
			for _, p := range old.Rolls {
				rt.rolls = append(rt.rolls, span{p.StartTick, p.EndTick})
			}
		}
		kinds := map[int64]NoteKind{}
		for _, pn := range pt.Notes {
			tick := scaleTick(pn.Tick, pt.Resolution, c.Resolution)
			length := scaleTick(pn.Length, pt.Resolution, c.Resolution)
			if k, ok := kindFromName(pn.Kind); ok {
				kinds[tick] = k
			}
			for _, l := range pn.Lanes {
				if l < 0 || l > LaneOpen {
					continue
				}
				rt.notes = append(rt.notes, rawNote{tick: tick, lane: l, length: length})
			}
		}
		t := buildGuitar(rt, c.Tempo, buildOpts{hopoThreshold: hopo, sustainCutoff: cutoff})
		for i := range t.Notes {
			if k, ok := kinds[t.Notes[i].Tick]; ok {
				t.Notes[i].Kind = k
			}
		}
		if len(t.Notes) == 0 {
			delete(c.Tracks, key)
			continue
		}
		c.Tracks[key] = t
	}
	return firstErr
}

// applyProjectDrums replaces a drum part with the authored notes, built with
// the chart's drum builder. The mode is the project's, else the chart's own.
// The chart's star power, solo, fill and roll phrases are kept by tick.
func applyProjectDrums(c *Chart, key TrackKey, pt ProjTrack) {
	old := c.Tracks[key]
	mode := DrumsFourLane
	if m, ok := parseDrumMode(pt.Mode); ok {
		mode = m
	} else if old != nil {
		mode = old.DrumMode
	}
	rt := newRawTrack(key)
	if old != nil {
		for _, p := range old.StarPower {
			rt.starPower = append(rt.starPower, span{p.StartTick, p.EndTick})
		}
		for _, p := range old.Solos {
			rt.solos = append(rt.solos, span{p.StartTick, p.EndTick})
		}
		for _, p := range old.Fills {
			rt.fills = append(rt.fills, span{p.StartTick, p.EndTick})
		}
		for _, p := range old.Rolls {
			rt.rolls = append(rt.rolls, span{p.StartTick, p.EndTick})
		}
	}
	for _, pn := range pt.Notes {
		tick := scaleTick(pn.Tick, pt.Resolution, c.Resolution)
		for _, l := range pn.Lanes {
			if l < DrumKick || l > DrumGreen5 {
				continue
			}
			rt.notes = append(rt.notes, rawNote{tick: tick, lane: l, twoX: pn.TwoX && l == DrumKick})
		}
		for _, l := range pn.Cymbals {
			if l >= DrumKick && l <= DrumGreen5 {
				rt.cymbalTick[tick] |= 1 << l
			}
		}
		for _, l := range pn.Accents {
			if l >= DrumKick && l <= DrumGreen5 {
				rt.accentTick[tick] |= 1 << l
			}
		}
		for _, l := range pn.Ghosts {
			if l >= DrumKick && l <= DrumGreen5 {
				rt.ghostTick[tick] |= 1 << l
			}
		}
	}
	t := buildDrums(rt, c.Tempo, buildOpts{drumMode: mode})
	if len(t.Notes) == 0 {
		delete(c.Tracks, key)
		return
	}
	c.Tracks[key] = t
}

// ProjectNotes lists a track's notes as authored data, in the chart's own
// resolution. It returns nil when the chart has no such track.
func (c *Chart) ProjectNotes(key TrackKey) []ProjNote {
	t := c.Track(key.Inst, key.Diff)
	if t == nil {
		return nil
	}
	if key.Inst.IsDrums() {
		return drumProjectNotes(t)
	}
	out := make([]ProjNote, 0, len(t.Notes))
	for i := range t.Notes {
		n := &t.Notes[i]
		pn := ProjNote{Tick: n.Tick, Kind: n.Kind.String()}
		var length int64
		for l := 0; l < NumLanes; l++ {
			if n.Lanes&(1<<l) == 0 {
				continue
			}
			pn.Lanes = append(pn.Lanes, l)
			if d := n.EndTick[l] - n.Tick; d > length {
				length = d
			}
		}
		pn.Length = length
		if len(pn.Lanes) > 0 {
			out = append(out, pn)
		}
	}
	return out
}

// drumProjectNotes lists a drum track's notes: lanes, cymbals, accents, ghosts
// and double kicks.
func drumProjectNotes(t *Track) []ProjNote {
	out := make([]ProjNote, 0, len(t.Notes))
	for i := range t.Notes {
		n := &t.Notes[i]
		pn := ProjNote{Tick: n.Tick, TwoX: n.DoubleKick}
		for l := 0; l < NumLanes; l++ {
			bit := uint8(1) << l
			if n.Lanes&bit != 0 {
				pn.Lanes = append(pn.Lanes, l)
			}
			if n.Cymbals&bit != 0 {
				pn.Cymbals = append(pn.Cymbals, l)
			}
			if n.Accents&bit != 0 {
				pn.Accents = append(pn.Accents, l)
			}
			if n.Ghosts&bit != 0 {
				pn.Ghosts = append(pn.Ghosts, l)
			}
		}
		if len(pn.Lanes) > 0 {
			out = append(out, pn)
		}
	}
	return out
}

// LoadProjectTracks reads the authored tracks in dir's chart.jam.json. A
// missing file gives nil and no error. A file that exists but can't be read
// is an error, so that a save never overwrites it.
func LoadProjectTracks(dir string) ([]ProjTrack, error) {
	b, err := os.ReadFile(filepath.Join(dir, ProjectFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pf projTracksFile
	if err := json.Unmarshal(b, &pf); err != nil {
		return nil, fmt.Errorf("%s: %w", ProjectFile, err)
	}
	return pf.Tracks, nil
}

// SaveProjectTracks writes the authored tracks into dir's chart.jam.json. Other
// sections of the file (vocals, grid, and anything else) are kept as they are.
func SaveProjectTracks(dir string, tracks []ProjTrack) error {
	doc, err := readProjectDoc(dir)
	if err != nil {
		return err
	}
	if tracks == nil {
		tracks = []ProjTrack{}
	}
	tr, err := json.Marshal(tracks)
	if err != nil {
		return err
	}
	doc["tracks"] = tr
	return writeProjectDoc(dir, doc)
}

// SetProjectTrack returns tracks with the entry for key replaced by pt (added
// when there is none). The order of the other entries is kept.
func SetProjectTrack(tracks []ProjTrack, key TrackKey, pt ProjTrack) []ProjTrack {
	for i := range tracks {
		inst, ok1 := ParseInstrument(tracks[i].Instrument)
		diff, ok2 := ParseDifficulty(tracks[i].Difficulty)
		if ok1 && ok2 && inst == key.Inst && diff == key.Diff {
			tracks[i] = pt
			return tracks
		}
	}
	return append(tracks, pt)
}
