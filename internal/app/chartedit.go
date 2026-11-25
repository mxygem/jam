package app

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/playback"
	"github.com/mxygem/jam/internal/render"
)

// editMode is what the editor is working on: the notes of one part, or the
// lead vocal lyrics.
type editMode uint8

const (
	modeNotes editMode = iota
	modeLyrics
)

// ChartEditor charts one part of a song on a timeline, with the song playing.
// Edits go to the song's chart.jam.json; the original chart file is never
// changed.
type ChartEditor struct {
	song *chart.Song
	inst chart.Instrument
	diff chart.Difficulty
	key  chart.TrackKey

	done      chan loadResult
	c         *chart.Chart
	au        *playback.Song
	base      *chart.TempoMap               // the project's tempo before sync points
	tm        *chart.TempoMap               // base with the sync points: ticks and song time
	anchors   []chart.Anchor                // sync points, sorted by tick
	syncDirty bool                          // sync points changed and not yet saved
	ed        *chartEdit                    // notes of the current part
	parts     map[chart.TrackKey]*chartEdit // every part opened this session, with its unsaved work
	padFamily input.Family                  // which controller profile the pads use
	frets     fretGesture                   // the guitar's frets: taps, sustains and strums
	tapKey    ebiten.Key                    // the key that times a word (Settings > lyric tap key)
	assignTap bool                          // Ctrl+K armed: the next key becomes the tap key
	lyr       *lyricEdit                    // lead vocals, loaded the first time lyrics open
	mode      editMode
	loadErr   string

	status      string
	statusUntil float64
	confirmExit bool
	last        float64 // clock at the previous frame
	viewTick    float64 // the tick at the hit line
	audioOff    float64 // the audio output latency, in seconds (as the game judges input)
	keys        editKeys
	pads        *input.Mapper // the controller's frets, strum and pads
	padBuf      []input.Mapped
}

// laneKeys are the keys that work the lane columns: 1-5, and 6 for the
// fifth lane of a five-lane kit.
var laneKeys = []ebiten.Key{ebiten.Key1, ebiten.Key2, ebiten.Key3, ebiten.Key4, ebiten.Key5, ebiten.Key6}

// Layout of the timeline, in screen pixels.
const (
	ceLaneX     = 720.0
	ceLaneSpan  = 600.0 // the width of all lane columns together
	ceHitY      = 860.0
	cePxPerBeat = 150.0
)

// editKeys is where the editor reads the keyboard. The game uses the real
// keys; tests script them.
type editKeys interface {
	just(k ebiten.Key) bool   // pressed this frame
	repeat(k ebiten.Key) bool // pressed this frame, or held long enough to repeat
	held(k ebiten.Key) bool   // down now
	typed() []rune            // characters typed this frame
}

type liveKeys struct{}

func (liveKeys) just(k ebiten.Key) bool { return inpututil.IsKeyJustPressed(k) }

func (liveKeys) held(k ebiten.Key) bool { return ebiten.IsKeyPressed(k) }

func (liveKeys) typed() []rune { return ebiten.AppendInputChars(nil) }

func (liveKeys) repeat(k ebiten.Key) bool {
	d := inpututil.KeyPressDuration(k)
	return d == 1 || (d >= 20 && (d-20)%5 == 0)
}

// NewChartEditor opens the editor for a song's part: notes (guitar-style or
// drums), lyrics and sync points.
func NewChartEditor(a *App, s *chart.Song, inst chart.Instrument, diff chart.Difficulty) *ChartEditor {
	e := &ChartEditor{song: s, inst: inst, diff: diff, key: chart.TrackKey{Inst: inst, Diff: diff}, last: clock.Now(), keys: liveKeys{}}
	e.audioOff = a.Settings.AudioOffsetMs / 1000
	fam := input.FamilyGuitar
	if inst.IsDrums() {
		fam = input.FamilyDrums
	}
	e.padFamily = fam
	e.tapKey = keyNamed(a.Settings.LyricTapKey, ebiten.KeyEnter)
	e.pads = input.NewMapper(a.Bindings.Profile(fam), a.Input.Devices())
	e.done = loadSong(a, s)
	return e
}

func (e *ChartEditor) setStatus(now float64, msg string) {
	e.status = msg
	e.statusUntil = now + 3
}

// songPos is the song time the player hears: the playback position less the
// audio latency. The view, the cursor and lyric stamps all use it.
func (e *ChartEditor) songPos() float64 {
	return max(0, e.au.Position()-e.audioOff)
}

// playing reports whether the song is playing.
func (e *ChartEditor) playing() bool { return e.au != nil && e.au.Playing() }

// dirty reports whether anything is unsaved.
func (e *ChartEditor) dirty() bool {
	if e.anyPartDirty() {
		return true
	}
	return e.syncDirty || (e.lyr != nil && e.lyr.dirty)
}

// setAnchors replaces the sync points and rebuilds the tempo map that uses them.
func (e *ChartEditor) setAnchors(points []chart.Anchor) {
	e.anchors = chart.ValidAnchors(points)
	e.tm = e.base.WithAnchors(e.anchors)
}

// addSync pins tick to song time sec. It is refused when the point would
// put time out of order with the sync points around it.
func (e *ChartEditor) addSync(tick int64, sec float64) bool {
	next := append(append([]chart.Anchor(nil), e.anchors...), chart.Anchor{Tick: tick, Sec: sec})
	valid := chart.ValidAnchors(next)
	kept := false
	for _, a := range valid {
		if a.Tick == tick && a.Sec == sec {
			kept = true
		}
	}
	if !kept {
		return false
	}
	e.setAnchors(valid)
	e.syncDirty = true
	return true
}

