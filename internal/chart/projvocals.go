package chart

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The types below are the project file's own vocal and grid data. The
// editor edits them directly, so effect tags and anything else it doesn't
// understand survive a save unchanged.

// ProjSyllable is one syllable in a project file. T is in ticks of the grid.
type ProjSyllable struct {
	T    int64    `json:"t"`
	Text string   `json:"text"`
	Join bool     `json:"join,omitempty"`
	FX   []string `json:"fx,omitempty"`
}

// ProjPhrase is one line. Start and End are ticks of the grid.
type ProjPhrase struct {
	Start     int64          `json:"start"`
	End       int64          `json:"end"`
	FX        []string       `json:"fx,omitempty"`
	Syllables []ProjSyllable `json:"syllables"`
}

// ProjVocalPart is one vocal part (lead, harmony or backing).
type ProjVocalPart struct {
	Name    string       `json:"name"`
	Lead    bool         `json:"lead"`
	FX      []string     `json:"fx,omitempty"`
	Phrases []ProjPhrase `json:"phrases"`
}

// ProjTempo and ProjSig are tempo and time signature changes in a grid.
type ProjTempo struct {
	Tick int64   `json:"tick"`
	BPM  float64 `json:"bpm"`
}

type ProjSig struct {
	Tick int64 `json:"tick"`
	Num  int   `json:"num"`
	Den  int   `json:"den"`
}

// ProjGrid is the tempo map the project's ticks are measured on. Offset is
// the chart offset without the song.ini delay (the loader adds the delay).
type ProjGrid struct {
	Resolution     int         `json:"resolution"`
	Tempos         []ProjTempo `json:"tempos"`
	TimeSignatures []ProjSig   `json:"time_signatures,omitempty"`
	Offset         float64     `json:"offset,omitempty"`
}

// ProjectGridFor writes the chart's own tempo map as a project grid, so that
// the project's ticks line up with the chart's.
func (c *Chart) ProjectGridFor(ini *SongIni) ProjGrid {
	g := ProjGrid{Resolution: c.Resolution}
	if c.Tempo == nil {
		return g
	}
	g.Resolution = c.Tempo.Resolution
	for _, t := range c.Tempo.Tempos {
		g.Tempos = append(g.Tempos, ProjTempo{Tick: t.Tick, BPM: t.BPM})
	}
	for _, s := range c.Tempo.Sigs {
		g.TimeSignatures = append(g.TimeSignatures, ProjSig{Tick: s.Tick, Num: s.Num, Den: s.Den})
	}
	g.Offset = c.Tempo.Offset
	if d, ok := ini.Int("delay"); ok {
		g.Offset -= float64(d) / 1000
	}
	return g
}

// tempoMap builds the tempo map a grid describes, with the song's delay.
func (g ProjGrid) tempoMap(ini *SongIni) *TempoMap {
	var tempos []Tempo
	for _, t := range g.Tempos {
		tempos = append(tempos, Tempo{Tick: t.Tick, BPM: t.BPM})
	}
	var sigs []TimeSig
	for _, s := range g.TimeSignatures {
		sigs = append(sigs, TimeSig{Tick: s.Tick, Num: s.Num, Den: max(1, s.Den)})
	}
	offset := g.Offset
	if d, ok := ini.Int("delay"); ok {
		offset += float64(d) / 1000
	}
	return NewTempoMap(g.Resolution, tempos, sigs, offset)
}

// ProjectVocals is what the project file holds about vocals.
type ProjectVocals struct {
	Parts   []ProjVocalPart
	HasGrid bool
}

// LoadProjectVocals reads the vocal parts stored in dir's chart.jam.json,
// including the older single-part form. A missing file gives no parts.
func LoadProjectVocals(dir string) (ProjectVocals, error) {
	doc, err := readProjectDoc(dir)
	if err != nil {
		return ProjectVocals{}, err
	}
	var pv ProjectVocals
	_, pv.HasGrid = doc["grid"]
	raw, ok := doc["vocals"]
	if !ok {
		return pv, nil
	}
	var v struct {
		Parts   []ProjVocalPart `json:"parts"`
		Phrases []ProjPhrase    `json:"phrases"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return pv, fmt.Errorf("%s: vocals: %w", ProjectFile, err)
	}
	pv.Parts = v.Parts
	if len(pv.Parts) == 0 && len(v.Phrases) > 0 {
		pv.Parts = []ProjVocalPart{{Name: "Lead", Lead: true, Phrases: v.Phrases}}
	}
	return pv, nil
}

// SaveProjectVocals writes the vocal parts into dir's chart.jam.json, keeping
// the other keys of the vocals section and every other section. If grid is
// non-nil it is written too (the project's ticks need one to mean anything).
func SaveProjectVocals(dir string, parts []ProjVocalPart, grid *ProjGrid) error {
	doc, err := readProjectDoc(dir)
	if err != nil {
		return err
	}
	vocals := map[string]json.RawMessage{}
	if raw, ok := doc["vocals"]; ok {
		if err := json.Unmarshal(raw, &vocals); err != nil {
			return fmt.Errorf("%s: vocals: %w", ProjectFile, err)
		}
	}
	// The older single-phrase form would shadow the parts; drop it.
	delete(vocals, "phrases")
	if parts == nil {
		parts = []ProjVocalPart{}
	}
	pb, err := json.Marshal(parts)
	if err != nil {
		return err
	}
	vocals["parts"] = pb
	vb, err := json.Marshal(vocals)
	if err != nil {
		return err
	}
	doc["vocals"] = vb
	if grid != nil {
		gb, err := json.Marshal(grid)
		if err != nil {
			return err
		}
		doc["grid"] = gb
	}
	return writeProjectDoc(dir, doc)
}

// readProjectDoc reads chart.jam.json as a set of top-level sections. A
// missing file is an empty document. A file that isn't a jam project is an
// error, so that a save never overwrites something else.
func readProjectDoc(dir string) (map[string]json.RawMessage, error) {
	doc := map[string]json.RawMessage{}
	b, err := os.ReadFile(filepath.Join(dir, ProjectFile))
	if errors.Is(err, fs.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", ProjectFile, err)
	}
	raw, ok := doc["format"]
	if !ok && len(doc) > 0 {
		return nil, fmt.Errorf("%s is not a jam project (no format)", ProjectFile)
	}
	if ok {
		var f string
		if err := json.Unmarshal(raw, &f); err != nil || !strings.HasPrefix(f, "jam-project/") {
			return nil, fmt.Errorf("%s is not a jam project (format %s)", ProjectFile, raw)
		}
	}
	return doc, nil
}

// writeProjectDoc writes the sections back, atomically, with the format key set.
func writeProjectDoc(dir string, doc map[string]json.RawMessage) error {
	if _, ok := doc["format"]; !ok {
		doc["format"] = json.RawMessage(`"` + ProjectFormat + `"`)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, ProjectFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
