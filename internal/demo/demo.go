// Package demo writes a small built-in song (chart + synthesized stems) so
// the game is playable before any library is configured.
package demo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mxygem/jam/internal/playback"
)

const (
	res  = 192
	bpm  = 132.0
	beat = res
	e8   = res / 2
	s16  = res / 4
	bar  = res * 4
	bars = 32
)

func secs(tick int) float64 { return float64(tick) / res * 60 / bpm }

type note struct {
	tick   int
	lanes  []int // guitar lanes 0-4, 7 = open; drums 0-4
	length int
	tap    bool
	cym    []int // drums cymbal lanes (2,3,4)
}

type part struct {
	notes []note
	sp    [][2]int
}

func (p *part) add(n note) { p.notes = append(p.notes, n) }

func guitarExpert() part {
	var p part
	riff := [][]int{{0}, {1}, {2}, {1}, {0}, {1}, {2}, {3}}
	// Intro: 3 bars of riff, 1 bar chord.
	for b := 0; b < 3; b++ {
		for i, l := range riff {
			p.add(note{tick: b*bar + i*e8, lanes: l})
		}
	}
	p.add(note{tick: 3 * bar, lanes: []int{0, 2}, length: bar - e8})
	// Verse: riffs with 16th pull-offs and stabs.
	for b := 4; b < 12; b++ {
		base := b * bar
		if b%2 == 0 {
			seq := [][]int{{2}, {1}, {0}, {1}, {2}, {3}, {2}, {1}}
			for i, l := range seq {
				p.add(note{tick: base + i*e8, lanes: l})
			}
		} else {
			// strum + 16th pull-offs (natural HOPOs: 48 ticks apart, different frets)
			p.add(note{tick: base, lanes: []int{3}})
			p.add(note{tick: base + s16, lanes: []int{2}})
			p.add(note{tick: base + 2*s16, lanes: []int{1}})
			p.add(note{tick: base + beat, lanes: []int{0}})
			p.add(note{tick: base + 2*beat, lanes: []int{0, 1}, length: beat - s16})
			p.add(note{tick: base + 3*beat, lanes: []int{1, 2}, length: e8})
		}
	}
	// Chorus: sustained power chords with star power.
	shapes := [][]int{{0, 1}, {1, 2}, {2, 3}, {1, 2}}
	for b := 12; b < 20; b++ {
		base := b * bar
		sh := shapes[(b-12)%4]
		p.add(note{tick: base, lanes: sh, length: beat + e8})
		p.add(note{tick: base + 2*beat, lanes: sh})
		p.add(note{tick: base + 2*beat + e8, lanes: sh})
		p.add(note{tick: base + 3*beat, lanes: []int{sh[1] + 1}, length: e8})
	}
	p.sp = append(p.sp, [2]int{12 * bar, 2 * bar}, [2]int{16 * bar, 2 * bar})
	// Bridge: HOPO runs, opens, taps.
	run := []int{0, 1, 2, 3, 4, 3, 2, 1}
	for b := 20; b < 22; b++ {
		for i := 0; i < 16; i++ {
			lane := run[i%8]
			if b == 21 && i%4 == 0 {
				p.add(note{tick: b*bar + i*s16, lanes: []int{7}})
				continue
			}
			p.add(note{tick: b*bar + i*s16, lanes: []int{lane}})
		}
	}
	taps := []int{4, 2, 3, 1, 4, 2, 3, 0}
	for b := 22; b < 24; b++ {
		for i := 0; i < 16; i++ {
			p.add(note{tick: b*bar + i*s16, lanes: []int{taps[i%8]}, tap: true})
		}
	}
	// Outro.
	for b := 24; b < 30; b++ {
		base := b * bar
		p.add(note{tick: base, lanes: []int{0, 2}, length: e8})
		for i := 2; i < 8; i++ {
			p.add(note{tick: base + i*e8, lanes: riff[(i+b)%8]})
		}
	}
	p.sp = append(p.sp, [2]int{26 * bar, 2 * bar})
	p.add(note{tick: 30 * bar, lanes: []int{0, 1, 2}, length: 2*bar - beat})
	return p
}

