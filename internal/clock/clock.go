// Package clock provides the high resolution monotonic clock every input
// timestamp and the song clock are measured on.
package clock

var override func() float64

// Now returns seconds since the process started.
func Now() float64 {
	if override != nil {
		return override()
	}
	return now()
}

// Override replaces the clock (tests only) and returns a restore function.
func Override(f func() float64) func() {
	override = f
	return func() { override = nil }
}
