package chart

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// LoadDotChart reads a .chart file. ini may be nil.
func LoadDotChart(path string, ini *SongIni) (*Chart, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDotChart(bytes.NewReader(b), ini)
}

type chartEvent struct {
	tick int64
	typ  string
	a, b int64
	text string
}

type dotChartFile struct {
	sections map[string][]chartEvent
	named    map[string]map[string]string
}

func readDotChart(r io.Reader) (*dotChartFile, error) {
	f := &dotChartFile{sections: map[string][]chartEvent{}, named: map[string]map[string]string{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 256*1024), 16*1024*1024)
	section := ""
	first := true
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\uFEFF")
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || line == "{" || line == "}" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := f.named[section]; !ok {
				f.named[section] = map[string]string{}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || section == "" {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		tick, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			f.named[section][strings.ToLower(k)] = unquote(v)
			continue
		}
		typ, rest, _ := strings.Cut(v, " ")
		ev := chartEvent{tick: tick, typ: strings.ToUpper(strings.TrimSpace(typ))}
		rest = strings.TrimSpace(rest)
		switch ev.typ {
		case "E":
			ev.text = unquote(rest)
		default:
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
					ev.a = n
				} else {
					return nil, fmt.Errorf("line %d: bad value %q", lineNo, line)
				}
			}
			if len(fields) > 1 {
				if n, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					ev.b = n
				}
			}
		}
		f.sections[section] = append(f.sections[section], ev)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return f, nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return strings.TrimPrefix(s, "\"")
}

var chartDifficulties = map[string]Difficulty{"Easy": Easy, "Medium": Medium, "Hard": Hard, "Expert": Expert}

var chartInstruments = map[string]Instrument{
	"Single":       Guitar,
	"DoubleGuitar": GuitarCoop,
	"DoubleBass":   Bass,
	"DoubleRhythm": Rhythm,
	"Keyboard":     Keys,
	"Drums":        Drums,
}

func parseChartTrackName(name string) (TrackKey, bool) {
	for dn, d := range chartDifficulties {
		if strings.HasPrefix(name, dn) {
			if inst, ok := chartInstruments[name[len(dn):]]; ok {
				return TrackKey{inst, d}, true
			}
		}
	}
	return TrackKey{}, false
}

