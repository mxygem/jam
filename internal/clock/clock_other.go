//go:build !windows

package clock

import "time"

var start = time.Now()

func now() float64 { return time.Since(start).Seconds() }