// syncNear finds the sync point within half a beat of tick, or -1.
func (e *ChartEditor) syncNear(tick int64) int {
	half := int64(e.ed.res) / 2
	for i, a := range e.anchors {
		if d := a.Tick - tick; d <= half && d >= -half {
			return i
		}
	}
	return -1
}

// bpmAt is the tempo in beats per minute around tick, as the sync points
// and tempo changes give it.
func (e *ChartEditor) bpmAt(tick int64) float64 {
	res := int64(e.ed.res)
	span := e.tm.Seconds(tick+res) - e.tm.Seconds(tick)
	if span <= 0 {
		return 0
	}
	return 60 / span
}

// leave stops the audio and returns to the song list. A song still loading
// is closed as soon as it arrives, so its audio isn't leaked.
func (e *ChartEditor) leave(a *App) {
	if e.au != nil {
		e.au.Pause()
		e.au.Close()
		e.au = nil
	}
	if e.c == nil && e.done != nil {
		go func(ch chan loadResult) {
			if r := <-ch; r.audio != nil {
				r.audio.Close()
			}
		}(e.done)
		e.done = nil
	}
	a.SetScene(NewSongSelect(a))
}

// partEdit returns the editing state for a part: the work already done this
// session, or the part as the chart has it (empty when the chart has none).
func (e *ChartEditor) partEdit(key chart.TrackKey) *chartEdit {
	if p, ok := e.parts[key]; ok {
		return p
	}
	p := newChartEdit(e.c.Resolution, e.c.ProjectNotes(key))
	p.drums = key.Inst.IsDrums()
	if t := e.c.Track(key.Inst, key.Diff); t != nil && p.drums {
		p.mode = t.DrumMode
	}
	e.parts[key] = p
	return p
}

// switchPart makes inst and diff the part being charted. The work on the part
// being left stays in memory until it is saved. The pads follow the part's
// family (guitar or drums).
func (e *ChartEditor) switchPart(a *App, now float64, inst chart.Instrument, diff chart.Difficulty) {
	e.inst, e.diff = inst, diff
	e.key = chart.TrackKey{Inst: inst, Diff: diff}
	e.frets = fretGesture{}
	e.ed = e.partEdit(e.key)
	fam := input.FamilyGuitar
	if inst.IsDrums() {
		fam = input.FamilyDrums
	}
	if e.padFamily != fam {
		e.padFamily = fam
		e.pads = input.NewMapper(a.Bindings.Profile(fam), a.Input.Devices())
	}
	if e.c.Track(inst, diff) == nil && !e.ed.dirty {
		e.setStatus(now, "The chart has no "+diff.Title()+" "+inst.Title()+" part yet. Notes you add create one.")
	} else {
		e.setStatus(now, "Charting "+diff.Title()+" "+inst.Title()+".")
	}
}

// nextInstrument and nextDifficulty step through the parts.
func (e *ChartEditor) nextInstrument(a *App, now float64) {
	insts := chart.Instruments()
	next := insts[0]
	for i, in := range insts {
		if in == e.inst {
			next = insts[(i+1)%len(insts)]
			break
		}
	}
	e.switchPart(a, now, next, e.diff)
}

func (e *ChartEditor) nextDifficulty(a *App, now float64) {
	e.switchPart(a, now, e.inst, (e.diff+1)%4)
}

// anyPartDirty reports whether a part has unsaved notes.
func (e *ChartEditor) anyPartDirty() bool {
	return len(e.dirtyKeys()) > 0
}

// dirtyKeys lists the parts with unsaved notes, in a stable order.
func (e *ChartEditor) dirtyKeys() []chart.TrackKey {
	var out []chart.TrackKey
	for k, p := range e.parts {
		if p.dirty {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Inst != out[j].Inst {
			return out[i].Inst < out[j].Inst
		}
		return out[i].Diff < out[j].Diff
	})
	return out
}

// save writes the notes and (when they were changed) the lyrics to the
// song's project file. A failure keeps the changes marked unsaved.
func (e *ChartEditor) save(now float64) {
	dir := e.song.Dir
	if e.anyPartDirty() {
		tracks, err := chart.LoadProjectTracks(dir)
		if err == nil {
			for _, key := range e.dirtyKeys() {
				p := e.parts[key]
				pt := chart.ProjTrack{
					Instrument: key.Inst.String(),
					Difficulty: key.Diff.String(),
					Resolution: p.res,
					Notes:      p.notes,
				}
				if p.drums {
					pt.Mode = p.mode.String()
				}
				tracks = chart.SetProjectTrack(tracks, key, pt)
			}
			err = chart.SaveProjectTracks(dir, tracks)
		}
		if err != nil {
			e.setStatus(now, "Not saved: "+err.Error())
			return
		}
		for _, p := range e.parts {
			p.dirty = false
		}
	}
	if e.syncDirty {
		if err := chart.SaveProjectSync(dir, e.anchors); err != nil {
			e.setStatus(now, "Sync points not saved: "+err.Error())
			return
		}
		e.syncDirty = false
	}
	if e.lyr != nil && e.lyr.dirty {
		pv, err := chart.LoadProjectVocals(dir)
		if err != nil {
			e.setStatus(now, "Lyrics not saved: "+err.Error())
			return
		}
		var grid *chart.ProjGrid
		if !pv.HasGrid {
			// The project's ticks need a grid to mean anything: write the chart's.
			g := e.c.ProjectGridFor(e.song.Ini)
			grid = &g
		}
		if err := chart.SaveProjectVocals(dir, e.lyr.Parts(), grid); err != nil {
			e.setStatus(now, "Lyrics not saved: "+err.Error())
			return
		}
		e.lyr.dirty = false
	}
	e.confirmExit = false
	e.setStatus(now, "Saved to "+chart.ProjectFile)
}

