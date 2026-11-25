package app

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/input"
)

// scriptedKeys stands in for the keyboard: the keys listed in next count as
// pressed on the next frame, and held keys stay down.
type scriptedKeys struct {
	next  []ebiten.Key
	down  map[ebiten.Key]bool
	chars []rune // typed this frame
}

func (k *scriptedKeys) has(key ebiten.Key) bool {
	for _, n := range k.next {
		if n == key {
			return true
		}
	}
	return false
}

func (k *scriptedKeys) just(key ebiten.Key) bool   { return k.has(key) }
func (k *scriptedKeys) repeat(key ebiten.Key) bool { return k.has(key) }
func (k *scriptedKeys) held(key ebiten.Key) bool   { return k.down[key] }
func (k *scriptedKeys) typed() []rune              { return k.chars }

// editorSong writes a song folder with a guitar part: a chord at tick 0 and a
// single note on beat 2.
func editorSong(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chartText := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n" +
		"[ExpertSingle]\n{\n  0 = N 0 0\n  0 = N 1 0\n  192 = N 4 0\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "notes.chart"), []byte(chartText), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// startEditor opens the editor on the song in a harness and waits for it to load.
func startEditor(t *testing.T, dir string, inst chart.Instrument) (*harness, *ChartEditor, *scriptedKeys) {
	t.Helper()
	h := newHarness(t)
	s, err := chart.LoadSongFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := NewChartEditor(h.a, s, inst, chart.Expert)
	keys := &scriptedKeys{down: map[ebiten.Key]bool{}}
	e.keys = keys
	h.a.SetScene(e)
	// The song loads on another goroutine, so wait in real time.
	deadline := time.Now().Add(10 * time.Second)
	for e.c == nil && e.loadErr == "" {
		if time.Now().After(deadline) {
			t.Fatal("timed out loading the song")
		}
		h.frame()
		time.Sleep(2 * time.Millisecond)
	}
	if e.loadErr != "" {
		t.Fatalf("load: %s", e.loadErr)
	}
	return h, e, keys
}

// press runs one frame with the given keys pressed.
func press(h *harness, k *scriptedKeys, keys ...ebiten.Key) {
	k.next = keys
	h.frame()
	k.next = nil
}

func TestChartEditorLoadsChartNotes(t *testing.T) {
	_, e, _ := startEditor(t, editorSong(t), chart.Guitar)
	want := []chart.ProjNote{
		{Tick: 0, Lanes: []int{0, 1}, Kind: "strum"},
		{Tick: 192, Lanes: []int{4}, Kind: "strum"},
	}
	if !reflect.DeepEqual(e.ed.notes, want) {
		t.Errorf("notes:\n got %+v\nwant %+v", e.ed.notes, want)
	}
	if e.ed.res != 192 || e.ed.dirty {
		t.Errorf("res %d dirty %v", e.ed.res, e.ed.dirty)
	}
}

func TestChartEditorEditsAndSaves(t *testing.T) {
	dir := editorSong(t)
	h, e, keys := startEditor(t, dir, chart.Guitar)

	press(h, keys, ebiten.Key3) // yellow to the chord at tick 0 (cursor starts at 0)
	press(h, keys, ebiten.KeyArrowUp)
	if e.ed.cursor != 48 {
		t.Fatalf("cursor after Up = %d, want 48 (quarter beat)", e.ed.cursor)
	}
	press(h, keys, ebiten.Key5) // orange at tick 48
	press(h, keys, ebiten.KeyL) // no sustain on a note at tick 48 yet: L works and gives it one
	if !e.ed.dirty {
		t.Fatal("edits should mark dirty")
	}
	press(h, keys, ebiten.KeyS) // no Ctrl: must NOT save
	if _, err := os.Stat(filepath.Join(dir, chart.ProjectFile)); err == nil {
		t.Fatal("saved without Ctrl")
	}
	keys.down[ebiten.KeyControlLeft] = true
	press(h, keys, ebiten.KeyS)
	keys.down[ebiten.KeyControlLeft] = false
	if e.ed.dirty {
		t.Fatalf("Ctrl+S should save: status %q", e.status)
	}

	// The saved project loads back into the same notes.
	s, _ := chart.LoadSongFolder(dir)
	c, err := s.LoadChart()
	if err != nil || s.ProjectErr != nil {
		t.Fatal(err, s.ProjectErr)
	}
	got := c.ProjectNotes(chart.TrackKey{Inst: chart.Guitar, Diff: chart.Expert})
	want := []chart.ProjNote{
		{Tick: 0, Lanes: []int{0, 1, 2}, Kind: "strum"},
		{Tick: 48, Lanes: []int{4}, Length: 192, Kind: "hopo"}, // 48 ticks after the chord, on a new lane: a natural HOPO
		{Tick: 192, Lanes: []int{4}, Kind: "strum"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("saved notes:\n got %+v\nwant %+v", got, want)
	}
}

func TestChartEditorDeleteAndCursorBounds(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyArrowDown) // already at 0: stays there
	if e.ed.cursor != 0 {
		t.Errorf("cursor below zero: %d", e.ed.cursor)
	}
	press(h, keys, ebiten.KeyDelete)
	if len(e.ed.notes) != 1 || e.ed.notes[0].Tick != 192 {
		// the note at 0 goes; the cursor is at 0
		t.Errorf("delete at cursor: %+v", e.ed.notes)
	}
}

func TestChartEditorSnapKeys(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyBracketRight) // finer: 1/8
	if e.ed.div() != 8 {
		t.Errorf("] should go finer: div %d", e.ed.div())
	}
	press(h, keys, ebiten.KeyBracketLeft)
	press(h, keys, ebiten.KeyBracketLeft)
	if e.ed.div() != 2 {
		t.Errorf("[ should go coarser: div %d", e.ed.div())
	}
}

