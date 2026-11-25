package chart

import (
	"encoding/json"
	"fmt"
)

// ProjAnchor is one sync point as stored in a project file: a tick and the
// song time (seconds, the audio clock) it lands on.
type ProjAnchor struct {
	Tick int64   `json:"tick"`
	Sec  float64 `json:"sec"`
}

func anchorsFrom(list []ProjAnchor) []Anchor {
	out := make([]Anchor, 0, len(list))
	for _, p := range list {
		out = append(out, Anchor{Tick: p.Tick, Sec: p.Sec})
	}
	return out
}

func projAnchorsOf(points []Anchor) []ProjAnchor {
	out := make([]ProjAnchor, 0, len(points))
	for _, p := range points {
		out = append(out, ProjAnchor{Tick: p.Tick, Sec: p.Sec})
	}
	return out
}

// ProjectTempos returns the base tempo map the project's ticks use (its grid,
// or the chart's own) and the sync points stored with it. The sync points are
// not applied to the base, so the editor can edit them.
func (c *Chart) ProjectTempos(dir string, ini *SongIni) (*TempoMap, []Anchor, error) {
	doc, err := readProjectDoc(dir)
	if err != nil {
		return nil, nil, err
	}
	base := c.Tempo
	if raw, ok := doc["grid"]; ok {
		var g ProjGrid
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, nil, fmt.Errorf("%s: grid: %w", ProjectFile, err)
		}
		if g.Resolution > 0 && len(g.Tempos) > 0 {
			base = g.tempoMap(ini)
		}
	}
	var list []ProjAnchor
	if raw, ok := doc["sync"]; ok {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, nil, fmt.Errorf("%s: sync: %w", ProjectFile, err)
		}
	}
	return base, ValidAnchors(anchorsFrom(list)), nil
}

// ProjectTempo is the tempo map that the project's ticks are measured on,
// with its sync points applied.
func (c *Chart) ProjectTempo(dir string, ini *SongIni) (*TempoMap, error) {
	base, anchors, err := c.ProjectTempos(dir, ini)
	if err != nil {
		return nil, err
	}
	return base.WithAnchors(anchors), nil
}

// LoadProjectSync reads the sync points stored in dir's project file.
func LoadProjectSync(dir string) ([]Anchor, error) {
	_, anchors, err := (&Chart{}).ProjectTempos(dir, nil)
	return anchors, err
}

// SaveProjectSync writes the sync points into dir's project file, keeping the
// other sections.
func SaveProjectSync(dir string, points []Anchor) error {
	doc, err := readProjectDoc(dir)
	if err != nil {
		return err
	}
	list := projAnchorsOf(ValidAnchors(points))
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	doc["sync"] = b
	return writeProjectDoc(dir, doc)
}

// applyProjectSync moves the whole chart onto the project's tempo and sync
// points, when there are sync points. Notes, sustains, phrases, sections,
// events and the chart's own lyrics are all retimed from their ticks (lyrics
// through their times, which were made from the chart's tempo).
func applyProjectSync(c *Chart, dir string, ini *SongIni) error {
	base, anchors, err := c.ProjectTempos(dir, ini)
	if err != nil {
		return err
	}
	if len(anchors) == 0 {
		return nil
	}
	c.retime(c.Tempo, base.WithAnchors(anchors))
	return nil
}

// retime moves every time in the chart from old to nw.
func (c *Chart) retime(old, nw *TempoMap) {
	for _, t := range c.Tracks {
		for i := range t.Notes {
			n := &t.Notes[i]
			n.Time = nw.Seconds(n.Tick)
			for l := range n.End {
				if n.EndTick[l] > 0 {
					n.End[l] = nw.Seconds(n.EndTick[l])
				}
			}
		}
		for _, list := range [][]Phrase{t.StarPower, t.Solos, t.Fills, t.Rolls} {
			for i := range list {
				list[i].Start = nw.Seconds(list[i].StartTick)
				list[i].End = nw.Seconds(list[i].EndTick)
			}
		}
	}
	for i := range c.Sections {
		c.Sections[i].Time = nw.Seconds(c.Sections[i].Tick)
	}
	for i := range c.Events {
		c.Events[i].Time = nw.Seconds(c.Events[i].Tick)
	}
	conv := func(sec float64) float64 { return nw.SecondsF(old.Ticks(sec)) }
	for vi := range c.Vocals {
		for pi := range c.Vocals[vi].Phrases {
			ph := &c.Vocals[vi].Phrases[pi]
			ph.Start, ph.End = conv(ph.Start), conv(ph.End)
			for si := range ph.Syllables {
				ph.Syllables[si].Time = conv(ph.Syllables[si].Time)
			}
		}
	}
	if len(c.Vocals) > 0 {
		c.Lyrics = c.Vocals[0].Phrases
	}
	c.Tempo = nw
}
