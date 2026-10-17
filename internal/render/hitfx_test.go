package render

import (
	"math"
	"testing"
)

func TestHitFxHelpers(t *testing.T) {
	// Effects are pure functions of the hit, so the same hit always draws
	// the same sparks (replays look identical).
	for i := 0; i < 50; i++ {
		a, b := hash01(12.345, 2, i), hash01(12.345, 2, i)
		if a != b || a < 0 || a >= 1 {
			t.Fatalf("hash01 not stable or out of range: %v %v", a, b)
		}
	}
	if hash01(12.345, 2, 0) == hash01(12.345, 3, 0) {
		t.Error("different lanes should get different sparks")
	}
	if d, l := punch(0); d < 10 || l < 0.99 {
		t.Errorf("punch at impact: down %.1f lit %.2f", d, l)
	}
	if d, _ := punch(0.09); d > 0 {
		t.Errorf("the punch should overshoot upward on the rebound, got %.2f", d)
	}
	if d, l := punch(punchLife + 0.01); d != 0 || l != 0 {
		t.Error("punch should be over after its life")
	}
	hits := []HitFx{{At: 1, Lane: 2}, {At: 1.2, Lane: 2}, {At: 1.25, Lane: 0}}
	if dt := lastHit(hits, 2, 1.3); math.Abs(dt-0.1) > 1e-9 {
		t.Errorf("lastHit = %v, want the most recent hit on the lane", dt)
	}
	if dt := lastHit(hits, 2, 1.1); math.Abs(dt-0.1) > 1e-9 {
		t.Errorf("lastHit must ignore hits in the future, got %v", dt)
	}
	for _, l := range []FxLevel{FxFull, FxReduced, FxOff} {
		if ParseFxLevel(l.String()) != l {
			t.Errorf("FxLevel %v doesn't round trip", l)
		}
	}
	if ParseFxLevel("") != FxFull {
		t.Error("unset hit effects should default to full")
	}
}

func TestThumpCalmsOnRuns(t *testing.T) {
	open := 0
	single := thump([]HitFx{{At: 1, Lane: open}}, open, 1.0)
	if single < 5 {
		t.Fatalf("a lone open note should still bounce: %.2f", single)
	}
	// A run of open notes 125 ms apart barely moves the highway.
	var run []HitFx
	for i := 0; i < 8; i++ {
		run = append(run, HitFx{At: 1 + float64(i)*0.125, Lane: open})
	}
	peak := 0.0
	for now := 1.875; now < 2.1; now += 0.005 {
		peak = math.Max(peak, math.Abs(thump(run, open, now)))
	}
	if peak > 0.8 {
		t.Errorf("open-note run bounces %.2f px, want under 0.8", peak)
	}
	if thump(run, 3, 1.9) != 0 {
		t.Error("other lanes don't thump")
	}
}