func TestChartEditorConfirmsDiscard(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.Key2) // a change
	press(h, keys, ebiten.KeyEscape)
	if _, ok := h.a.scene.(*ChartEditor); !ok || !e.confirmExit {
		t.Fatalf("first Esc should ask, scene %T", h.a.scene)
	}
	press(h, keys, ebiten.KeyEscape)
	if _, ok := h.a.scene.(*SongSelect); !ok {
		t.Fatalf("second Esc should discard and leave, scene %T", h.a.scene)
	}
}

func TestChartEditorLeavesCleanly(t *testing.T) {
	h, _, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyEscape)
	if _, ok := h.a.scene.(*SongSelect); !ok {
		t.Fatalf("Esc with nothing changed should leave, scene %T", h.a.scene)
	}
}

// drumSong writes a song with a four-lane drum part: kick and red on tick 0,
// yellow on beat 1.
func drumSong(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chartText := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n" +
		"[ExpertDrums]\n{\n  0 = N 0 0\n  0 = N 1 0\n  192 = N 2 0\n}\n"
	writeTestFile(t, dir, "notes.chart", chartText)
	return dir
}

func TestChartEditorDrums(t *testing.T) {
	dir := drumSong(t)
	h, e, keys := startEditor(t, dir, chart.Drums)
	if !e.ed.drums || e.ed.mode != chart.DrumsFourLane || e.ed.cols() != 5 {
		t.Fatalf("drum part: drums %v mode %v cols %d", e.ed.drums, e.ed.mode, e.ed.cols())
	}
	if len(e.ed.notes) != 2 || len(e.ed.notes[0].Lanes) != 2 {
		t.Fatalf("notes: %+v", e.ed.notes)
	}
	press(h, keys, ebiten.Key2) // red comes off the kick-and-red note at 0
	if got := e.ed.notes[0].Lanes; len(got) != 1 || got[0] != chart.DrumKick {
		t.Errorf("after 2: %v", got)
	}
	keys.down[ebiten.KeyControlLeft] = true
	press(h, keys, ebiten.Key1) // Ctrl+1: accent on the kick
	press(h, keys, ebiten.KeyX) // double kick
	keys.down[ebiten.KeyControlLeft] = false
	if !hasLane(e.ed.notes[0].Accents, chart.DrumKick) || !e.ed.notes[0].TwoX {
		t.Errorf("accent or double kick: %+v", e.ed.notes[0])
	}
	press(h, keys, ebiten.KeyM) // four-lane to pro
	if e.ed.mode != chart.DrumsPro {
		t.Fatalf("M should move to a pro kit, got %v", e.ed.mode)
	}
	for range 4 {
		press(h, keys, ebiten.KeyArrowUp)
	}
	keys.down[ebiten.KeyShiftLeft] = true
	press(h, keys, ebiten.Key3) // Shift+3: yellow on the note at beat 1 is a cymbal
	keys.down[ebiten.KeyShiftLeft] = false
	if !hasLane(e.ed.notes[1].Cymbals, chart.DrumYellow) {
		t.Errorf("cymbal: %+v", e.ed.notes[1])
	}
	press(h, keys, ebiten.KeyM) // to five-lane
	if e.ed.mode != chart.DrumsFiveLane || e.ed.cols() != 6 {
		t.Fatalf("five-lane kit: mode %v cols %d", e.ed.mode, e.ed.cols())
	}
	press(h, keys, ebiten.Key6) // green on the fifth lane at beat 1
	press(h, keys, ebiten.KeyM) // a note on lane 5 blocks the move back
	if e.ed.mode != chart.DrumsFiveLane || !strings.Contains(e.status, "lane 5") {
		t.Errorf("moving off a five-lane kit with lane 5 notes: mode %v status %q", e.ed.mode, e.status)
	}
	keys.down[ebiten.KeyControlLeft] = true
	press(h, keys, ebiten.KeyS)
	keys.down[ebiten.KeyControlLeft] = false
	if e.ed.dirty {
		t.Fatal("drum edits should save")
	}
	s, _ := chart.LoadSongFolder(dir)
	c, err := s.LoadChart()
	if err != nil || s.ProjectErr != nil {
		t.Fatal(err, s.ProjectErr)
	}
	tr := c.Track(chart.Drums, chart.Expert)
	if tr == nil || tr.DrumMode != chart.DrumsFiveLane {
		t.Fatalf("saved drums: %+v", tr)
	}
	if !tr.Notes[0].DoubleKick || tr.Notes[1].Cymbals&(1<<chart.DrumYellow) == 0 {
		t.Errorf("saved drum marks: %+v", tr.Notes)
	}
}