// reduce derives an easier difficulty from expert.
func reduce(p part, grid int, maxFret int, maxChord int) part {
	var out part
	out.sp = p.sp
	last := -1 << 30
	for _, n := range p.notes {
		if n.tick%grid != 0 || n.tick-last < grid {
			continue
		}
		m := note{tick: n.tick, length: n.length}
		seen := map[int]bool{}
		for _, l := range n.lanes {
			if l != 7 && l > maxFret {
				l = maxFret
			}
			if !seen[l] {
				seen[l] = true
				m.lanes = append(m.lanes, l)
			}
		}
		if len(m.lanes) > maxChord {
			m.lanes = m.lanes[:maxChord]
		}
		out.notes = append(out.notes, m)
		last = n.tick
	}
	return out
}

func bassExpert() part {
	var p part
	roots := []int{0, 0, 1, 2} // per bar root fret
	for b := 0; b < 30; b++ {
		r := roots[b%4]
		for i := 0; i < 8; i++ {
			l := r
			if i == 6 {
				l = r + 1
			}
			length := 0
			if b >= 12 && b < 20 && i%4 == 0 {
				length = e8 + s16
			}
			p.add(note{tick: b*bar + i*e8, lanes: []int{l}, length: length})
		}
	}
	p.add(note{tick: 30 * bar, lanes: []int{0}, length: 2*bar - beat})
	p.sp = [][2]int{{8 * bar, 2 * bar}, {16 * bar, 2 * bar}, {24 * bar, 2 * bar}}
	return p
}

func drumsExpert() part {
	var p part
	for b := 0; b < 30; b++ {
		base := b * bar
		fill := b%4 == 3 && b > 0
		for i := 0; i < 8; i++ {
			t := base + i*e8
			var lanes, cym []int
			if i == 0 || i == 4 || i == 5 {
				lanes = append(lanes, 0)
			}
			if i == 2 || i == 6 {
				lanes = append(lanes, 1)
			}
			if fill && i >= 4 {
				// tom fill
				lanes = []int{2 + (i-4)%3}
				if i == 4 {
					lanes = append(lanes, 0)
				}
				p.add(note{tick: t, lanes: lanes})
				continue
			}
			if b%4 == 0 && i == 0 {
				lanes = append(lanes, 4)
				cym = append(cym, 4)
			} else if b >= 12 && b < 20 {
				lanes = append(lanes, 3)
				cym = append(cym, 3) // ride in the chorus
			} else {
				lanes = append(lanes, 2)
				cym = append(cym, 2)
			}
			p.add(note{tick: t, lanes: lanes, cym: cym})
		}
	}
	p.add(note{tick: 30 * bar, lanes: []int{0, 4}, cym: []int{4}})
	p.sp = [][2]int{{12 * bar, 2 * bar}, {20 * bar, 2 * bar}}
	return p
}

func writeTrack(b *strings.Builder, name string, p part, drums bool) {
	fmt.Fprintf(b, "[%s]\n{\n", name)
	type line struct {
		tick int
		s    string
	}
	var lines []line
	for _, n := range p.notes {
		for _, l := range n.lanes {
			lines = append(lines, line{n.tick, fmt.Sprintf("N %d %d", l, n.length)})
		}
		if n.tap {
			lines = append(lines, line{n.tick, "N 6 0"})
		}
		for _, c := range n.cym {
			lines = append(lines, line{n.tick, fmt.Sprintf("N %d 0", 64+c)})
		}
	}
	for _, s := range p.sp {
		lines = append(lines, line{s[0], fmt.Sprintf("S 2 %d", s[1])})
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].tick < lines[j].tick })
	for _, l := range lines {
		fmt.Fprintf(b, "  %d = %s\n", l.tick, l.s)
	}
	b.WriteString("}\n")
}

