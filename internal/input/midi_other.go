//go:build !windows && !linux

package input

// MIDI input on macOS (CoreMIDI) is not implemented yet.
type noMIDI struct{}

func startMIDI(emit func(RawEvent)) midiSource { return noMIDI{} }

func (noMIDI) Devices() []DeviceInfo { return nil }
func (noMIDI) Close()                {}