func TestChartEditorPlayFollowsSong(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeySpace)
	if !e.playing() {
		t.Fatal("Space should start the song")
	}
	h.idle(1.0)
	if e.ed.cursor <= 0 {
		t.Errorf("cursor should follow the song, at %d", e.ed.cursor)
	}
	press(h, keys, ebiten.KeySpace)
	if e.playing() {
		t.Error("Space should pause")
	}
	h.idle(0.2)
	if e.viewTick < 0 {
		t.Errorf("view tick %v", e.viewTick)
	}
}

// TestChartEditorShot writes the editor's screen when JAM_SHOTS names a folder.
func TestChartEditorShot(t *testing.T) {
	if os.Getenv("JAM_SHOTS") == "" {
		t.Skip("set JAM_SHOTS to a folder to write screenshots")
	}
	dir := t.TempDir()
	chartText := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n" +
		"[ExpertSingle]\n{\n  0 = N 0 0\n  0 = N 2 0\n  192 = N 1 192\n  384 = N 3 0\n  576 = N 4 0\n  768 = N 0 0\n  768 = N 5 0\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "notes.chart"), []byte(chartText), 0o644); err != nil {
		t.Fatal(err)
	}
	h, e, keys := startEditor(t, dir, chart.Guitar)
	press(h, keys, ebiten.KeyArrowUp)
	press(h, keys, ebiten.KeyArrowUp)
	press(h, keys, ebiten.Key4)
	press(h, keys, ebiten.KeyL)
	h.idle(0.1)
	h.shot("chart-editor")
	_ = e
}

// typeChars types text over one frame, the way a keyboard does.
func typeChars(h *harness, k *scriptedKeys, text string) {
	k.chars = []rune(text)
	h.frame()
	k.chars = nil
}

func TestLyricModeWritesWordsAndSaves(t *testing.T) {
	dir := editorSong(t)
	h, e, keys := startEditor(t, dir, chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	if e.mode != modeLyrics || e.lyr == nil {
		t.Fatalf("Tab should open lyrics: mode %v", e.mode)
	}
	press(h, keys, ebiten.KeyInsert) // a first word, at the cursor's time
	typeChars(h, keys, "hel-")
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "lo")
	if got := e.lyr.Lines(); len(got) != 1 || got[0] != "hel- lo" {
		t.Fatalf("typed lines: %q", got)
	}
	keys.down[ebiten.KeyControlLeft] = true
	press(h, keys, ebiten.KeyS)
	keys.down[ebiten.KeyControlLeft] = false
	if e.lyr.dirty {
		t.Fatalf("Ctrl+S should save the lyrics: %q", e.status)
	}

	pv, err := chart.LoadProjectVocals(dir)
	if err != nil || len(pv.Parts) != 1 || !pv.HasGrid {
		t.Fatalf("saved vocals: %+v %v", pv, err)
	}
	s, _ := chart.LoadSongFolder(dir)
	c, err := s.LoadChart()
	if err != nil || s.ProjectErr != nil {
		t.Fatal(err, s.ProjectErr)
	}
	if len(c.Lyrics) != 1 || c.Lyrics[0].Line() != "hello" {
		t.Errorf("the game should show the saved lyric, got %+v", c.Lyrics)
	}
	// The notes were not touched by the lyric edit.
	if got := c.ProjectNotes(chart.TrackKey{Inst: chart.Guitar, Diff: chart.Expert}); len(got) != 2 {
		t.Errorf("notes changed by a lyric save: %+v", got)
	}
}