// togglePlay starts or stops the song. Stopping leaves the cursor where the
// song was.
func (e *ChartEditor) togglePlay() {
	if e.au == nil {
		return
	}
	if e.au.Playing() {
		e.au.Pause()
		return
	}
	if d := e.au.Duration(); d > 0 && e.au.Position() >= d-0.05 {
		e.au.Seek(0)
	}
	e.au.Play()
}

// enterLyrics opens the lyric editor, reading the lead part from the project
// file or, for a song never edited that way, from the chart's own lyrics.
func (e *ChartEditor) enterLyrics(now float64) {
	if e.lyr == nil {
		pv, err := chart.LoadProjectVocals(e.song.Dir)
		if err != nil {
			e.setStatus(now, "Lyrics can't be read: "+err.Error())
			return
		}
		parts := pv.Parts
		if len(parts) == 0 {
			parts = lyricPartsFromChart(e.c, e.tm)
		}
		e.lyr = newLyricEdit(parts, e.ed.res)
	}
	e.mode = modeLyrics
}

// lyricInsertTick is where a new word goes: where the song is, while it
// plays; stopped, one beat after the selected word, so words typed in a row
// line up instead of piling up.
func (e *ChartEditor) lyricInsertTick() int64 {
	if e.playing() {
		return int64(math.Round(e.tm.Ticks(e.songPos())))
	}
	if s := e.lyr.Selected(); s != nil {
		return s.T + int64(e.ed.res)
	}
	return int64(math.Round(e.viewTick))
}

// lyricNow is the tick a lyric stamp lands on: where the song is, while it plays.
// Stopped, Enter only moves on, so the time is the selected syllable's own.
func (e *ChartEditor) lyricNow() int64 {
	if e.playing() {
		return int64(math.Round(e.tm.Ticks(e.songPos())))
	}
	if s := e.lyr.Selected(); s != nil {
		return s.T
	}
	return int64(math.Round(e.viewTick))
}

// ctrl is Ctrl or Cmd, as the rest of the game reads it.
func (e *ChartEditor) ctrl() bool {
	return e.keys.held(ebiten.KeyControlLeft) || e.keys.held(ebiten.KeyControlRight) ||
		e.keys.held(ebiten.KeyMetaLeft) || e.keys.held(ebiten.KeyMetaRight)
}

func (e *ChartEditor) shift() bool {
	return e.keys.held(ebiten.KeyShiftLeft) || e.keys.held(ebiten.KeyShiftRight)
}

func (e *ChartEditor) Update(a *App) error {
	now := clock.Now()
	dt := now - e.last
	e.last = now
	if dt < 0 || dt > 0.5 {
		dt = 0
	}
	if e.c == nil && e.loadErr == "" {
		select {
		case r := <-e.done:
			if r.err != nil {
				e.loadErr = r.err.Error()
				return nil
			}
			e.c, e.au = r.c, r.audio
			base, anchors, err := r.c.ProjectTempos(e.song.Dir, e.song.Ini)
			if err != nil {
				base, anchors = r.c.Tempo, nil
				e.setStatus(now, "The project file can't be read: "+err.Error())
			}
			e.base = base
			e.setAnchors(anchors)
			e.parts = map[chart.TrackKey]*chartEdit{}
			e.ed = e.partEdit(e.key)
			if r.c.Track(e.inst, e.diff) == nil {
				e.setStatus(now, "This chart has no "+e.diff.Title()+" "+e.inst.Title()+" part yet. Notes you add create one.")
			}
		default:
		}
		if e.c == nil {
			if e.keys.just(ebiten.KeyEscape) {
				e.leave(a)
			}
			return nil
		}
	}
	if e.loadErr != "" {
		if e.keys.just(ebiten.KeyEscape) || e.keys.just(ebiten.KeyEnter) {
			a.SetScene(NewSongSelect(a))
		}
		return nil
	}

	if e.au != nil {
		e.au.Update(dt)
	}
	// Assigning a tap key takes the next key, so nothing else reads keys then.
	if e.assignTap {
		e.assignTapKey(a, now)
		return nil
	}
	// Keys that work the same in both modes.
	switch {
	case e.keys.just(ebiten.KeyEscape):
		if e.dirty() && !e.confirmExit {
			e.confirmExit = true
			e.setStatus(now, "Unsaved changes. Ctrl+S saves; Esc again discards them.")
			return nil
		}
		e.leave(a)
		return nil
	case e.ctrl() && e.keys.just(ebiten.KeyK) && e.mode == modeLyrics:
		e.assignTap = true
		e.setStatus(now, "Press the key to time lyrics with (Esc cancels).")
	case e.ctrl() && !e.shift() && e.keys.just(ebiten.KeyZ):
		e.undoLast(now)
	case e.ctrl() && (e.keys.just(ebiten.KeyY) || (e.shift() && e.keys.just(ebiten.KeyZ))):
		e.redoLast(now)
	case e.ctrl() && e.keys.just(ebiten.KeyS):
		e.save(now)
	case e.keys.just(ebiten.KeySpace) && (e.mode == modeNotes || e.ctrl()):
		// In lyrics, Space ends a word, so play/pause takes Ctrl+Space there.
		e.togglePlay()
	case e.keys.just(ebiten.KeyTab):
		if e.mode == modeNotes {
			e.enterLyrics(now)
		} else {
			e.mode = modeNotes
		}
	}
	e.controllerInput(a.Raw, now)
	if e.mode == modeLyrics {
		e.updateLyrics(now)
	} else {
		e.updateNotes(a, now)
	}
	if e.confirmExit && now > e.statusUntil {
		e.confirmExit = false
	}
	return nil
}

