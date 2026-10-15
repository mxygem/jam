// Package clock provides the high resolution monotonic clock every input
// timestamp and the song clock are measured on.
package clock

// Now returns seconds since the process started.
func Now() float64 { return now() }
