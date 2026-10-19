package app

import (
	"math/rand"
	"testing"
	"time"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/demo"
	"github.com/mxygem/jam/internal/input"
)

// until runs frames (drawing every few) until cond is true or the song
// clock has advanced by max seconds.
func (h *harness) until(what string, max float64, cond func() bool, feed func() []input.RawEvent) {
	h.t.Helper()
	end := h.now + max
	for !cond() {
		if h.now > end {
			h.t.Fatalf("timed out waiting for %s (scene %T)", what, h.a.scene)
		}
		var evs []input.RawEvent
		if feed != nil {
			evs = feed()
		}
		switch sc := h.a.scene.(type) {
		case *Loading, *BandLoading, *PartyStart:
			time.Sleep(2 * time.Millisecond) // the loader is a real goroutine
		case *BandPlay:
			if sc.endAt > 0 {
				time.Sleep(2 * time.Millisecond) // the next party song loads
			}
		}
		h.frame(evs...)
	}
}

func sceneIs[T Scene](h *harness) func() bool {
	return func() bool { _, ok := h.a.scene.(T); return ok }
}

func withDemoLibrary(h *harness) {
	if _, err := demo.Write(config.SongsDir()); err != nil {
		h.t.Fatal(err)
	}
	h.a.library = chart.ScanLibrary([]string{config.SongsDir()})
	if len(h.a.library) == 0 {
		h.t.Fatal("demo song not found by the scanner")
	}
	h.a.byDir = map[string]*chart.Song{}
	for _, s := range h.a.library {
		h.a.byDir[dirKey(s.Dir)] = s
	}
}

// mashGuitar returns random but plausible guitar input: frets come and go,
// strums every ~150 ms, whammy wiggles.
func mashGuitar(h *harness, rng *rand.Rand) func() []input.RawEvent {
	held := map[int]bool{}
	frets := []int{bitA, bitB, bitY, bitX, bitLB}
	next := 0.0
	return func() []input.RawEvent {
		var evs []input.RawEvent
		if h.now < next {
			return nil
		}
		next = h.now + 0.05 + rng.Float64()*0.2
		f := frets[rng.Intn(5)]
		held[f] = !held[f]
		evs = append(evs, xbtn(f, held[f]))
		s := bitDPadDown
		if rng.Intn(2) == 0 {
			s = bitDPadUp
		}
		evs = append(evs, xbtn(s, true))
		evs = append(evs, input.RawEvent{Time: h.now + 0.004, Src: input.SrcXInput, Kind: input.KindButton, Code: s})
		evs = append(evs, xaxis(axisRX, float32(rng.Float64()*2-1)))
		return evs
	}
}

func TestGuitarFlowMenuToResults(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.scene = NewMainMenu(a)
	h.idle(0.1)
	h.tap(bitA) // Play
	if !sceneIs[*SongSelect](h)() {
		t.Fatalf("scene %T after Play", a.scene)
	}
	h.tap(bitA) // open the picker
	h.tap(bitA) // "Play"
	h.until("play scene", 10, sceneIs[*Play](h), nil)
	p := a.scene.(*Play)
	rng := rand.New(rand.NewSource(1))
	mash := mashGuitar(h, rng)
	h.until("song start", 5, func() bool { return p.started }, nil)
	h.until("10 s into the song", 12, func() bool { return p.songNow(h.now) > 10 }, mash)
	// Pause with Start, resume with green.
	h.tap(bitStart)
	if !p.paused {
		t.Fatal("Start should pause")
	}
	h.idle(1)
	h.tap(bitA)
	if p.paused {
		t.Fatal("green on Resume should resume")
	}
	h.until("results", 200, sceneIs[*Results](h), mash)
	// let go of whatever the mash left held
	h.frame(xbtn(bitA, false), xbtn(bitB, false), xbtn(bitY, false), xbtn(bitX, false), xbtn(bitLB, false))
	r := a.scene.(*Results)
	if r.rec.Result.NotesHit+r.rec.Result.NotesMissed == 0 {
		t.Fatal("results show no judged notes")
	}
	for i := 0; i < 4; i++ { // flip through the tabs, scroll each
		h.tap(bitLB) // orange: next tab
		for k := 0; k < 30; k++ {
			h.tap(bitDPadDown)
		}
	}
	h.tap(bitB) // red: back
	h.until("song select", 2, sceneIs[*SongSelect](h), nil)
}

