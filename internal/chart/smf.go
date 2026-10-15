package chart

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// smfEvent is a decoded event with an absolute tick.
type smfEvent struct {
	tick   int64
	status byte // 0x80-0xEF channel events, 0xF0/0xF7 sysex, 0xFF meta
	meta   byte // meta type when status == 0xFF
	data1  byte
	data2  byte
	data   []byte // meta / sysex payload
}

func (e smfEvent) isNoteOn() bool  { return e.status&0xF0 == 0x90 && e.data2 > 0 }
func (e smfEvent) isNoteOff() bool { return e.status&0xF0 == 0x80 || (e.status&0xF0 == 0x90 && e.data2 == 0) }
func (e smfEvent) channel() byte   { return e.status & 0x0F }
func (e smfEvent) isText() bool    { return e.status == 0xFF && e.meta >= 0x01 && e.meta <= 0x0F }

type smfTrack struct {
	name   string
	events []smfEvent
}

type smfFile struct {
	format     int
	resolution int
	tracks     []smfTrack
}

var errNotMIDI = errors.New("not a standard MIDI file")

func parseSMF(b []byte) (*smfFile, error) {
	if len(b) < 14 || string(b[:4]) != "MThd" {
		return nil, errNotMIDI
	}
	hlen := int(binary.BigEndian.Uint32(b[4:8]))
	if hlen < 6 || 8+hlen > len(b) {
		return nil, fmt.Errorf("midi: bad header length %d", hlen)
	}
	f := &smfFile{
		format: int(binary.BigEndian.Uint16(b[8:10])),
	}
	div := binary.BigEndian.Uint16(b[12:14])
	if div&0x8000 != 0 {
		return nil, errors.New("midi: SMPTE time division is not supported")
	}
	f.resolution = int(div)
	pos := 8 + hlen
	for pos+8 <= len(b) {
		typ := string(b[pos : pos+4])
		n := int(binary.BigEndian.Uint32(b[pos+4 : pos+8]))
		pos += 8
		if n < 0 || pos+n > len(b) {
			n = len(b) - pos // tolerate truncated final chunk
		}
		chunk := b[pos : pos+n]
		pos += n
		if typ != "MTrk" {
			continue
		}
		t, err := parseTrack(chunk)
		if err != nil {
			return nil, fmt.Errorf("midi track %d: %w", len(f.tracks), err)
		}
		f.tracks = append(f.tracks, t)
	}
	return f, nil
}

func readVLQ(b []byte, pos int) (int64, int, error) {
	var v int64
	for i := 0; i < 4; i++ {
		if pos >= len(b) {
			return 0, pos, errors.New("truncated variable-length value")
		}
		c := b[pos]
		pos++
		v = v<<7 | int64(c&0x7F)
		if c&0x80 == 0 {
			return v, pos, nil
		}
	}
	return v, pos, nil
}

func parseTrack(b []byte) (smfTrack, error) {
	var t smfTrack
	var tick int64
	var running byte
	pos := 0
	for pos < len(b) {
		delta, p, err := readVLQ(b, pos)
		if err != nil {
			return t, err
		}
		pos = p
		tick += delta
		if pos >= len(b) {
			break
		}
		status := b[pos]
		if status < 0x80 {
			// Running status. Some charts keep running status across meta and
			// sysex events, which the spec says resets it; tolerate both.
			if running == 0 {
				return t, fmt.Errorf("data byte without status at offset %d", pos)
			}
			status = running
		} else {
			pos++
		}
		switch {
		case status == 0xFF:
			if pos >= len(b) {
				return t, errors.New("truncated meta event")
			}
			mt := b[pos]
			pos++
			n, p, err := readVLQ(b, pos)
			if err != nil {
				return t, err
			}
			pos = p
			end := pos + int(n)
			if end > len(b) {
				end = len(b)
			}
			ev := smfEvent{tick: tick, status: 0xFF, meta: mt, data: b[pos:end]}
			pos = end
			if mt == 0x2F {
				t.events = append(t.events, ev)
				return t, nil
			}
			if mt == 0x03 && t.name == "" {
				t.name = string(ev.data)
			}
			t.events = append(t.events, ev)
		case status == 0xF0 || status == 0xF7:
			n, p, err := readVLQ(b, pos)
			if err != nil {
				return t, err
			}
			pos = p
			end := pos + int(n)
			if end > len(b) {
				end = len(b)
			}
			t.events = append(t.events, smfEvent{tick: tick, status: status, data: b[pos:end]})
			pos = end
		case status >= 0x80 && status < 0xF0:
			running = status
			need := 2
			if hi := status & 0xF0; hi == 0xC0 || hi == 0xD0 {
				need = 1
			}
			if pos+need > len(b) {
				return t, errors.New("truncated channel event")
			}
			ev := smfEvent{tick: tick, status: status, data1: b[pos]}
			if need == 2 {
				ev.data2 = b[pos+1]
			}
			pos += need
			t.events = append(t.events, ev)
		default:
			// System common/realtime bytes are not valid in files; skip one byte.
		}
	}
	return t, nil
}

// midiNote is a paired note on/off.
type midiNote struct {
	start, end int64
	note       int
	vel        int
	channel    int
}

// pairNotes turns note on/off events into notes. Overlapping notes with the
// same number are closed when the next one starts.
func pairNotes(events []smfEvent) []midiNote {
	type key struct{ ch, note byte }
	open := map[key]int{}
	var out []midiNote
	for _, e := range events {
		switch {
		case e.isNoteOn():
			k := key{e.channel(), e.data1}
			if i, ok := open[k]; ok {
				out[i].end = e.tick
			}
			out = append(out, midiNote{start: e.tick, end: -1, note: int(e.data1), vel: int(e.data2), channel: int(e.channel())})
			open[k] = len(out) - 1
		case e.isNoteOff():
			k := key{e.channel(), e.data1}
			if i, ok := open[k]; ok {
				out[i].end = e.tick
				delete(open, k)
			}
		}
	}
	for i := range out {
		if out[i].end < out[i].start {
			out[i].end = out[i].start
		}
	}
	return out
}
