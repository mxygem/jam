package input

// midiStream decodes a raw MIDI byte stream (with running status) into
// note and control change events. Used by drivers that read raw bytes.
type midiStream struct {
	status byte
	data   [2]byte
	n      int
}

// feed processes one byte and calls out for each complete message.
func (s *midiStream) feed(b byte, out func(status, d1, d2 byte)) {
	switch {
	case b >= 0xF8:
		return // realtime, may appear anywhere
	case b >= 0xF0:
		s.status = 0 // system common/sysex cancels running status
		s.n = 0
		return
	case b&0x80 != 0:
		s.status = b
		s.n = 0
		return
	}
	if s.status == 0 {
		return
	}
	s.data[s.n] = b
	s.n++
	need := 2
	if hi := s.status & 0xF0; hi == 0xC0 || hi == 0xD0 {
		need = 1
	}
	if s.n >= need {
		out(s.status, s.data[0], s.data[1])
		s.n = 0
	}
}

func midiToRaw(t float64, dev int, status, d1, d2 byte) (RawEvent, bool) {
	switch status & 0xF0 {
	case 0x90:
		if d2 > 0 {
			return RawEvent{Time: t, Src: SrcMIDI, Device: dev, Kind: KindNote, Code: int(d1), Value: float32(d2) / 127, Down: true}, true
		}
		return RawEvent{Time: t, Src: SrcMIDI, Device: dev, Kind: KindNote, Code: int(d1)}, true
	case 0x80:
		return RawEvent{Time: t, Src: SrcMIDI, Device: dev, Kind: KindNote, Code: int(d1)}, true
	case 0xB0:
		return RawEvent{Time: t, Src: SrcMIDI, Device: dev, Kind: KindCC, Code: int(d1), Value: float32(d2) / 127, Down: d2 >= 64}, true
	}
	return RawEvent{}, false
}