func TestLyricStampWhilePlayingTimesWords(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "one")
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "two")
	press(h, keys, ebiten.KeyArrowLeft) // back to "one"

	pressCtrl(h, keys, ebiten.KeySpace)
	var want []int64
	for _, wait := range []float64{0.5, 0.5} {
		h.idle(wait)
		// The stamp is the song position the player hears at the press.
		want = append(want, int64(math.Round(e.tm.Ticks(e.songPos()))))
		press(h, keys, ebiten.KeyEnter)
	}
	words := e.lyr.leadPart().Phrases[0].Syllables
	if len(words) != 2 {
		t.Fatalf("words: %+v", words)
	}
	for i := range words {
		if d := words[i].T - want[i]; d < -1 || d > 1 {
			t.Errorf("word %d stamped at tick %d, the song was at %d", i, words[i].T, want[i])
		}
	}
	if gap := words[1].T - words[0].T; gap < 170 || gap > 220 {
		t.Errorf("0.5 s apart should be about a beat (192 ticks): got %d", gap)
	}
	if !e.lyr.dirty {
		t.Error("stamps are edits")
	}
}

func TestLyricStampWhileStoppedOnlyMoves(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "a")
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "b")
	press(h, keys, ebiten.KeyArrowLeft)
	before := e.lyr.leadPart().Phrases[0].Syllables[0].T
	press(h, keys, ebiten.KeyEnter)
	if got := e.lyr.leadPart().Phrases[0].Syllables[0].T; got != before {
		t.Errorf("stopped Enter must not retime the word: %d -> %d", before, got)
	}
	if e.lyr.Selected().Text != "b" {
		t.Errorf("Enter should move on to %q", "b")
	}
}

func TestTabBackToNotesKeepsTheCursor(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyArrowUp)
	cursor := e.ed.cursor
	press(h, keys, ebiten.KeyTab)
	press(h, keys, ebiten.KeyTab)
	if e.mode != modeNotes || e.ed.cursor != cursor {
		t.Errorf("mode %v cursor %d (was %d)", e.mode, e.ed.cursor, cursor)
	}
}

func TestLyricEscapeAsksAboutUnsavedWords(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "x")
	press(h, keys, ebiten.KeyEscape)
	if !e.confirmExit {
		t.Fatal("unsaved lyrics should block the first Esc")
	}
}

// The audio latency setting moves stamps earlier, as the game judges input.
func TestAudioOffsetShiftsStamps(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	e.audioOff = 0.25
	press(h, keys, ebiten.KeyTab)
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "a")
	pressCtrl(h, keys, ebiten.KeySpace)
	h.idle(0.5) // the song is at 0.5 s; heard at 0.25 s: tick 96 at 120 BPM
	press(h, keys, ebiten.KeyEnter)
	if d := e.lyr.leadPart().Phrases[0].Syllables[0].T - 96; d < -24 || d > 24 {
		t.Errorf("stamp %d, want about 96 with a 250 ms audio offset", e.lyr.leadPart().Phrases[0].Syllables[0].T)
	}
}

