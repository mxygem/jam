package chart

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

const sampleChart = "\xef\xbb\xbf" + `[Song]
{
  Name = "Test Song"
  Artist = "Test Artist"
  Resolution = 192
  Offset = 0
}
[SyncTrack]
{
  0 = TS 4
  0 = B 120000
  768 = B 60000
  1536 = TS 3 3
}
[Events]
{
  0 = E "section Intro"
  768 = E "section_Verse 1"
  1536 = E "[prc_chorus]"
}
[ExpertSingle]
{
  0 = N 0 0
  64 = N 1 0
  128 = N 1 0
  192 = N 0 0
  192 = N 2 0
  256 = N 3 0
  256 = N 5 0
  384 = N 4 96
  384 = S 2 200
  448 = N 2 0
  448 = N 6 0
  768 = N 7 0
  832 = N 0 0
  1000 = N 1 0
  1000 = N 5 0
}
[ExpertDrums]
{
  0 = N 0 0
  0 = N 2 0
  0 = N 66 0
  96 = N 3 0
  96 = N 32 0
  192 = N 4 0
  192 = N 68 0
  192 = N 34 0
  288 = N 1 0
  288 = N 40 0
  288 = S 64 0
}
`

func mustChart(t *testing.T, src string, ini *SongIni) *Chart {
	t.Helper()
	c, err := ParseDotChart(strings.NewReader(src), ini)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestTempoMapTimes(t *testing.T) {
	c := mustChart(t, sampleChart, nil)
	tm := c.Tempo
	cases := map[int64]float64{0: 0, 192: 0.5, 768: 2.0, 960: 3.0, 1536: 6.0}
	for tick, want := range cases {
		if got := tm.Seconds(tick); !near(got, want) {
			t.Errorf("Seconds(%d) = %v, want %v", tick, got, want)
		}
		if got := tm.Ticks(want); math.Abs(got-float64(tick)) > 1e-6 {
			t.Errorf("Ticks(%v) = %v, want %d", want, got, tick)
		}
	}
	if s := tm.SigAt(1600); s.Num != 3 || s.Den != 8 {
		t.Errorf("sig = %+v", s)
	}
	if b := tm.Beats(0, 2.0); !near(b, 4) {
		t.Errorf("beats 0..2s = %v", b)
	}
}

func TestDotChartGuitar(t *testing.T) {
	c := mustChart(t, sampleChart, nil)
	tr := c.Track(Guitar, Expert)
	if tr == nil {
		t.Fatal("no expert guitar")
	}
	type want struct {
		tick  int64
		lanes string
		kind  NoteKind
	}
	wants := []want{
		{0, "G----", Strum},
		{64, "-R---", HOPO},   // close, different fret
		{128, "-R---", Strum}, // same fret as previous: never natural HOPO
		{192, "G-Y--", Strum}, // chord
		{256, "---B-", Strum}, // natural HOPO flipped to strum by N 5
		{384, "----O", Strum}, // 128 ticks away > 65 threshold
		{448, "--Y--", Tap},
		{768, "open", Strum},
		{832, "G----", HOPO}, // open -> green within threshold
		{1000, "-R---", HOPO}, // far, but forced flip
	}
	if len(tr.Notes) != len(wants) {
		t.Fatalf("got %d notes, want %d", len(tr.Notes), len(wants))
	}
	for i, w := range wants {
		n := tr.Notes[i]
		if n.Tick != w.tick || n.LaneString() != w.lanes || n.Kind != w.kind {
			t.Errorf("note %d: got tick=%d lanes=%s kind=%s, want tick=%d lanes=%s kind=%s",
				i, n.Tick, n.LaneString(), n.Kind, w.tick, w.lanes, w.kind)
		}
	}
	sus := tr.Notes[5]
	if sus.EndTick[LaneOrange] != 480 || !near(sus.End[LaneOrange], 1.25) {
		t.Errorf("sustain end tick=%d time=%v", sus.EndTick[LaneOrange], sus.End[LaneOrange])
	}
	if !sus.StarPower || !tr.Notes[6].StarPower || !tr.Notes[6].SPEnd || tr.Notes[7].StarPower {
		t.Errorf("star power flags wrong: %v %v %v %v", sus.StarPower, tr.Notes[6].StarPower, tr.Notes[6].SPEnd, tr.Notes[7].StarPower)
	}
	if got := []string{c.Sections[0].Name, c.Sections[1].Name, c.Sections[2].Name}; got[0] != "Intro" || got[1] != "Verse 1" || got[2] != "Chorus" {
		t.Errorf("sections = %v", got)
	}
}

func TestDotChartDrums(t *testing.T) {
	c := mustChart(t, sampleChart, nil)
	tr := c.Track(Drums, Expert)
	if tr == nil {
		t.Fatal("no drums")
	}
	if tr.DrumMode != DrumsPro {
		t.Fatalf("mode = %s", tr.DrumMode)
	}
	n0, n1, n2, n3 := tr.Notes[0], tr.Notes[1], tr.Notes[2], tr.Notes[3]
	if n0.Lanes != 1<<DrumKick|1<<DrumYellow || n0.Cymbals != 1<<DrumYellow {
		t.Errorf("n0 lanes=%b cym=%b", n0.Lanes, n0.Cymbals)
	}
	if n1.Lanes != 1<<DrumKick|1<<DrumBlue || !n1.DoubleKick || n1.Cymbals != 0 {
		t.Errorf("n1 lanes=%b 2x=%v cym=%b", n1.Lanes, n1.DoubleKick, n1.Cymbals)
	}
	if n2.Cymbals != 1<<DrumGreen4 || n2.Accents != 0 {
		t.Errorf("n2 cym=%b acc=%b", n2.Cymbals, n2.Accents)
	}
	if n3.Ghosts != 1<<DrumRed || !n3.Activation {
		t.Errorf("n3 ghost=%b activation=%v", n3.Ghosts, n3.Activation)
	}
}

func TestSongIniOverrides(t *testing.T) {
	ini, _ := ParseSongIni(strings.NewReader("[song]\nname = X\ndelay = 250\nhopo_frequency = 30\nfive_lane_drums = True\n"))
	c := mustChart(t, sampleChart, ini)
	if got := c.Tempo.Seconds(0); !near(got, 0.25) {
		t.Errorf("delay not applied: %v", got)
	}
	tr := c.Track(Guitar, Expert)
	if tr.Notes[1].Kind != Strum { // 64 ticks > 30
		t.Errorf("hopo_frequency not applied: %s", tr.Notes[1].Kind)
	}
	if c.Track(Drums, Expert).DrumMode != DrumsFiveLane {
		t.Errorf("five_lane_drums override not applied")
	}
}

func TestBeatLines(t *testing.T) {
	c := mustChart(t, sampleChart, nil)
	lines := c.Tempo.BeatLines(nil, 0, 2.0)
	// 120 BPM 4/4: beats at 0, .5, 1, 1.5, 2 (2.0 is the next measure at 60bpm)
	if len(lines) != 5 || lines[0].Kind != BeatMeasure || lines[1].Kind != BeatStrong || lines[4].Kind != BeatMeasure {
		t.Fatalf("lines = %+v", lines)
	}
}

// --- MIDI ---

type midiEv struct {
	tick int64
	data []byte
}

func vlq(v int64) []byte {
	out := []byte{byte(v & 0x7F)}
	for v >>= 7; v > 0; v >>= 7 {
		out = append([]byte{byte(v&0x7F) | 0x80}, out...)
	}
	return out
}

func buildTrack(name string, evs []midiEv) []byte {
	var body bytes.Buffer
	if name != "" {
		body.Write([]byte{0x00, 0xFF, 0x03})
		body.Write(vlq(int64(len(name))))
		body.WriteString(name)
	}
	var last int64
	for _, e := range evs {
		body.Write(vlq(e.tick - last))
		last = e.tick
		body.Write(e.data)
	}
	body.Write([]byte{0x00, 0xFF, 0x2F, 0x00})
	var out bytes.Buffer
	out.WriteString("MTrk")
	binary.Write(&out, binary.BigEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func note(tick, length int64, n byte) []midiEv {
	return []midiEv{{tick, []byte{0x90, n, 100}}, {tick + length, []byte{0x80, n, 0}}}
}

func sortEvs(evs []midiEv) []midiEv {
	// insertion sort by tick, stable
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0 && evs[j].tick < evs[j-1].tick; j-- {
			evs[j], evs[j-1] = evs[j-1], evs[j]
		}
	}
	return evs
}

func TestMidiGuitarAndDrums(t *testing.T) {
	const res = 480
	tempo := buildTrack("tempo", []midiEv{
		{0, []byte{0xFF, 0x51, 0x03, 0x07, 0xA1, 0x20}}, // 500000us = 120bpm
		{0, []byte{0xFF, 0x58, 0x04, 0x04, 0x02, 0x18, 0x08}},
	})
	var g []midiEv
	g = append(g, note(0, 60, 96)...)    // green, short (cut off)
	g = append(g, note(120, 60, 97)...)  // red, natural HOPO (120 <= 161)
	g = append(g, note(480, 960, 98)...) // yellow sustain
	g = append(g, note(480, 960, 116)...)
	g = append(g, note(600, 10, 99)...)  // blue
	g = append(g, note(600, 10, 102)...) // force strum (was natural HOPO)
	sys := []byte{0xF0, 0x08, 0x50, 0x53, 0x00, 0x00, 0x03, 0x04, 0x01, 0xF7}
	sysOff := []byte{0xF0, 0x08, 0x50, 0x53, 0x00, 0x00, 0x03, 0x04, 0x00, 0xF7}
	g = append(g, midiEv{1440, sys})
	g = append(g, note(1440, 10, 100)...) // tap via sysex
	g = append(g, midiEv{1450, sysOff})
	guitar := buildTrack("PART GUITAR", sortEvs(g))

	var d []midiEv
	d = append(d, note(0, 10, 96)...)  // kick
	d = append(d, note(0, 10, 98)...)  // yellow (cymbal by default)
	d = append(d, note(240, 10, 99)...) // blue
	d = append(d, note(240, 10, 111)...) // blue tom marker
	drums := buildTrack("PART DRUMS", sortEvs(d))

	var f bytes.Buffer
	f.WriteString("MThd")
	binary.Write(&f, binary.BigEndian, uint32(6))
	binary.Write(&f, binary.BigEndian, uint16(1))
	binary.Write(&f, binary.BigEndian, uint16(3))
	binary.Write(&f, binary.BigEndian, uint16(res))
	f.Write(tempo)
	f.Write(guitar)
	f.Write(drums)

	c, err := ParseMidiChart(f.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	gt := c.Track(Guitar, Expert)
	if gt == nil || len(gt.Notes) != 5 {
		t.Fatalf("guitar notes: %+v", gt)
	}
	if gt.Notes[0].HasSustain() {
		t.Errorf("short sustain not cut")
	}
	if gt.Notes[1].Kind != HOPO {
		t.Errorf("note1 kind %s", gt.Notes[1].Kind)
	}
	if !gt.Notes[2].HasSustain() || !near(gt.Notes[2].End[LaneYellow], 1.5) || !gt.Notes[2].StarPower {
		t.Errorf("note2 sustain=%v end=%v sp=%v", gt.Notes[2].HasSustain(), gt.Notes[2].End[LaneYellow], gt.Notes[2].StarPower)
	}
	if gt.Notes[3].Kind != Strum {
		t.Errorf("note3 kind %s (force strum)", gt.Notes[3].Kind)
	}
	if gt.Notes[4].Kind != Tap {
		t.Errorf("note4 kind %s (sysex tap)", gt.Notes[4].Kind)
	}
	dt := c.Track(Drums, Expert)
	if dt == nil || dt.DrumMode != DrumsPro {
		t.Fatalf("drums: %+v", dt)
	}
	if dt.Notes[0].Cymbals != 1<<DrumYellow || dt.Notes[1].Cymbals != 0 {
		t.Errorf("cymbals: %b %b", dt.Notes[0].Cymbals, dt.Notes[1].Cymbals)
	}
}