func chartText() string {
	var b strings.Builder
	b.WriteString("[Song]\n{\n  Name = \"Highway Test\"\n  Artist = \"jam\"\n  Charter = \"jam demo\"\n  Resolution = 192\n  Offset = 0\n}\n")
	fmt.Fprintf(&b, "[SyncTrack]\n{\n  0 = TS 4\n  0 = B %d\n}\n", int(bpm*1000))
	b.WriteString("[Events]\n{\n")
	for _, s := range []struct {
		bar  int
		name string
	}{{0, "Intro"}, {4, "Verse"}, {12, "Chorus"}, {20, "Bridge"}, {22, "Tapping"}, {24, "Outro"}} {
		fmt.Fprintf(&b, "  %d = E \"section %s\"\n", s.bar*bar, s.name)
	}
	b.WriteString("}\n")
	g := guitarExpert()
	writeTrack(&b, "ExpertSingle", g, false)
	writeTrack(&b, "HardSingle", reduce(g, e8, 4, 2), false)
	writeTrack(&b, "MediumSingle", reduce(g, beat, 3, 2), false)
	writeTrack(&b, "EasySingle", reduce(g, 2*beat, 2, 1), false)
	bs := bassExpert()
	writeTrack(&b, "ExpertDoubleBass", bs, false)
	writeTrack(&b, "HardDoubleBass", reduce(bs, e8, 4, 1), false)
	writeTrack(&b, "MediumDoubleBass", reduce(bs, beat, 3, 1), false)
	writeTrack(&b, "EasyDoubleBass", reduce(bs, 2*beat, 2, 1), false)
	d := drumsExpert()
	writeTrack(&b, "ExpertDrums", d, true)
	writeTrack(&b, "HardDrums", reduce(d, e8, 4, 2), true)
	writeTrack(&b, "MediumDrums", reduce(d, beat, 4, 2), true)
	writeTrack(&b, "EasyDrums", reduce(d, 2*beat, 4, 1), true)
	return b.String()
}

var guitarHz = []float64{164.81, 196.0, 220.0, 246.94, 293.66}

func render() (guitar, bass, drums, song playback.PCM) {
	total := secs(bars*bar) + 1.5
	guitar, bass, drums, song = playback.NewPCM(total), playback.NewPCM(total), playback.NewPCM(total), playback.NewPCM(total)
	for _, n := range guitarExpert().notes {
		dur := secs(max(n.length, e8)) + 0.05
		for _, l := range n.lanes {
			f := 82.41
			if l < 5 {
				f = guitarHz[l]
			}
			guitar.Add(secs(n.tick), playback.Tone(f, dur, 0), 0.5)
		}
	}
	for _, n := range bassExpert().notes {
		dur := secs(max(n.length, e8)) + 0.02
		for _, l := range n.lanes {
			bass.Add(secs(n.tick), playback.Tone(guitarHz[l]/4, dur, 1), 0.8)
		}
	}
	kick, snare, hat, ride, crash := playback.Kick(), playback.Snare(), playback.HiHat(false), playback.HiHat(true), playback.Crash()
	for _, n := range drumsExpert().notes {
		cym := map[int]bool{}
		for _, c := range n.cym {
			cym[c] = true
		}
		for _, l := range n.lanes {
			t := secs(n.tick)
			switch {
			case l == 0:
				drums.Add(t, kick, 1)
			case l == 1:
				drums.Add(t, snare, 1)
			case l == 2 && cym[2]:
				drums.Add(t, hat, 1)
			case l == 3 && cym[3]:
				drums.Add(t, ride, 0.6)
			case l == 4 && cym[4]:
				drums.Add(t, crash, 1)
			default:
				drums.Add(t, playback.TomVoice(220-float64(l)*35), 1)
			}
		}
	}
	chords := [][]float64{{82.41, 123.47, 164.81}, {65.41, 98.0, 130.81}, {73.42, 110.0, 146.83}, {82.41, 123.47, 164.81}}
	for b := 0; b < bars; b++ {
		for _, f := range chords[b%4] {
			song.Add(secs(b*bar), playback.Tone(f*2, secs(bar)+0.1, 2), 0.18)
		}
	}
	return
}

// Write creates the demo song folder in dir (skipped if already present).
func Write(dir string) (string, error) {
	folder := filepath.Join(dir, "jam - Highway Test")
	if _, err := os.Stat(filepath.Join(folder, "notes.chart")); err == nil {
		return folder, nil
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	g, b, d, s := render()
	files := map[string][]byte{
		"notes.chart": []byte(chartText()),
		"song.ini":    []byte("[song]\nname = Highway Test\nartist = jam\ncharter = jam demo\ngenre = Test\nyear = 2026\ndiff_guitar = 3\ndiff_bass = 2\ndiff_drums = 3\npro_drums = True\n"),
		"guitar.wav":  g.WAV(),
		"bass.wav":    b.WAV(),
		"drums.wav":   d.WAV(),
		"song.wav":    s.WAV(),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(folder, name), data, 0o644); err != nil {
			return "", err
		}
	}
	return folder, nil
}
