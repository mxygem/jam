package app

import (
	"math"
	"testing"
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
