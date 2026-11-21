package app

import (
	"strings"
	"testing"
)

func TestFPSMeterReadout(t *testing.T) {
	m := fpsMeter{loggedGfx: true}
	now := 10.0
	for i := 0; i < 40; i++ {
		dt := 1.0 / 60
		if i == 20 {
			dt = 0.05 // one hitch
		}
		now += dt
		m.frame(now)
	}
	if m.text == "" {
		t.Fatal("no readout after half a second")
	}
	if !strings.Contains(m.text, "max 50.0 ms") {
		t.Fatalf("readout %q doesn't show the 50 ms frame", m.text)
	}
}

func TestFrameSummary(t *testing.T) {
	run := make([]float32, 0, 600)
	for i := 0; i < 600; i++ {
		run = append(run, 1.0/60)
	}
	run[100], run[200] = 0.1, 0.05
	s := frameSummary(run)
	for _, want := range []string{"frames: 600", "worst 100.0 ms", "2 over 2x median", "median 16.7 ms"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary %q lacks %q", s, want)
		}
	}
	if frameSummary(nil) != "" {
		t.Error("empty run should give no summary")
	}
	var m fpsMeter
	if m.endRun() != "" {
		t.Error("endRun without startRun should be empty")
	}
}
