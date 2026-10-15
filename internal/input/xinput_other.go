//go:build !windows

package input

// XInput only exists on Windows; elsewhere guitars come through Ebitengine's
// gamepad support.
func startXInput(emit func(RawEvent)) xinputSource { return nil }