// ParseDotChart parses .chart content into a Chart. ini may be nil.
func ParseDotChart(r io.Reader, ini *SongIni) (*Chart, error) {
	f, err := readDotChart(r)
	if err != nil {
		return nil, err
	}
	song := f.named["Song"]
	res := 192
	if v, ok := song["resolution"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			res = n
		}
	}
	offset := 0.0
	if v, ok := song["offset"]; ok {
		if x, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			offset = x
		}
	}
	if d, ok := ini.Int("delay"); ok {
		offset += float64(d) / 1000
	}

	var tempos []Tempo
	var sigs []TimeSig
	for _, ev := range f.sections["SyncTrack"] {
		switch ev.typ {
		case "B":
			tempos = append(tempos, Tempo{Tick: ev.tick, BPM: float64(ev.a) / 1000})
		case "TS":
			den := 4
			if ev.b > 0 {
				den = 1 << ev.b
			}
			sigs = append(sigs, TimeSig{Tick: ev.tick, Num: int(ev.a), Den: den})
		}
	}
	tm := NewTempoMap(res, tempos, sigs, offset)

	c := &Chart{Source: "chart", Resolution: res, Tempo: tm, Tracks: map[TrackKey]*Track{}, Meta: song}
	for _, ev := range f.sections["Events"] {
		if ev.typ != "E" {
			continue
		}
		c.Events = append(c.Events, TextEvent{Tick: ev.tick, Time: tm.Seconds(ev.tick), Text: ev.text})
		if name, ok := sectionName(ev.text); ok {
			c.Sections = append(c.Sections, Section{Tick: ev.tick, Time: tm.Seconds(ev.tick), Name: name})
		}
	}
	c.Lyrics = lyricsFromEvents(tm, c.Events)

	hopo := int64(65 * res / 192)
	if v, ok := ini.Int("hopo_frequency"); ok && v > 0 {
		hopo = v
	} else if v, ok := ini.Bool("eighthnote_hopo"); ok && v {
		hopo = int64(res / 2)
	}
	cutoff := int64(0)
	if v, ok := ini.Int("sustain_cutoff_threshold"); ok && v > 0 {
		cutoff = v
	}

	for name, events := range f.sections {
		key, ok := parseChartTrackName(name)
		if !ok {
			continue
		}
		rt := newRawTrack(key)
		var soloStart int64 = -1
		cymbals, fiveLane := false, false
		for _, ev := range events {
			switch ev.typ {
			case "N":
				if key.Inst.IsDrums() {
					switch {
					case ev.a >= 0 && ev.a <= 5:
						rt.notes = append(rt.notes, rawNote{tick: ev.tick, lane: int(ev.a), length: ev.b})
						if ev.a == 5 {
							fiveLane = true
						}
					case ev.a == 32:
						rt.notes = append(rt.notes, rawNote{tick: ev.tick, lane: DrumKick, twoX: true})
					case ev.a >= 34 && ev.a <= 38:
						rt.accentTick[ev.tick] |= 1 << (ev.a - 33)
					case ev.a >= 40 && ev.a <= 44:
						rt.ghostTick[ev.tick] |= 1 << (ev.a - 39)
					case ev.a >= 66 && ev.a <= 68:
						rt.cymbalTick[ev.tick] |= 1 << (ev.a - 64)
						cymbals = true
					}
					continue
				}
				switch ev.a {
				case 0, 1, 2, 3, 4:
					rt.notes = append(rt.notes, rawNote{tick: ev.tick, lane: int(ev.a), length: ev.b})
				case 7:
					rt.notes = append(rt.notes, rawNote{tick: ev.tick, lane: LaneOpen, length: ev.b})
				case 5:
					rt.flip[ev.tick] = true
				case 6:
					rt.tapTick[ev.tick] = true
				}
			case "S":
				s := span{ev.tick, ev.tick + ev.b}
				switch ev.a {
				case 2:
					rt.starPower = append(rt.starPower, s)
				case 64:
					rt.fills = append(rt.fills, s)
				case 65, 66:
					rt.rolls = append(rt.rolls, s)
				}
			case "E":
				switch strings.ToLower(strings.Trim(ev.text, "[] ")) {
				case "solo":
					soloStart = ev.tick
				case "soloend":
					if soloStart >= 0 {
						rt.solos = append(rt.solos, span{soloStart, ev.tick})
						soloStart = -1
					}
				}
			}
		}
		var t *Track
		if key.Inst.IsDrums() {
			t = buildDrums(rt, tm, buildOpts{drumMode: detectDrumMode(ini, cymbals, fiveLane)})
		} else {
			t = buildGuitar(rt, tm, buildOpts{hopoThreshold: hopo, sustainCutoff: cutoff})
		}
		if len(t.Notes) > 0 {
			c.Tracks[key] = t
		}
	}
	return c, nil
}

// sectionName extracts a practice section name from a global event.
func sectionName(text string) (string, bool) {
	t := strings.TrimSpace(text)
	t = strings.TrimPrefix(t, "[")
	t = strings.TrimSuffix(t, "]")
	t = strings.TrimSpace(t)
	lower := strings.ToLower(t)
	var name string
	switch {
	case strings.HasPrefix(lower, "section "), strings.HasPrefix(lower, "section_"):
		name = t[len("section "):]
	case strings.HasPrefix(lower, "prc_"):
		name = t[len("prc_"):]
	default:
		return "", false
	}
	name = strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
	if name == "" {
		return "", false
	}
	return prettySection(name), true
}

func prettySection(s string) string {
	// Capitalise the first letter; keep the rest as written.
	r := []rune(s)
	if len(r) > 0 && r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