// updateNotes handles keys in the notes timeline.
func (e *ChartEditor) updateNotes(a *App, now float64) {
	if e.playing() {
		// The cursor and the view follow the song.
		pos := e.au.Position()
		e.viewTick = e.tm.Ticks(e.songPos())
		e.ed.cursor = e.ed.snap(e.viewTick)
		if d := e.au.Duration(); d > 0 && pos >= d {
			e.au.Pause()
		}
	} else {
		e.navigateNotes()
		e.viewTick = float64(e.ed.cursor)
	}
	if e.keys.repeat(ebiten.KeyBracketLeft) {
		e.ed.coarser()
	}
	if e.keys.repeat(ebiten.KeyBracketRight) {
		e.ed.finer()
	}
	for i, k := range laneKeys {
		if i < e.ed.cols() && e.keys.just(k) {
			e.laneKey(i, now)
		}
	}
	if e.keys.just(ebiten.KeyI) {
		e.nextInstrument(a, now)
	}
	if e.keys.just(ebiten.KeyD) {
		e.nextDifficulty(a, now)
	}
	if !e.ed.drums && e.keys.just(ebiten.KeyT) && !e.ctrl() {
		if !e.ed.CycleKind() {
			e.setStatus(now, "Put the cursor on a note to change its type.")
		}
	}
	if e.ed.drums && e.keys.just(ebiten.KeyX) {
		if !e.ed.ToggleTwoX() {
			e.setStatus(now, "Double kick: put the cursor on a note with a kick.")
		}
	}
	if e.ed.drums && e.keys.just(ebiten.KeyM) {
		if mode, err := e.ed.NextMode(); err != nil {
			e.setStatus(now, err.Error())
		} else {
			e.setStatus(now, "Kit: "+mode.String()+".")
		}
	}
	if e.keys.just(ebiten.KeyL) {
		switch {
		case e.ed.drums:
			e.setStatus(now, "Drums have no sustains.")
		case !e.ed.CycleSustain():
			e.setStatus(now, "No note at the cursor to give a sustain.")
		}
	}
	if e.keys.just(ebiten.KeyS) && !e.ctrl() {
		e.syncKey(now)
	}
	if e.keys.repeat(ebiten.KeyDelete) || e.keys.repeat(ebiten.KeyBackspace) {
		e.ed.DeleteAtCursor()
	}
}

// syncKey is S (add a sync point while playing) and Shift+S (remove the
// nearest one).
func (e *ChartEditor) syncKey(now float64) {
	if e.shift() {
		i := e.syncNear(e.ed.cursor)
		if i < 0 {
			e.setStatus(now, "No sync point near the cursor.")
			return
		}
		e.setAnchors(append(append([]chart.Anchor(nil), e.anchors[:i]...), e.anchors[i+1:]...))
		e.syncDirty = true
		e.setStatus(now, "Sync point removed.")
		return
	}
	if !e.playing() {
		e.setStatus(now, "Play the song and press S on the beats to add sync points.")
		return
	}
	res := int64(e.ed.res)
	beat := int64(math.Round(e.viewTick/float64(res))) * res
	if e.addSync(beat, e.songPos()) {
		e.setStatus(now, fmt.Sprintf("Sync point at beat %d, %s.", beat/res+1, fmtSongTime(e.songPos())))
	} else {
		e.setStatus(now, "That beat is out of order with the sync points around it.")
	}
}

// laneKey handles lane key i: a plain press toggles the lane at the cursor;
// Ctrl marks a drum as accent, then ghost, then plain; Shift marks a cymbal.
func (e *ChartEditor) laneKey(i int, now float64) {
	switch {
	case e.ed.drums && e.ctrl():
		if !e.ed.CycleMark(i) {
			e.setStatus(now, "Put the cursor on a note with this drum to mark it.")
		}
	case e.ed.drums && e.shift():
		if !e.ed.ToggleCymbal(i) {
			e.setStatus(now, "Cymbals: a pro kit marks yellow, blue or green on a note.")
		}
	default:
		e.ed.ToggleLane(i)
	}
}

// controllerInput turns the controller's frets, strum and pads into edits.
// Keyboard input is left to the keys above, so the keyboard's own guitar keys
// (A, S and the rest) don't also edit the chart.
func (e *ChartEditor) controllerInput(evs []input.RawEvent, now float64) {
	if e.pads == nil {
		return
	}
	for _, ev := range evs {
		if ev.Src == input.SrcKeyboard {
			continue
		}
		e.padBuf = e.pads.Map(ev, e.padBuf[:0])
		for _, mp := range e.padBuf {
			e.padInput(mp.Action, mp.Down, now)
		}
	}
}

// padInput applies one controller event. Frets go down and up (a fret's
// release is always taken, so none stays held); strum and Star Power act on press.
// Drum pads toggle their lane on press.
func (e *ChartEditor) padInput(act action.Action, down bool, now float64) {
	if !e.ed.drums {
		if l, ok := act.FretLane(); ok {
			if l >= 5 {
				return
			}
			if down && e.mode == modeNotes {
				e.fretDown(l, now)
			} else if !down {
				e.fretUp(l, now)
			}
			return
		}
	}
	if !down || e.mode != modeNotes {
		return
	}
	switch act {
	case action.StrumUp, action.StrumDown:
		if !e.ed.drums {
			e.strum(now)
		}
		return
	case action.StarPower: // Star Power (Back) steps the note type
		if !e.ed.drums && !e.ed.CycleKind() {
			e.setStatus(now, "Put the cursor on a note to change its type.")
		}
		return
	}
	if !e.ed.drums || !act.IsDrum() {
		return
	}
	lane := drumLaneOf(e.ed.mode, act)
	if lane < 0 || lane >= e.ed.cols() {
		return
	}
	e.ed.ToggleLane(lane)
	if act >= action.CymYellow && act <= action.CymGreen && e.ed.mode == chart.DrumsPro {
		e.ed.ForceCymbal(lane)
	}
}

