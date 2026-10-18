package app

import (
	"testing"
	"time"
)

func TestWallClockText(t *testing.T) {
	for _, c := range []struct {
		h, m int
		want string
	}{
		{0, 5, "12:05 AM"},
		{9, 30, "9:30 AM"},
		{12, 0, "12:00 PM"},
		{14, 57, "2:57 PM"},
		{23, 59, "11:59 PM"},
	} {
		got := wallClockText(time.Date(2026, 10, 18, c.h, c.m, 0, 0, time.Local))
		if got != c.want {
			t.Errorf("%02d:%02d = %q, want %q", c.h, c.m, got, c.want)
		}
	}
}