func TestPauseMenuRestartAndQuit(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	a.scene = NewSongSelect(a)
	h.tap(bitA)
	h.tap(bitA)
	h.until("play", 10, sceneIs[*Play](h), nil)
	h.until("4 s in", 10, func() bool { return a.scene.(*Play).songNow(h.now) > 4 }, nil)
	first := a.scene
	h.tap(bitStart)
	h.tap(bitDPadDown) // Restart
	h.tap(bitA)
	if a.scene == first {
		t.Fatal("Restart should start a fresh play scene")
	}
	h.idle(4)
	h.tap(bitStart)
	for i := 0; i < 4; i++ {
		h.tap(bitDPadDown) // to "Quit to song list"
	}
	h.tap(bitA)
	h.until("song select", 2, sceneIs[*SongSelect](h), nil)
	// End-early path produces a reviewable run.
	h.tap(bitA)
	h.tap(bitA)
	h.until("play", 10, sceneIs[*Play](h), nil)
	h.until("6 s in", 10, func() bool { return a.scene.(*Play).songNow(h.now) > 6 }, mashGuitar(h, rand.New(rand.NewSource(2))))
	h.tap(bitStart)
	for i := 0; i < 3; i++ {
		h.tap(bitDPadDown) // to "End run and review it"
	}
	h.tap(bitA)
	h.until("results", 2, sceneIs[*Results](h), nil)
}

// Roland TD-07 default note map.
var td07 = map[action.Action][]int{
	action.DrumRed: {38, 40}, action.CymYellow: {42, 46, 22, 26}, action.DrumYellow: {48, 50},
	action.CymBlue: {51, 59, 53}, action.DrumBlue: {45, 47}, action.CymGreen: {49, 55},
	action.DrumGreen: {43, 58}, action.Kick: {36},
}

func midiHit(note int, vel float32) input.RawEvent {
	return input.RawEvent{Src: input.SrcMIDI, Device: 0, Kind: input.KindNote, Code: note, Value: vel, Down: true}
}

func midiOff(note int) input.RawEvent {
	return input.RawEvent{Src: input.SrcMIDI, Device: 0, Kind: input.KindNote, Code: note}
}

func (h *harness) hit(note int) {
	h.frame(midiHit(note, 0.8))
	h.idle(0.03)
	h.frame(midiOff(note))
	h.idle(0.15)
}

func TestDrumWizardAndPlayWithTD07(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	w := NewWizard(a, input.FamilyDrums)
	a.scene = w
	h.hit(38) // pick the kit
	if w.stage != stageBind {
		t.Fatalf("stage %v after hitting the snare", w.stage)
	}
	for _, act := range action.DrumSet {
		if w.stage != stageBind {
			break
		}
		if w.current() != act {
			t.Fatalf("at %s, expected %s", w.current().Title(), act.Title())
		}
		notes, ok := td07[act]
		if !ok {
			h.frame(keyDown(keyBackspace)) // no orange pad, SP, start or select on the kit
			h.idle(0.1)
			continue
		}
		for _, n := range notes {
			h.hit(n)
		}
		h.idle(1.8) // wizard moves on once the kit goes quiet
	}
	if w.stage != stageTest {
		t.Fatalf("drum wizard stuck at %s", w.current().Title())
	}
	for _, n := range []int{38, 40, 42, 26, 36, 53, 44} {
		h.hit(n)
	}
	h.frame(keyDown(keyEnter))
	p := a.Bindings.Profile(input.FamilyDrums)
	got := map[int]bool{}
	for _, b := range p.Get(action.CymYellow) {
		if b.Src == "midi" {
			got[b.Code] = true
		}
	}
	for _, n := range td07[action.CymYellow] {
		if !got[n] {
			t.Errorf("hi-hat note %d not bound (got %v)", n, got)
		}
	}
	for _, b := range p.Get(action.DrumOrange) {
		if b.Src == "midi" {
			t.Errorf("skipped orange pad still bound to %s", b.Describe())
		}
	}

	// Play the demo's drum chart on the kit.
	a.Settings.LastInstrument = "drums"
	a.scene = NewSongSelect(a)
	h.frame(midiHit(43, 0.8)) // green tom: confirm
	h.frame(midiOff(43))
	h.idle(0.1)
	h.frame(midiHit(43, 0.8)) // Play
	h.frame(midiOff(43))
	h.until("drum play", 10, sceneIs[*Play](h), nil)
	pl := a.scene.(*Play)
	if !pl.drums {
		t.Fatal("expected the drums part")
	}
	rng := rand.New(rand.NewSource(3))
	pads := []int{36, 38, 40, 42, 46, 48, 45, 43, 49, 51, 53, 44}
	next := 0.0
	h.until("results", 200, sceneIs[*Results](h), func() []input.RawEvent {
		if h.now < next {
			return nil
		}
		next = h.now + 0.08 + rng.Float64()*0.15
		n := pads[rng.Intn(len(pads))]
		return []input.RawEvent{midiHit(n, float32(rng.Float64())), {Time: h.now + 0.002, Src: input.SrcMIDI, Kind: input.KindNote, Code: n}}
	})
	r := a.scene.(*Results)
	if r.rec.Instrument != "drums" || r.rec.Result.NotesHit == 0 {
		t.Fatalf("drum run: %s, hit %d", r.rec.Instrument, r.rec.Result.NotesHit)
	}
	for i := 0; i < 3; i++ {
		h.frame(midiHit(51, 0.8)) // blue: tabs/scroll
		h.frame(midiOff(51))
	}
}
