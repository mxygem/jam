package input

// xinputSource is implemented by the Windows XInput poller.
type xinputSource interface {
	Devices() []DeviceInfo
	Axis(slot, axis int) (float32, bool)
	Button(slot, bit int) bool
	Close()
}

// midiSource is implemented per platform.
type midiSource interface {
	Devices() []DeviceInfo
	Close()
}
