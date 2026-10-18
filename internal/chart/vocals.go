package chart

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// VocalPart is one sung line of a song: the lead, a harmony or a backing part.
// Parts are shown at the same time, each sweeping on its own timing.
type VocalPart struct {
	Name    string // "Lead", "Harmony 2", "Backing", ...
	Lead    bool   // the main vocal line
	Phrases []LyricPhrase
}

// setVocals stores parts (dropping empty ones) and points Lyrics at the lead.
func (c *Chart) setVocals(parts []VocalPart) {
	c.Vocals = c.Vocals[:0]
	for _, p := range parts {
		if len(p.Phrases) > 0 {
			c.Vocals = append(c.Vocals, p)
		}
	}
	// The lead goes first; a chart without a marked lead shows its first part as the lead.
	sort.SliceStable(c.Vocals, func(i, j int) bool { return c.Vocals[i].Lead && !c.Vocals[j].Lead })
	c.Lyrics = nil
	if len(c.Vocals) > 0 {
		c.Lyrics = c.Vocals[0].Phrases
	}
}

func leadPart(phrases []LyricPhrase) []VocalPart {
	return []VocalPart{{Name: "Lead", Lead: true, Phrases: phrases}}
}

// midiVocalParts builds parts from PART VOCALS and HARM1-3. Rock Band's HARM1
// is usually the lead melody again, so it is dropped when it says the same as
// PART VOCALS; without PART VOCALS, HARM1 is the lead.
func midiVocalParts(tm *TempoMap, tracks map[string]smfTrack) []VocalPart {
	var parts []VocalPart
	var lead []LyricPhrase
	if t, ok := tracks["PART VOCALS"]; ok {
		lead, _ = vocalPhrases(tm, t, nil, false)
		parts = append(parts, VocalPart{Name: "Lead", Lead: true, Phrases: lead})
	}
	var prev []span
	for i, name := range []string{"HARM1", "HARM2", "HARM3"} {
		t, ok := tracks[name]
		if !ok {
			continue
		}
		ph, spans := vocalPhrases(tm, t, prev, i > 0)
		prev = spans
		if i == 0 {
			if len(lead) > 0 && sameWords(lead, ph) {
				continue
			}
			if len(lead) == 0 {
				parts = append(parts, VocalPart{Name: "Lead", Lead: true, Phrases: ph})
				continue
			}
		}
		parts = append(parts, VocalPart{Name: fmt.Sprintf("Harmony %d", i+1), Phrases: ph})
	}
	return parts
}

// sameWords reports whether two parts sing mostly the same syllables.
func sameWords(a, b []LyricPhrase) bool {
	words := func(ps []LyricPhrase) map[string]int {
		m := map[string]int{}
		for _, p := range ps {
			for _, s := range p.Syllables {
				m[strings.ToLower(s.Text)]++
			}
		}
		return m
	}
	wa, wb := words(a), words(b)
	same, total := 0, 0
	for k, n := range wa {
		total += n
		same += min(n, wb[k])
	}
	return total > 0 && float64(same) >= 0.8*float64(total)
}

// ProjectFile is the name of a jam project file next to a chart. When it has
// vocals, they replace the chart's lyrics: the project format can carry any
// number of vocal parts, which .chart can't.
const ProjectFile = "chart.jam.json"

// jamProject is the part of a jam-project/1 file the game reads.
type jamProject struct {
	Format string `json:"format"`
	Grid   struct {
		Resolution int `json:"resolution"`
		Tempos     []struct {
			Tick int64   `json:"tick"`
			BPM  float64 `json:"bpm"`
		} `json:"tempos"`
		TimeSignatures []struct {
			Tick int64 `json:"tick"`
			Num  int   `json:"num"`
			Den  int   `json:"den"`
		} `json:"time_signatures"`
		Offset float64 `json:"offset"`
	} `json:"grid"`
	Vocals *struct {
		Parts   []jamVocalPart `json:"parts"`
		Phrases []jamPhrase    `json:"phrases"` // older files: one lead part
	} `json:"vocals"`
}

type jamVocalPart struct {
	Name    string      `json:"name"`
	Lead    bool        `json:"lead"`
	Phrases []jamPhrase `json:"phrases"`
}

type jamPhrase struct {
	Start     int64 `json:"start"`
	End       int64 `json:"end"`
	Syllables []struct {
		T    int64  `json:"t"`
		Text string `json:"text"`
		Join bool   `json:"join"`
	} `json:"syllables"`
}

// loadProjectVocals reads the vocal parts of a jam project file. ini supplies
// the same delay the chart gets, so both share one timeline.
func loadProjectVocals(path string, ini *SongIni) ([]VocalPart, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var jp jamProject
	if err := json.Unmarshal(b, &jp); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if !strings.HasPrefix(jp.Format, "jam-project/") {
		return nil, fmt.Errorf("%s: not a jam project (format %q)", filepath.Base(path), jp.Format)
	}
	if jp.Vocals == nil || jp.Grid.Resolution <= 0 || len(jp.Grid.Tempos) == 0 {
		return nil, nil
	}
	var tempos []Tempo
	for _, t := range jp.Grid.Tempos {
		tempos = append(tempos, Tempo{Tick: t.Tick, BPM: t.BPM})
	}
	var sigs []TimeSig
	for _, s := range jp.Grid.TimeSignatures {
		sigs = append(sigs, TimeSig{Tick: s.Tick, Num: s.Num, Den: max(1, s.Den)})
	}
	offset := jp.Grid.Offset
	if d, ok := ini.Int("delay"); ok {
		offset += float64(d) / 1000
	}
	tm := NewTempoMap(jp.Grid.Resolution, tempos, sigs, offset)
	raw := jp.Vocals.Parts
	if len(raw) == 0 && len(jp.Vocals.Phrases) > 0 {
		raw = []jamVocalPart{{Name: "Lead", Lead: true, Phrases: jp.Vocals.Phrases}}
	}
	var parts []VocalPart
	for i, rp := range raw {
		vp := VocalPart{Name: rp.Name, Lead: rp.Lead}
		if vp.Name == "" {
			vp.Name = fmt.Sprintf("Part %d", i+1)
		}
		for _, p := range rp.Phrases {
			lp := LyricPhrase{Start: tm.Seconds(p.Start), End: tm.Seconds(p.End)}
			for _, s := range p.Syllables {
				text, join, ok := cleanSyllable(s.Text)
				if !ok {
					continue
				}
				lp.Syllables = append(lp.Syllables, Syllable{Time: tm.Seconds(s.T), Text: text, Join: join || s.Join})
			}
			if len(lp.Syllables) > 0 {
				vp.Phrases = append(vp.Phrases, lp)
			}
		}
		sort.SliceStable(vp.Phrases, func(a, b int) bool { return vp.Phrases[a].Start < vp.Phrases[b].Start })
		parts = append(parts, vp)
	}
	return parts, nil
}

// applyProjectVocals swaps in the vocal parts from a project file next to the
// chart, when there is one with vocals. A broken project file is reported
// and the chart's own lyrics are kept.
func applyProjectVocals(c *Chart, dir string, ini *SongIni) error {
	path := filepath.Join(dir, ProjectFile)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	parts, err := loadProjectVocals(path, ini)
	if err != nil {
		return err
	}
	if len(parts) > 0 {
		c.setVocals(parts)
	}
	return nil
}