// navigateNotes moves the cursor while the song is stopped.
func (e *ChartEditor) navigateNotes() {
	beat := e.ed.res / int(e.ed.step())
	if e.keys.repeat(ebiten.KeyArrowUp) {
		e.ed.moveSteps(1)
	}
	if e.keys.repeat(ebiten.KeyArrowDown) {
		e.ed.moveSteps(-1)
	}
	if e.keys.repeat(ebiten.KeyArrowRight) {
		e.ed.moveSteps(beat)
	}
	if e.keys.repeat(ebiten.KeyArrowLeft) {
		e.ed.moveSteps(-beat)
	}
	if e.keys.repeat(ebiten.KeyPageUp) {
		e.ed.moveSteps(4 * beat)
	}
	if e.keys.repeat(ebiten.KeyPageDown) {
		e.ed.moveSteps(-4 * beat)
	}
	if e.keys.just(ebiten.KeyHome) {
		e.ed.cursor = 0
	}
}

// updateLyrics handles keys in the lyric editor.
func (e *ChartEditor) updateLyrics(now float64) {
	if e.lyr == nil {
		return
	}
	if e.playing() {
		e.viewTick = e.tm.Ticks(e.songPos())
	} else if s := e.lyr.Selected(); s != nil {
		e.viewTick = float64(s.T)
	}
	if e.keys.just(ebiten.KeySpace) && !e.ctrl() {
		// Space ends the word being written and starts the next one.
		if s := e.lyr.Selected(); s != nil && s.Text != "" {
			e.lyr.Insert(e.lyricInsertTick())
		}
	}
	tapped := e.keys.just(e.tapKey) || (e.tapKey == ebiten.KeyEnter && e.keys.just(ebiten.KeyNumpadEnter))
	if tapped {
		switch {
		case e.ctrl():
			if !e.lyr.SplitLine() {
				e.setStatus(now, "Nothing after this word to start a new line with.")
			}
		case e.shift():
			e.lyr.move(-1)
		default:
			e.lyr.Stamp(e.lyricNow())
		}
	}
	if e.ctrl() && e.keys.just(ebiten.KeyV) {
		e.pasteLyrics(now)
	}
	if e.ctrl() && e.keys.just(ebiten.KeyBackspace) {
		if !e.lyr.JoinLine() {
			e.setStatus(now, "No line after this one to join.")
		}
	} else if e.keys.repeat(ebiten.KeyBackspace) {
		e.lyr.Backspace()
	}
	if e.keys.repeat(ebiten.KeyDelete) {
		e.lyr.DeleteSelected()
	}
	if e.keys.just(ebiten.KeyInsert) {
		e.lyr.Insert(e.lyricInsertTick())
	}
	if e.keys.repeat(ebiten.KeyArrowLeft) {
		e.lyr.move(-1)
	}
	if e.keys.repeat(ebiten.KeyArrowRight) {
		e.lyr.move(1)
	}
	if !e.ctrl() && !tapped {
		// Typed letters, digits and marks go into the selected syllable. The
		// frame that taps a letter used as the tap key types nothing.
		for _, r := range e.keys.typed() {
			if r == ' ' || !unicode.IsPrint(r) {
				continue
			}
			e.lyr.TypeText(string(r), e.lyricInsertTick())
		}
	}
}

// fmtSongTime shows a time in the song as minutes, seconds and hundredths.
func fmtSongTime(sec float64) string {
	sec = math.Max(0, sec)
	return fmt.Sprintf("%d:%05.2f", int(sec)/60, math.Mod(sec, 60))
}

// undoLast undoes the last change in the lyrics or the notes, whichever is open.
func (e *ChartEditor) undoLast(now float64) {
	if e.mode == modeLyrics && e.lyr != nil {
		if e.lyr.Undo() {
			e.setStatus(now, "Undone.")
		} else {
			e.setStatus(now, "Nothing to undo.")
		}
		return
	}
	if e.ed.Undo() {
		e.setStatus(now, "Undone.")
	} else {
		e.setStatus(now, "Nothing to undo.")
	}
}

// redoLast redoes the change undone last.
func (e *ChartEditor) redoLast(now float64) {
	if e.mode == modeLyrics && e.lyr != nil {
		if e.lyr.Redo() {
			e.setStatus(now, "Redone.")
		} else {
			e.setStatus(now, "Nothing to redo.")
		}
		return
	}
	if e.ed.Redo() {
		e.setStatus(now, "Redone.")
	} else {
		e.setStatus(now, "Nothing to redo.")
	}
}

// assignTapKey takes the next key pressed as the tap key, and saves it.
// Modifiers, Space (which ends a word) and Esc can't be tap keys.
func (e *ChartEditor) assignTapKey(a *App, now float64) {
	if e.keys.just(ebiten.KeyEscape) {
		e.assignTap = false
		e.setStatus(now, "Tap key unchanged.")
		return
	}
	for k := ebiten.Key(0); k <= ebiten.KeyMax; k++ {
		if isModifier(k) || k == ebiten.KeySpace || k == ebiten.KeyEscape || !e.keys.just(k) {
			continue
		}
		e.tapKey, e.assignTap = k, false
		a.Settings.LyricTapKey = k.String()
		if err := a.Settings.Save(); err != nil {
			e.setStatus(now, "Tap key set to "+k.String()+" (not saved: "+err.Error()+").")
			return
		}
		e.setStatus(now, "Lyrics are timed with "+k.String()+".")
		return
	}
}