// TestChartEditorLyricShot writes the lyric editor's screen when JAM_SHOTS is set.
func TestChartEditorLyricShot(t *testing.T) {
	if os.Getenv("JAM_SHOTS") == "" {
		t.Skip("set JAM_SHOTS to a folder to write screenshots")
	}
	dir := editorSong(t)
	h, _, keys := startEditor(t, dir, chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	for i, w := range []string{"hel-", "lo", "world", "and", "some", "more", "words"} {
		press(h, keys, ebiten.KeyInsert)
		typeChars(h, keys, w)
		if i == 3 {
			press(h, keys, ebiten.KeyEnter) // ctrl would split; plain Enter stamps
		}
	}
	press(h, keys, ebiten.KeyArrowLeft)
	press(h, keys, ebiten.KeyArrowLeft)
	h.idle(0.1)
	h.shot("chart-editor-lyrics")
}

func TestSyncPointWhilePlayingIsSaved(t *testing.T) {
	dir := editorSong(t)
	h, e, keys := startEditor(t, dir, chart.Guitar)
	press(h, keys, ebiten.KeySpace)
	h.idle(0.5)
	press(h, keys, ebiten.KeyS)
	if len(e.anchors) != 1 {
		t.Fatalf("S while playing should add a sync point: %+v (%q)", e.anchors, e.status)
	}
	a := e.anchors[0]
	if a.Tick%192 != 0 {
		t.Errorf("sync points sit on beats, got tick %d", a.Tick)
	}
	if !e.syncDirty || !e.dirty() {
		t.Error("a sync point is an unsaved change")
	}
	keys.down[ebiten.KeyControlLeft] = true
	press(h, keys, ebiten.KeyS)
	keys.down[ebiten.KeyControlLeft] = false
	if e.syncDirty || len(e.anchors) != 1 {
		t.Fatalf("Ctrl+S must save, not add another point: %+v dirty %v", e.anchors, e.syncDirty)
	}
	got, err := chart.LoadProjectSync(dir)
	if err != nil || len(got) != 1 || got[0].Tick != a.Tick {
		t.Errorf("saved sync points: %+v %v", got, err)
	}
}

func TestSyncStoppedOnlyExplains(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyS)
	if len(e.anchors) != 0 || !strings.Contains(e.status, "Play the song") {
		t.Errorf("stopped S: anchors %+v status %q", e.anchors, e.status)
	}
}

func TestSyncRefusesOutOfOrderPoints(t *testing.T) {
	_, e, _ := startEditor(t, editorSong(t), chart.Guitar)
	if !e.addSync(0, 1.0) {
		t.Fatal("first point should be accepted")
	}
	if e.addSync(384, 0.5) {
		t.Error("a later beat at an earlier time must be refused")
	}
	if !e.addSync(384, 3.0) {
		t.Error("a later beat at a later time is fine")
	}
	if len(e.anchors) != 2 {
		t.Errorf("anchors %+v", e.anchors)
	}
}

func TestShiftSRemovesNearestPoint(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	e.addSync(192, 2.0)
	e.addSync(576, 4.0)
	e.ed.cursor = 192
	keys.down[ebiten.KeyShiftLeft] = true
	press(h, keys, ebiten.KeyS)
	keys.down[ebiten.KeyShiftLeft] = false
	if len(e.anchors) != 1 || e.anchors[0].Tick != 576 {
		t.Errorf("after Shift+S: %+v", e.anchors)
	}
}

// The sync points in the project file set the editor's timing from the start.
func TestSavedSyncPointsSetTheEditorsTiming(t *testing.T) {
	dir := editorSong(t)
	if err := chart.SaveProjectSync(dir, []chart.Anchor{{Tick: 0, Sec: 0.5}, {Tick: 384, Sec: 2.5}}); err != nil {
		t.Fatal(err)
	}
	h, e, _ := startEditor(t, dir, chart.Guitar)
	_ = h
	if got := e.tm.Seconds(192); got < 1.4999 || got > 1.5001 {
		t.Errorf("tick 192 at %v s, want 1.5 (halfway between the points)", got)
	}
	if bpm := e.bpmAt(0); bpm < 59.9 || bpm > 60.1 {
		t.Errorf("tempo between the points %v BPM, want 60", bpm)
	}
}

// TestChartEditorSyncShot writes the editor with sync points when JAM_SHOTS is set.
func TestChartEditorSyncShot(t *testing.T) {
	if os.Getenv("JAM_SHOTS") == "" {
		t.Skip("set JAM_SHOTS to a folder to write screenshots")
	}
	dir := t.TempDir()
	chartText := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n" +
		"[ExpertSingle]\n{\n  0 = N 0 0\n  192 = N 1 0\n  384 = N 2 0\n  576 = N 3 0\n  768 = N 4 0\n}\n"
	writeTestFile(t, dir, "notes.chart", chartText)
	h, e, keys := startEditor(t, dir, chart.Guitar)
	e.addSync(0, 0.25)
	e.addSync(384, 1.0) // the middle of the song runs slower
	e.addSync(768, 2.5)
	press(h, keys, ebiten.KeyArrowUp)
	press(h, keys, ebiten.KeyArrowUp)
	h.idle(0.1)
	h.shot("chart-editor-sync")
}

