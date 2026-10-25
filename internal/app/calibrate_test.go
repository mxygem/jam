package app

import (
	"math"
	"testing"

	"github.com/mxygem/jam/internal/engine"
)

// Tapping a steady 45 ms behind the clicks measures 45 ms, ignores a stray
// hit, and "Use" stores it as the audio offset.
func TestAudioCalibration(t *testing.T) {
	h := newHarness(t)
	c := NewCalibration(h.a)
	h.a.scene = c
	h.idle(0.1)
	h.frame(xbtn(bitA, true)) // green: confirm "Audio latency"
	h.frame(xbtn(bitA, false))
	if c.mode != calAudio {
		t.Fatalf("mode %v after choosing audio", c.mode)
	}
	for k := 0; k < calBeats; k++ {
		at := c.beatTime(k) + 0.045
		if k == 7 {
			at = c.beatTime(k) + 0.19 // one sloppy tap
		}
		for h.now < at {
			h.frame()
		}
		// The poller timestamps each press precisely, between frames.
		ev := xbtn(bitDPadDown, true)
		ev.Time = at
		h.frame(ev)
		h.idle(0.05)
		h.frame(xbtn(bitDPadDown, false))
	}
	h.idle(1)
	if c.mode != calDone {
		t.Fatalf("mode %v after the last beat", c.mode)
	}
	if math.Abs(c.result-45) > 3 {
		t.Fatalf("measured %.1f ms from %d taps, want about 45", c.result, len(c.offsets))
	}
	h.tap(bitA) // "Use 45 ms"
	if got := h.a.Settings.AudioOffsetMs; math.Abs(got-45) > 3 {
		t.Fatalf("audio offset saved as %v", got)
	}
	if c.mode != calMenu {
		t.Fatalf("mode %v after saving", c.mode)
	}
}

func TestVideoCalibrationCancel(t *testing.T) {
	h := newHarness(t)
	c := NewCalibration(h.a)
	h.a.scene = c
	h.frame(xbtn(bitDPadDown, true)) // strum down to "Video latency"
	h.frame(xbtn(bitDPadDown, false))
	h.tap(bitA)
	if c.mode != calVideo {
		t.Fatalf("mode %v, want video", c.mode)
	}
	h.idle(3)
	h.frame(keyDown(keyEscape))
	if c.mode != calMenu {
		t.Fatalf("Esc should cancel back to the menu, mode %v", c.mode)
	}
}

// Video calibration plays gems down the real highway: tapped gems clear,
// untapped ones scroll past without being judged, and the taps still
// measure the offset. With JAM_SHOTS it saves the screen mid-run.
func TestVideoCalibrationHighway(t *testing.T) {
	h := newHarness(t)
	c := NewCalibration(h.a)
	h.a.scene = c
	h.frame(xbtn(bitDPadDown, true)) // strum down to "Video latency"
	h.frame(xbtn(bitDPadDown, false))
	h.tap(bitA)
	if c.mode != calVideo || c.vis == nil {
		t.Fatalf("mode %v, visual %v", c.mode, c.vis != nil)
	}
	if n := len(c.vis.t.Notes); n != calBeats {
		t.Fatalf("calibration chart has %d gems, want %d", n, calBeats)
	}
	for i, n := range c.vis.t.Notes {
		if want := calLead + float64(i)*calInterval; math.Abs(n.Time-want) > 1e-6 {
			t.Fatalf("gem %d at %.4f s, want %.4f", i, n.Time, want)
		}
	}
	for k := 0; k < calBeats; k++ {
		at := c.beatTime(k) + 0.030
		for h.now < at {
			h.frame()
		}
		if k == 5 { // skip one gem: it must stay pending, not missed
			continue
		}
		ev := xbtn(bitDPadDown, true)
		ev.Time = at
		h.frame(ev)
		if k == 7 {
			h.idle(0.02)
			h.shot("calibrate-video")
		}
		h.idle(0.05)
		h.frame(xbtn(bitDPadDown, false))
		if k == 7 {
			if c.vis.eng.Status(4) != engine.NoteHit || c.vis.eng.Status(5) != engine.Pending {
				t.Fatalf("gem 4 status %v, gem 5 status %v", c.vis.eng.Status(4), c.vis.eng.Status(5))
			}
		}
	}
	h.idle(1)
	if c.mode != calDone {
		t.Fatalf("mode %v after the last beat", c.mode)
	}
	if math.Abs(c.result-30) > 3 {
		t.Fatalf("measured %.1f ms from %d taps, want about 30", c.result, len(c.offsets))
	}
}

// Every gem in the video calibration chart is an open note.
func TestVideoCalibrationOpenNotes(t *testing.T) {
	h := newHarness(t)
	v := newCalVisual(h.a)
	for i, n := range v.t.Notes {
		if !n.IsOpen() {
			t.Fatalf("gem %d is not an open note (lanes %08b)", i, n.Lanes)
		}
	}
}