// pasteLyrics adds the clipboard's lines as lyrics, one line a phrase and
// each word a syllable, spaced a beat apart from where the next word goes.
// Each word is then timed by tapping the tap key as it's sung.
func (e *ChartEditor) pasteLyrics(now float64) {
	text, err := clipboardText()
	if err != nil {
		e.setStatus(now, "The clipboard can't be read.")
		return
	}
	n := e.lyr.Paste(lyricLines(text), e.lyricInsertTick(), int64(e.ed.res))
	if n == 0 {
		e.setStatus(now, "The clipboard has no words to paste.")
		return
	}
	e.setStatus(now, fmt.Sprintf("Pasted %d words. Play the song and tap %s on each one.", n, e.tapKey.String()))
}

// keyNamed finds the key whose name is name, or def.
func keyNamed(name string, def ebiten.Key) ebiten.Key {
	for k := ebiten.Key(0); k <= ebiten.KeyMax; k++ {
		if k.String() == name {
			return k
		}
	}
	return def
}

// isModifier reports whether k is Shift, Ctrl, Alt or Meta.
func isModifier(k ebiten.Key) bool {
	switch k {
	case ebiten.KeyShiftLeft, ebiten.KeyShiftRight, ebiten.KeyControlLeft, ebiten.KeyControlRight,
		ebiten.KeyAltLeft, ebiten.KeyAltRight, ebiten.KeyMetaLeft, ebiten.KeyMetaRight:
		return true
	}
	return false
}

// tickY maps a tick to a screen height: later ticks sit higher.
func (e *ChartEditor) tickY(tick float64) float64 {
	return ceHitY - (tick-e.viewTick)/float64(e.ed.res)*cePxPerBeat
}

// Draw paints the editor.
func (e *ChartEditor) Draw(dst *ebiten.Image, a *App) {
	MenuBackground(dst)
	title := e.song.Title()
	if e.loadErr != "" {
		Header(dst, "Chart editor", title)
		render.Text(dst, e.loadErr, render.UI, 28, 96, 260, th.Bad, render.Left)
		Footer(dst, "Esc back")
		return
	}
	if e.c == nil {
		Header(dst, "Chart editor", title)
		render.Text(dst, "Loading "+e.diff.Title()+" "+e.inst.Title()+"...", render.Cond, 30, 96, 260, th.TextDim, render.Left)
		Footer(dst, "Esc back")
		return
	}

	e.drawTimeline(dst)
	if e.mode == modeLyrics {
		e.drawLyricsSidebar(dst)
		Footer(dst, "Tap "+e.tapKey.String()+" to time   Ctrl+V paste   Ctrl+K tap key   Space next word   Ctrl+Space play   Tab notes   Ctrl+S save   Esc back")
	} else {
		e.drawSidebar(dst)
		if e.ed.drums {
			Footer(dst, "Space play/pause   1-6 pads   Ctrl+1-6 accent/ghost   Shift+1-6 cymbal   X double kick   M kit   Del remove   Up/Down Left/Right move   [ ] snap   S sync (playing)   Tab lyrics   Ctrl+S save   Esc back")
		} else {
			Footer(dst, "Space play/pause   1-5 lanes   L sustain   Del remove   Up/Down Left/Right move   [ ] snap   S sync point (playing)   Shift+S remove   Tab lyrics   Ctrl+S save   Esc back")
		}
	}
}

func (e *ChartEditor) drawTimeline(dst *ebiten.Image) {
	ed := e.ed
	top, bottom := 0.0, float64(render.ScreenH-60)
	render.FillRect(dst, ceLaneX-14, top, ceLaneSpan+28, bottom, color.RGBA{0, 0, 0, 0x60})

	// Beat and bar lines.
	res := float64(ed.res)
	firstBeat := math.Floor((e.viewTick-3*res)/res) - 1
	lastBeat := math.Ceil((e.viewTick+9*res)/res) + 1
	for b := firstBeat; b <= lastBeat; b++ {
		tick := b * res
		if tick < 0 {
			continue
		}
		y := e.tickY(tick)
		if y < top || y > bottom {
			continue
		}
		sig := e.tm.SigAt(int64(tick))
		perBar := 4
		if sig.Den > 0 {
			perBar = sig.Num * 4 / sig.Den
		}
		if perBar < 1 {
			perBar = 4
		}
		bar := int(b) % perBar
		c := color.RGBA{0xff, 0xff, 0xff, 0x30}
		h := 2.0
		if bar == 0 {
			c = color.RGBA{0xff, 0xff, 0xff, 0x80}
			h = 3
		}
		render.FillRect(dst, ceLaneX-14, y, ceLaneSpan+28, h, c)
	}

	if e.mode == modeLyrics {
		e.drawLyricsTimeline(dst, bottom)
	} else {
		e.drawNotes(dst, bottom)
	}

	// Sync points.
	for _, a := range e.anchors {
		y := e.tickY(float64(a.Tick))
		if y < top || y > bottom {
			continue
		}
		render.FillRect(dst, ceLaneX-14, y-2, ceLaneSpan+28, 4, th.StarPower)
		render.Text(dst, "sync", render.Cond, 22, ceLaneX+ceLaneSpan+26, y-12, th.StarPower, render.Left)
	}

	// Cursor row and hit line.
	render.FillRect(dst, ceLaneX-14, ceHitY-2, ceLaneSpan+28, 4, th.Accent)
	if e.mode == modeNotes {
		cy := e.tickY(float64(ed.cursor))
		render.StrokeRect(dst, ceLaneX-14, cy-18, ceLaneSpan+28, 36, 2, th.StarPower)
	}
	for l := 0; l < ed.cols(); l++ {
		render.Text(dst, e.laneName(l), render.CondBold, 26,
			e.colCenter(l), ceHitY+24, e.laneColor(l), render.Center)
	}
}