// The song list's options open the chart editor on the highlighted song.
func TestSongListOptionsOpenChartEditor(t *testing.T) {
	h := newHarness(t)
	withDemoLibrary(h)
	a := h.a
	s := NewSongSelect(a)
	a.scene = s
	h.idle(0.2)
	if s.current() == nil {
		t.Fatal("the demo library should have a selected song")
	}
	s.openOptions(a)
	opened := false
	for _, it := range s.menu.Items {
		if it.Label == "Edit chart" {
			it.Activate()
			opened = true
		}
	}
	if !opened {
		t.Fatal("no Edit chart option in the song list's options")
	}
	if _, ok := a.scene.(*ChartEditor); !ok {
		t.Fatalf("Edit chart should open the editor, scene %T", a.scene)
	}
}

// noteAt finds the note on a tick, or nil.
func noteAt(e *ChartEditor, tick int64) *chart.ProjNote {
	for i := range e.ed.notes {
		if e.ed.notes[i].Tick == tick {
			return &e.ed.notes[i]
		}
	}
	return nil
}

// A fret pressed and let go without a strum is a tap note.
func TestFretTapMakesATapNote(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyArrowUp) // cursor to 48
	h.frame(xbtn(bitA, true))
	h.frame(xbtn(bitA, false))
	n := noteAt(e, 48)
	if n == nil || !hasLane(n.Lanes, chart.LaneGreen) || n.Kind != "tap" || n.Length != 0 {
		t.Fatalf("a quick fret should be a tap: %+v", n)
	}
}

// A fret held down is a sustain as long as the hold.
func TestFretHoldMakesASustain(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyArrowUp)
	h.frame(xbtn(bitA, true))
	h.idle(0.5) // one beat at 120 BPM
	h.frame(xbtn(bitA, false))
	n := noteAt(e, 48)
	if n == nil || n.Kind != "tap" || n.Length < 144 || n.Length > 240 || n.Length%48 != 0 {
		t.Fatalf("a half-second hold should be about a beat of sustain: %+v", n)
	}
}

// A strum takes the frets held at the time into one note.
func TestStrumTakesHeldFrets(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyArrowUp)
	h.frame(xbtn(bitA, true))
	h.frame(xbtn(bitDPadDown, true)) // strum
	h.frame(xbtn(bitDPadDown, false))
	h.frame(xbtn(bitA, false))
	n := noteAt(e, 48)
	if n == nil || !hasLane(n.Lanes, chart.LaneGreen) || n.Kind != "strum" {
		t.Fatalf("strum with green held: %+v", n)
	}
}

// A strum with no frets held is an open note.
func TestStrumWithNoFretsIsOpen(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyArrowUp)
	h.frame(xbtn(bitDPadUp, true)) // strum up
	h.frame(xbtn(bitDPadUp, false))
	n := noteAt(e, 48)
	if n == nil || !hasLane(n.Lanes, chart.LaneOpen) || n.Kind != "strum" {
		t.Fatalf("strum with nothing held should be open: %+v", n)
	}
}

// A fret held through a strum sustains that strum's note.
func TestFretHeldThroughStrumSustainsIt(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyArrowUp)
	h.frame(xbtn(bitA, true))
	h.frame(xbtn(bitDPadDown, true))
	h.frame(xbtn(bitDPadDown, false))
	h.idle(0.5)
	h.frame(xbtn(bitA, false))
	n := noteAt(e, 48)
	if n == nil || n.Kind != "strum" || n.Length < 144 {
		t.Fatalf("held through the strum should sustain it: %+v", n)
	}
}

// Star Power steps the note at the cursor through tap, hopo and strum.
func TestStarPowerCyclesNoteType(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyArrowUp)
	h.frame(xbtn(bitA, true))
	h.frame(xbtn(bitA, false))
	var got []string
	for range 4 {
		h.frame(xbtn(bitBack, true))
		h.frame(xbtn(bitBack, false))
		got = append(got, noteAt(e, 48).Kind)
	}
	if !reflect.DeepEqual(got, []string{"hopo", "strum", "tap", "hopo"}) {
		t.Errorf("kinds after each Star Power press: %v", got)
	}
}

// Releasing a fret while the editor is in lyrics doesn't leave it held.
func TestFretReleasedInLyricsIsNotStuck(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	h.frame(xbtn(bitA, true))
	press(h, keys, ebiten.KeyTab)
	h.frame(xbtn(bitA, false))
	if e.frets.held[0] {
		t.Error("the green fret is still held after release")
	}
}

