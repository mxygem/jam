package engine

import (
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
)

// 120 BPM, res 192: one beat (192 ticks) = 0.5s.
const guitarChart = `[Song]
{
  Resolution = 192
}
[SyncTrack]
{
  0 = TS 4
  0 = B 120000
}
[ExpertSingle]
{
  192 = N 0 0
  384 = N 1 0
  448 = N 2 0
  768 = N 0 0
  768 = N 1 0
  960 = N 3 384
  1536 = N 4 0
  1536 = S 2 400
  1728 = N 3 0
  1920 = N 2 0
}
[ExpertDrums]
{
  192 = N 0 0
  192 = N 2 0
  192 = N 66 0
  384 = N 1 0
  576 = N 2 0
}
`

func load(t *testing.T) (*chart.Chart, *chart.Track, *chart.Track) {
	t.Helper()
	c, err := chart.ParseDotChart(strings.NewReader(guitarChart), nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, c.Track(chart.Guitar, chart.Expert), c.Track(chart.Drums, chart.Expert)
}

type script []action.Event

func (s *script) fret(t float64, a action.Action, down bool) {
	*s = append(*s, action.Event{Time: t, Action: a, Down: down})
}
func (s *script) strum(t float64) {
	*s = append(*s, action.Event{Time: t, Action: action.StrumDown, Down: true}, action.Event{Time: t + 0.03, Action: action.StrumDown, Down: false})
}

func sortScript(s script) script {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Time < s[j-1].Time; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}

func run(e Engine, evs script, end float64) {
	for _, ev := range evs {
		e.Process(ev)
	}
	e.Advance(end)
}

func count(e Engine, r Result) int {
	n := 0
	for _, j := range e.Judgments() {
		if j.Result == r {
			n++
		}
	}
	return n
}

// perfect plays every note of the guitar chart.
func perfect(tr *chart.Track, offset float64) script {
	var s script
	held := uint8(0)
	frets := []action.Action{action.Green, action.Red, action.Yellow, action.Blue, action.Orange}
	for i, n := range tr.Notes {
		want := n.Lanes & chart.FretMask
		t := n.Time + offset
		pre := t - 0.04
		if i > 0 {
			// release previous frets just before
			prev := tr.Notes[i-1]
			rel := math.Max(pre, prev.SustainEnd()+0.001)
			if rel > t-0.001 {
				rel = t - 0.002
			}
			for l := 0; l < 5; l++ {
				if held&(1<<l) != 0 && want&(1<<l) == 0 {
					s.fret(rel, frets[l], false)
				}
			}
		}
		for l := 0; l < 5; l++ {
			if want&(1<<l) != 0 && held&(1<<l) == 0 {
				s.fret(t-0.001, frets[l], true)
			}
		}
		held = want
		s.strum(t)
	}
	return sortScript(s)
}

func TestGuitarPerfect(t *testing.T) {
	c, tr, _ := load(t)
	e := New(c, tr, DefaultSettings())
	run(e, perfect(tr, 0.010), 10)
	st := e.Stats()
	if st.NotesHit != len(tr.Notes) || st.NotesMissed != 0 || st.Overstrums != 0 {
		t.Fatalf("hit=%d miss=%d over=%d judgments=%+v", st.NotesHit, st.NotesMissed, st.Overstrums, e.Judgments())
	}
	if st.MaxStreak != len(tr.Notes) {
		t.Errorf("max streak %d", st.MaxStreak)
	}
	for _, j := range e.Judgments() {
		if j.Result == Hit && j.Via == ViaStrum && math.Abs(j.Offset-0.010) > 1e-9 {
			t.Errorf("offset %v", j.Offset)
		}
	}
	if count(e, SustainDone) != 1 {
		t.Errorf("sustain done count %d", count(e, SustainDone))
	}
	if st.SPPhrasesHit != 1 || math.Abs(st.SPEnergy-0.25) > 1e-9 {
		t.Errorf("sp phrases %d energy %v", st.SPPhrasesHit, st.SPEnergy)
	}
	// 7 singles + 1 chord (2 gems) = 9 gems * 50 = 450, plus 2 beats of sustain * 25 = 50
	if math.Abs(st.Score-500) > 1e-6 {
		t.Errorf("score %v", st.Score)
	}
}

func TestGuitarMissAndOverstrum(t *testing.T) {
	c, tr, _ := load(t)
	e := New(c, tr, DefaultSettings())
	var s script
	s.strum(0.2) // nothing near: overstrum
	s.fret(0.45, action.Green, true)
	s.strum(0.5) // hit note 0
	s.fret(0.9, action.Green, false)
	// note at 1.0 (red) never played -> miss
	run(e, sortScript(s), 1.2)
	st := e.Stats()
	if st.Overstrums != 1 || st.NotesHit != 1 || st.NotesMissed != 1 {
		t.Fatalf("over=%d hit=%d miss=%d %+v", st.Overstrums, st.NotesHit, st.NotesMissed, e.Judgments())
	}
	if st.Streak != 0 {
		t.Errorf("streak should reset, got %d", st.Streak)
	}
}

func TestHOPOByFret(t *testing.T) {
	c, tr, _ := load(t)
	if tr.Notes[2].Kind != chart.HOPO {
		t.Fatalf("expected note 2 to be a HOPO, got %s", tr.Notes[2].Kind)
	}
	e := New(c, tr, DefaultSettings())
	var s script
	s.fret(0.45, action.Green, true)
	s.strum(0.5)
	s.fret(0.98, action.Red, true)
	s.fret(0.98, action.Green, false)
	s.strum(1.0)
	s.fret(1.165, action.Yellow, true) // hammer on to yellow at 1.1667
	s.strum(1.18)                      // stray strum just after: absorbed by lenience
	run(e, sortScript(s), 1.3)
	st := e.Stats()
	if st.NotesHit != 3 || st.Overstrums != 0 {
		t.Fatalf("hit=%d over=%d %+v", st.NotesHit, st.Overstrums, e.Judgments())
	}
	if j := e.Judgments()[2]; j.Via != ViaFret {
		t.Errorf("third hit via %s", j.Via)
	}
}

func TestAnchoringAndChords(t *testing.T) {
	c, tr, _ := load(t)
	e := New(c, tr, DefaultSettings()).(*Guitar)
	// Note 0 (green): holding red too must fail; note 1 (red): holding green too is fine.
	var s script
	s.fret(0.4, action.Green, true)
	s.fret(0.4, action.Red, true)
	s.strum(0.5) // G+R held for G: no match -> waits then overstrums
	s.strum(1.0) // G+R held for R: anchoring hit
	run(e, sortScript(s), 1.05)
	if e.Status(0) != NoteMissed && count(e, Overstrum) != 1 {
		t.Errorf("green with red held should not hit: %+v", e.Judgments())
	}
	if e.Status(1) != NoteHit {
		t.Errorf("red with green anchored should hit: %+v", e.Judgments())
	}
}

func TestFretAfterStrumGrace(t *testing.T) {
	c, tr, _ := load(t)
	e := New(c, tr, DefaultSettings())
	var s script
	s.strum(0.5)
	s.fret(0.515, action.Green, true) // frets land 15ms after the strum
	run(e, sortScript(s), 0.7)
	st := e.Stats()
	if st.NotesHit != 1 || st.Overstrums != 0 {
		t.Fatalf("hit=%d over=%d %+v", st.NotesHit, st.Overstrums, e.Judgments())
	}
	if j := e.Judgments()[0]; math.Abs(j.Offset) > 1e-9 {
		t.Errorf("offset should be measured from the strum, got %v", j.Offset)
	}
}

func TestSustainDrop(t *testing.T) {
	c, tr, _ := load(t)
	e := New(c, tr, DefaultSettings())
	// play up to the blue sustain at 2.5s (ends 3.5s), release at 3.0
	var s script
	s.fret(2.45, action.Blue, true)
	s.strum(2.5)
	s.fret(3.0, action.Blue, false)
	run(e, sortScript(s), 3.2)
	var drop *Judgment
	for i, j := range e.Judgments() {
		if j.Result == SustainDrop {
			drop = &e.Judgments()[i]
		}
	}
	if drop == nil || math.Abs(drop.Value-0.5) > 1e-6 {
		t.Fatalf("drop = %+v", drop)
	}
}

func TestStarPowerActivation(t *testing.T) {
	c, tr, _ := load(t)
	set := DefaultSettings()
	e := New(c, tr, set).(*Guitar)
	e.stats.SPEnergy = 0.5
	var s script
	s.fret(0.4, action.Green, true)
	*(&s) = append(s, action.Event{Time: 0.45, Action: action.StarPower, Down: true})
	s.strum(0.5)
	run(e, sortScript(s), 0.6)
	if !e.stats.SPActive || e.Multiplier() != 2 {
		t.Fatalf("sp active=%v mult=%d", e.stats.SPActive, e.Multiplier())
	}
	if e.stats.Score != 100 {
		t.Errorf("score with SP = %v", e.stats.Score)
	}
	// 0.5 energy lasts 16 beats = 8s at 120bpm from 0.45s
	e.Advance(8.44)
	if !e.stats.SPActive {
		t.Errorf("sp ended early")
	}
	e.Advance(8.46)
	if e.stats.SPActive {
		t.Errorf("sp should have ended at 8.45 (energy %v)", e.stats.SPEnergy)
	}
}

// Advancing at arbitrary frame times must not change the outcome.
func TestDeterministicReplay(t *testing.T) {
	c, tr, _ := load(t)
	evs := perfect(tr, -0.02)
	// sprinkle mistakes
	evs = append(evs, action.Event{Time: 1.7, Action: action.StrumUp, Down: true})
	evs = append(evs, action.Event{Time: 2.9, Action: action.Whammy, Value: 0.8})
	evs = sortScript(evs)

	a := New(c, tr, DefaultSettings())
	run(a, evs, 12)

	rng := rand.New(rand.NewSource(1))
	b := New(c, tr, DefaultSettings())
	frame := 0.0
	for _, ev := range evs {
		for frame < ev.Time {
			b.Advance(frame)
			frame += 0.004 + rng.Float64()*0.02
		}
		b.Process(ev)
	}
	for frame < 12 {
		b.Advance(frame)
		frame += 0.016
	}
	b.Advance(12)

	ja, jb := a.Judgments(), b.Judgments()
	if len(ja) != len(jb) {
		t.Fatalf("judgment count %d vs %d", len(ja), len(jb))
	}
	for i := range ja {
		if ja[i].Result != jb[i].Result || ja[i].Note != jb[i].Note || math.Abs(ja[i].Time-jb[i].Time) > 1e-9 {
			t.Fatalf("judgment %d differs: %+v vs %+v", i, ja[i], jb[i])
		}
	}
	if math.Abs(a.Stats().Score-b.Stats().Score) > 1e-6 {
		t.Fatalf("score %v vs %v", a.Stats().Score, b.Stats().Score)
	}
}

func TestDrums(t *testing.T) {
	c, _, dt := load(t)
	set := DefaultSettings()
	e := New(c, dt, set)
	evs := script{
		{Time: 0.5, Action: action.Kick, Down: true, Value: 1},
		{Time: 0.505, Action: action.DrumYellow, Down: true, Value: 1}, // tom, chart wants cymbal: overhit
		{Time: 1.0, Action: action.DrumRed, Down: true, Value: 1},
		{Time: 1.49, Action: action.DrumYellow, Down: true, Value: 1}, // tom matches
		{Time: 2.0, Action: action.DrumBlue, Down: true, Value: 1},    // nothing there
	}
	run(e, evs, 3)
	st := e.Stats()
	if st.NotesHit != 3 || st.NotesMissed != 1 || st.Overstrums != 2 {
		t.Fatalf("hit=%d miss=%d overhit=%d %+v", st.NotesHit, st.NotesMissed, st.Overstrums, e.Judgments())
	}
	if e.Status(0) != NoteHit || e.LaneHit(0) != 1<<chart.DrumKick {
		t.Errorf("note 0 status %d lanes %b", e.Status(0), e.LaneHit(0))
	}
	// Without pro matching the tom hit counts for the cymbal.
	set.ProDrums = false
	e2 := New(c, dt, set)
	run(e2, evs, 3)
	if e2.Stats().NotesHit != 4 {
		t.Errorf("non-pro hits = %d", e2.Stats().NotesHit)
	}
}

func TestStrumAfterHammeredHOPO(t *testing.T) {
	c, tr, _ := load(t)
	if tr.Notes[2].Kind != chart.HOPO {
		t.Fatalf("test chart: note 2 should be a HOPO, is %v", tr.Notes[2].Kind)
	}
	play := func(set Settings) Stats {
		e := New(c, tr, set)
		var s script
		s.fret(0.45, action.Green, true)
		s.strum(0.5)
		s.fret(0.6, action.Green, false)
		s.fret(0.95, action.Red, true)
		s.strum(1.0)
		s.fret(1.13, action.Yellow, true) // hammer the HOPO 37 ms early...
		s.strum(1.20)                     // ...then strum it out of habit, 33 ms late
		run(e, sortScript(s), 1.5)
		return *e.Stats()
	}
	st := play(DefaultSettings())
	if st.NotesHit != 3 || st.Overstrums != 0 || st.Streak != 3 {
		t.Errorf("current rules: hit=%d over=%d streak=%d, want 3/0/3", st.NotesHit, st.Overstrums, st.Streak)
	}
	if st := play(legacySettings()); st.Overstrums != 1 {
		t.Errorf("rules 0 should still overstrum 70 ms after the hammer (recordings replay as judged), got %d", st.Overstrums)
	}
}

func TestChordLenience(t *testing.T) {
	c, tr, _ := load(t)
	play := func(set Settings) Stats {
		e := New(c, tr, set)
		var s script
		s.strum(0.5)
		s.fret(0.54, action.Green, true) // fret lands 40 ms after the strum
		run(e, sortScript(s), 0.8)
		return *e.Stats()
	}
	if st := play(DefaultSettings()); st.NotesHit != 1 || st.Overstrums != 0 {
		t.Errorf("50 ms chord lenience: hit=%d over=%d", st.NotesHit, st.Overstrums)
	}
	if st := play(legacySettings()); st.Overstrums != 1 {
		t.Errorf("legacy 25 ms lenience should overstrum, got %d", st.Overstrums)
	}
}

func TestInfiniteFrontEnd(t *testing.T) {
	c, tr, _ := load(t)
	play := func(ife bool) *Guitar {
		set := DefaultSettings()
		set.InfiniteFrontEnd = ife
		e := New(c, tr, set).(*Guitar)
		var s script
		s.fret(0.45, action.Green, true)
		s.strum(0.5) // green at 0.5
		s.fret(0.55, action.Green, false)
		s.fret(0.70, action.Red, true)
		s.strum(0.80) // red is due at 1.0: 200 ms early
		run(e, sortScript(s), 0.9)
		return e
	}
	e := play(true)
	if e.Status(1) != NoteHit || e.Stats().Overstrums != 0 {
		t.Fatalf("infinite front end: red should hit 200 ms early: %+v", e.Judgments())
	}
	if j := e.Judgments()[len(e.Judgments())-1]; math.Abs(j.Offset+0.2) > 1e-9 {
		t.Errorf("early hit should record its real offset, got %v", j.Offset)
	}
	e = play(false)
	if e.Status(1) == NoteHit || e.Stats().Overstrums != 1 {
		t.Fatalf("normal front end: 200 ms early is an overstrum: %+v", e.Judgments())
	}
	// Only the next note opens early: before green is played, red can't be.
	set := DefaultSettings()
	g := New(c, tr, set).(*Guitar)
	var s script
	s.fret(0.2, action.Red, true)
	s.strum(0.25)
	run(g, sortScript(s), 0.3)
	if g.Status(1) == NoteHit {
		t.Fatal("red hit before green was due")
	}
}

// legacySettings are the defaults as recorded by runs before rules version 1
// (fields added since then are zero in those recordings).
func legacySettings() Settings {
	return Settings{EarlyWindow: 0.07, LateWindow: 0.07, StrumLenience: 0.06, NoFail: true, ProDrums: true, BassMaxMult: 4}
}