// drawNotes paints the notes, their sustains and, for drums, their marks.
func (e *ChartEditor) drawNotes(dst *ebiten.Image, bottom float64) {
	ed := e.ed
	for i := range ed.notes {
		n := &ed.notes[i]
		y := e.tickY(float64(n.Tick))
		if y < -60 || y > bottom+60 {
			continue
		}
		if n.Length > 0 {
			yEnd := e.tickY(float64(n.Tick + n.Length))
			for _, l := range n.Lanes {
				if l == chart.LaneOpen && !ed.drums {
					continue
				}
				render.FillRect(dst, e.colCenter(l)-7, yEnd, 14, y-yEnd, render.WithAlpha(e.laneColor(l), 0.6))
			}
		}
		for _, l := range n.Lanes {
			x := e.colCenter(l)
			if l == chart.LaneOpen && !ed.drums {
				render.RoundRect(dst, ceLaneX, y-10, ceLaneSpan, 20, 10, render.WithAlpha(th.TextDim, 0.7))
				continue
			}
			render.RoundRect(dst, x-34, y-16, 68, 32, 9, e.laneColor(l))
			switch n.Kind {
			case "hopo":
				render.Text(dst, "H", render.CondBold, 22, x, y-13, th.Text, render.Center)
			case "tap":
				render.Text(dst, "T", render.CondBold, 22, x, y-13, th.Text, render.Center)
			}
			if ed.drums {
				e.drawDrumMarks(dst, n, l, x, y)
			}
		}
	}
}

// drawDrumMarks writes a drum's cymbal, accent and ghost marks on its gem, and
// the double kick on a kick.
func (e *ChartEditor) drawDrumMarks(dst *ebiten.Image, n *chart.ProjNote, l int, x, y float64) {
	var marks string
	if hasLane(n.Cymbals, l) {
		marks += "C"
	}
	if hasLane(n.Accents, l) {
		marks += "A"
	}
	if hasLane(n.Ghosts, l) {
		marks += "g"
	}
	if l == chart.DrumKick && n.TwoX {
		marks += "2x"
	}
	if marks != "" {
		render.Text(dst, marks, render.CondBold, 22, x, y-13, th.Text, render.Center)
	}
}

// laneName is the label of a lane column: the fret letter, or the drum pad.
func (e *ChartEditor) laneName(l int) string {
	if !e.ed.drums {
		return []string{"G", "R", "Y", "B", "O", "open"}[min(l, 5)]
	}
	switch l {
	case chart.DrumKick:
		return "K"
	case chart.DrumRed:
		return "R"
	case chart.DrumYellow:
		return "Y"
	case chart.DrumBlue:
		return "B"
	case chart.DrumOrange:
		if e.ed.mode == chart.DrumsFiveLane {
			return "O"
		}
	}
	return "G"
}

// laneColor is the colour of a lane column: the fret colours for a guitar,
// and the matching colours for drum pads (kick is the accent colour).
func (e *ChartEditor) laneColor(l int) color.Color {
	if !e.ed.drums {
		return th.Lanes[l]
	}
	switch {
	case l == chart.DrumKick:
		return th.Accent
	case l == chart.DrumOrange && e.ed.mode == chart.DrumsFiveLane:
		return th.Lanes[4]
	case l >= 1 && l <= 3:
		return th.Lanes[l]
	}
	return th.Lanes[0]
}

// colW is the width of one lane column; a five-lane kit has six.
func (e *ChartEditor) colW() float64 { return ceLaneSpan / float64(e.ed.cols()) }

// colX is the left edge of lane column l.
func (e *ChartEditor) colX(l int) float64 { return ceLaneX + float64(l)*e.colW() }

// colCenter is the middle of lane column l.
func (e *ChartEditor) colCenter(l int) float64 { return e.colX(l) + e.colW()/2 }

// drawLyricsTimeline shows each syllable at its time, the selected one lit.
func (e *ChartEditor) drawLyricsTimeline(dst *ebiten.Image, bottom float64) {
	x := ceLaneX + ceLaneSpan/2
	for p, ph := range e.lyr.leadPart().Phrases {
		y0 := e.tickY(float64(ph.Start))
		y1 := e.tickY(float64(ph.End))
		if y1 <= bottom && y0 >= -60 {
			// A thin bar on the left marks each line's span.
			render.FillRect(dst, ceLaneX-40, y1, 6, math.Max(2, y0-y1), render.WithAlpha(th.TextDim, 0.5))
		}
		for s, syl := range ph.Syllables {
			y := e.tickY(float64(syl.T))
			if y < -40 || y > bottom+40 {
				continue
			}
			text := syl.Text
			if text == "" {
				text = "_"
			}
			if e.lyr.sel == (lyricPos{p, s}) {
				w, _ := render.Measure(text, render.UIBold, 34)
				render.RoundRect(dst, x-w/2-14, y-22, w+28, 44, 10, render.WithAlpha(th.Accent, 0.85))
				render.Text(dst, text, render.UIBold, 34, x, y-18, th.Text, render.Center)
			} else {
				render.Text(dst, text, render.UI, 30, x, y-16, th.Text, render.Center)
			}
		}
	}
}