// The keyboard's own guitar keys don't edit the chart (S is the sync key).
func TestKeyboardGuitarKeysDoNotEdit(t *testing.T) {
	h, e, _ := startEditor(t, editorSong(t), chart.Guitar)
	before := len(e.ed.notes)
	h.frame(keyDown(ebiten.KeyA)) // A is green on the default keyboard guitar
	h.frame(keyUp(ebiten.KeyA))
	if len(e.ed.notes) != before || e.ed.dirty {
		t.Errorf("keyboard A changed the chart: %+v", e.ed.notes)
	}
}

// TestChartEditorDrumShot writes a drum part on a five-lane kit when JAM_SHOTS is set.
func TestChartEditorDrumShot(t *testing.T) {
	if os.Getenv("JAM_SHOTS") == "" {
		t.Skip("set JAM_SHOTS to a folder to write screenshots")
	}
	dir := t.TempDir()
	chartText := "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n" +
		"[ExpertDrums]\n{\n  0 = N 0 0\n  0 = N 2 0\n  192 = N 1 0\n  384 = N 4 0\n  384 = N 5 0\n  576 = N 3 0\n}\n"
	writeTestFile(t, dir, "notes.chart", chartText)
	h, _, keys := startEditor(t, dir, chart.Drums)
	press(h, keys, ebiten.KeyM) // pro
	press(h, keys, ebiten.KeyM) // five-lane
	keys.down[ebiten.KeyControlLeft] = true
	press(h, keys, ebiten.Key2)
	keys.down[ebiten.KeyControlLeft] = false
	h.idle(0.1)
	h.shot("chart-editor-drums")
}

// A chart with only guitar gets a bass part: I moves to bass, the notes go into
// a new part, and saving writes it next to the guitar.
func TestAddInstrumentPartToExistingChart(t *testing.T) {
	dir := editorSong(t)
	h, e, keys := startEditor(t, dir, chart.Guitar)
	press(h, keys, ebiten.KeyI) // guitar -> bass
	if e.inst != chart.Bass || e.ed.dirty || len(e.ed.notes) != 0 {
		t.Fatalf("bass should start empty: inst %v notes %+v", e.inst, e.ed.notes)
	}
	press(h, keys, ebiten.Key1)
	press(h, keys, ebiten.Key3)
	keys.down[ebiten.KeyControlLeft] = true
	press(h, keys, ebiten.KeyS)
	keys.down[ebiten.KeyControlLeft] = false
	if e.dirty() {
		t.Fatalf("save should clear the bass work: %q", e.status)
	}
	s, _ := chart.LoadSongFolder(dir)
	c, err := s.LoadChart()
	if err != nil || s.ProjectErr != nil {
		t.Fatal(err, s.ProjectErr)
	}
	if bass := c.Track(chart.Bass, chart.Expert); bass == nil || len(bass.Notes) != 1 {
		t.Errorf("bass part not saved: %+v", bass)
	}
	if g := c.Track(chart.Guitar, chart.Expert); g == nil || len(g.Notes) != 2 {
		t.Errorf("the guitar part must be unchanged: %+v", g)
	}
}

// Unsaved work on one part survives switching to another and back.
func TestSwitchingPartsKeepsUnsavedWork(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.Key2) // remove red from the chord at 0
	before := e.ed.notes[0].Lanes
	press(h, keys, ebiten.KeyI) // bass
	press(h, keys, ebiten.Key1)
	press(h, keys, ebiten.KeyI) // rhythm
	press(h, keys, ebiten.KeyI) // guitar co-op... cycle on to find guitar again
	for e.inst != chart.Guitar {
		press(h, keys, ebiten.KeyI)
	}
	if len(e.ed.notes) == 0 || len(e.ed.notes[0].Lanes) != len(before) {
		t.Errorf("guitar work lost after switching: %+v want lanes %v", e.ed.notes, before)
	}
	if !e.dirty() {
		t.Error("unsaved work should still be marked dirty")
	}
}

func TestDifficultyKeyCyclesParts(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyD)
	if e.diff != chart.Easy {
		t.Errorf("expert -> easy by wrap, got %v", e.diff)
	}
	if !strings.Contains(e.status, "Easy") {
		t.Errorf("status %q", e.status)
	}
}

// Switching to drums changes the controller profile to the drum kit's.
func TestSwitchingToDrumsUsesTheDrumPads(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	if e.padFamily != input.FamilyGuitar {
		t.Fatalf("guitar pads first, got %v", e.padFamily)
	}
	for e.inst != chart.Drums {
		press(h, keys, ebiten.KeyI)
	}
	if e.padFamily != input.FamilyDrums || !e.ed.drums {
		t.Errorf("drum pads %v, drums %v", e.padFamily, e.ed.drums)
	}
}