func (e *ChartEditor) drawSidebar(dst *ebiten.Image) {
	ed := e.ed
	x, y := 96.0, 230.0
	Header(dst, "Chart editor", e.song.Title())
	part := e.diff.Title() + " " + e.inst.Title()
	render.Text(dst, part, render.UIBold, 34, x, y, th.Text, render.Left)
	y += 56

	line := func(label, value string) {
		render.Text(dst, label, render.Cond, 26, x, y, th.TextDim, render.Left)
		render.Text(dst, value, render.UIBold, 28, x+260, y, th.Text, render.Left)
		y += 40
	}
	beats := float64(ed.cursor) / float64(ed.res)
	if ed.drums {
		line("Kit", ed.mode.String())
	}
	line("Snap", snapLabel(ed.div()))
	line("Beat", fmt.Sprintf("%.2f", beats+1))
	line("Time", fmtSongTime(e.tm.Seconds(ed.cursor)))
	line("Notes", fmt.Sprintf("%d", len(ed.notes)))
	line("Tempo here", fmt.Sprintf("%.1f BPM", e.bpmAt(ed.cursor)))
	line("Sync points", fmt.Sprintf("%d", len(e.anchors)))
	if n := ed.NoteAtCursor(); n != nil {
		desc := ""
		for _, l := range n.Lanes {
			if l == chart.LaneOpen {
				desc += "open "
				continue
			}
			desc += e.laneName(l)
		}
		if n.Length > 0 {
			desc += fmt.Sprintf("  sustain %.1f beats", float64(n.Length)/float64(ed.res))
		}
		line("At cursor", desc)
	} else {
		line("At cursor", "empty")
	}
	e.drawFileState(dst, x, y)
}

// drawFileState writes whether the notes and lyrics are saved, and the status line.
func (e *ChartEditor) drawFileState(dst *ebiten.Image, x, y float64) {
	saved := "saved"
	if e.dirty() {
		saved = "unsaved changes"
	}
	render.Text(dst, "File", render.Cond, 26, x, y, th.TextDim, render.Left)
	render.Text(dst, saved, render.UIBold, 28, x+260, y, th.Text, render.Left)
	if e.playing() {
		render.Text(dst, "Song playing", render.Cond, 26, x, y+40, th.TextDim, render.Left)
	}
	if e.status != "" && clock.Now() < e.statusUntil {
		drawWrapped(dst, e.status, render.UI, 26, x, 700, 640, th.Accent)
	}
}

// drawLyricsSidebar shows the lines around the selection, with the selected
// word lit, and the selected word's details.
func (e *ChartEditor) drawLyricsSidebar(dst *ebiten.Image) {
	x, y := 96.0, 230.0
	Header(dst, "Chart editor", e.song.Title())
	render.Text(dst, "Lead vocals", render.UIBold, 34, x, y, th.Text, render.Left)
	y += 56

	selPhrase := e.lyr.sel.phrase
	phrases := e.lyr.leadPart().Phrases
	first := max(0, selPhrase-3)
	last := min(len(phrases), first+7)
	for p := first; p < last; p++ {
		ph := phrases[p]
		cx := x
		for s, syl := range ph.Syllables {
			text := syl.Text
			if text == "" {
				text = "_"
			}
			c := th.TextDim
			if p == selPhrase {
				c = th.Text
			}
			if e.lyr.sel == (lyricPos{p, s}) {
				c = th.Accent
			}
			render.Text(dst, text, render.UIBold, 30, cx, y, c, render.Left)
			w, _ := render.Measure(text, render.UIBold, 30)
			cx += w
			if !syl.Join && s < len(ph.Syllables)-1 {
				sp, _ := render.Measure(" ", render.UIBold, 30)
				cx += sp
			}
		}
		y += 46
	}
	if len(phrases) == 0 {
		render.Text(dst, "No lyrics yet. Press Ins to add a word, then type it.", render.UI, 26, x, y, th.TextDim, render.Left)
	}

	y = 610
	if s := e.lyr.Selected(); s != nil {
		line := func(label, value string) {
			render.Text(dst, label, render.Cond, 26, x, y, th.TextDim, render.Left)
			render.Text(dst, value, render.UIBold, 28, x+260, y, th.Text, render.Left)
			y += 40
		}
		word := s.Text
		if word == "" {
			word = "(empty)"
		}
		line("Word", word)
		line("Time", fmtSongTime(e.tm.Seconds(s.T)))
		join := "no"
		if s.Join {
			join = "yes (no space after)"
		}
		line("Joins next", join)
		if len(s.FX) > 0 {
			line("Effects", fmt.Sprintf("%d kept as written", len(s.FX)))
		}
	}
	e.drawFileState(dst, x, y+10)

	hx, hy := 1380.0, 230.0
	render.Text(dst, "How to write lyrics", render.CondBold, 26, hx, hy, th.TextDim, render.Left)
	hy += 44
	help := []string{
		"Play the song and tap " + e.tapKey.String() + " as each word is sung. The word is timed where the song is, and the next word is selected.",
		"Stopped, the tap key moves to the next word. Shift with it goes back one word.",
		"Type to change the selected word. A hyphen at the end joins it to the next word (for example hel-).",
		"Paste lyrics with Ctrl+V (a line per phrase) and time them by tapping. Space ends a word and starts the next one. Ctrl+Enter starts a new line after the selected word; Ctrl+Backspace joins the line to the next. Ctrl+Space plays and pauses.",
		"Effects on words stay exactly as they were in the file.",
	}
	for _, h := range help {
		lines := wrapText(h, render.Cond, 24, 500)
		drawWrapped(dst, h, render.Cond, 24, hx, hy, 500, th.TextDim)
		hy += float64(len(lines))*32 + 16
	}
}