// pressCtrl presses a key with Ctrl held, for one frame.
func pressCtrl(h *harness, k *scriptedKeys, key ebiten.Key) {
	k.down[ebiten.KeyControlLeft] = true
	press(h, k, key)
	k.down[ebiten.KeyControlLeft] = false
}

// In lyrics, Space ends a word; the next word starts empty and is selected.
func TestSpaceEndsAWordInLyrics(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "one")
	press(h, keys, ebiten.KeySpace)
	if e.lyr.Selected() == nil || e.lyr.Selected().Text != "" {
		t.Fatalf("Space should start an empty word, selected %+v", e.lyr.Selected())
	}
	typeChars(h, keys, "two")
	if got := e.lyr.Lines(); len(got) != 1 || got[0] != "one two" {
		t.Errorf("lines %q", got)
	}
	if e.playing() {
		t.Error("Space in lyrics must not start the song")
	}
}

// Space on an empty word does nothing, so spaces don't pile up.
func TestSpaceOnEmptyWordDoesNothing(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	press(h, keys, ebiten.KeyInsert) // an empty word
	press(h, keys, ebiten.KeySpace)
	if n := len(e.lyr.leadPart().Phrases[0].Syllables); n != 1 {
		t.Errorf("Space on an empty word added %d words", n)
	}
}

// Shift+Enter goes back one word (probe).
func TestShiftEnterGoesBack(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "one")
	press(h, keys, ebiten.KeyInsert)
	typeChars(h, keys, "two")
	keys.down[ebiten.KeyShiftLeft] = true
	press(h, keys, ebiten.KeyEnter)
	keys.down[ebiten.KeyShiftLeft] = false
	if e.lyr.Selected() == nil || e.lyr.Selected().Text != "one" {
		t.Fatalf("Shift+Enter should select the previous word, got %+v", e.lyr.Selected())
	}
}

// Paste the clipboard as lines of words, then time each word with the tap key.
func TestPasteThenTapTimesWords(t *testing.T) {
	old := clipboardText
	clipboardText = func() (string, error) { return "hello there\r\n\nsecond line", nil }
	t.Cleanup(func() { clipboardText = old })
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	pressCtrl(h, keys, ebiten.KeyV)
	if got := e.lyr.Lines(); !reflect.DeepEqual(got, []string{"hello there", "second line"}) {
		t.Fatalf("pasted lines %q", got)
	}
	if e.lyr.Selected() == nil || e.lyr.Selected().Text != "hello" {
		t.Fatalf("the first pasted word should be selected: %+v", e.lyr.Selected())
	}
	pressCtrl(h, keys, ebiten.KeySpace)
	h.idle(0.5)
	press(h, keys, ebiten.KeyEnter) // the tap key
	words := e.lyr.leadPart().Phrases[0].Syllables
	if d := words[0].T - 192; d < -24 || d > 24 {
		t.Errorf("tapped word at tick %d, want about 192", words[0].T)
	}
}

// Ctrl+K arms the editor; the next key becomes the tap key and is saved.
func TestAssignTapKey(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	pressCtrl(h, keys, ebiten.KeyK)
	if !e.assignTap {
		t.Fatal("Ctrl+K should arm the tap key")
	}
	press(h, keys, ebiten.KeyJ)
	if e.tapKey != ebiten.KeyJ || e.assignTap || h.a.Settings.LyricTapKey != "J" {
		t.Fatalf("tap key %v armed %v setting %q", e.tapKey, e.assignTap, h.a.Settings.LyricTapKey)
	}
	if got := keyNamed(h.a.Settings.LyricTapKey, ebiten.KeyEnter); got != ebiten.KeyJ {
		t.Errorf("setting reads back as %v", got)
	}
}

// Esc cancels assigning, and Space can't be a tap key (it ends a word).
func TestAssignTapKeyCancelsAndRefusesSpace(t *testing.T) {
	h, e, keys := startEditor(t, editorSong(t), chart.Guitar)
	press(h, keys, ebiten.KeyTab)
	pressCtrl(h, keys, ebiten.KeyK)
	press(h, keys, ebiten.KeyEscape)
	if e.assignTap || e.tapKey != ebiten.KeyEnter {
		t.Errorf("Esc should cancel: armed %v key %v", e.assignTap, e.tapKey)
	}
	pressCtrl(h, keys, ebiten.KeyK)
	press(h, keys, ebiten.KeySpace)
	if e.tapKey == ebiten.KeySpace {
		t.Error("Space must not become the tap key")
	}
}
