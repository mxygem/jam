//go:build linux

package input

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mxygem/jam/internal/clock"
)

// linuxMIDI reads ALSA raw MIDI devices (/dev/snd/midiC*D*) directly, which
// needs no cgo or ALSA sequencer bindings.
type linuxMIDI struct {
	mu      sync.Mutex
	files   []*os.File
	devices []DeviceInfo
}

func startMIDI(emit func(RawEvent)) midiSource {
	m := &linuxMIDI{}
	paths, _ := filepath.Glob("/dev/snd/midiC*D*")
	sort.Strings(paths)
	for i, p := range paths {
		name := cardName(p)
		f, err := os.Open(p)
		if err != nil {
			m.devices = append(m.devices, DeviceInfo{Src: SrcMIDI, Index: i, Name: name, Detail: "could not open: " + err.Error()})
			continue
		}
		m.files = append(m.files, f)
		m.devices = append(m.devices, DeviceInfo{Src: SrcMIDI, Index: i, Name: name})
		go func(dev int, f *os.File) {
			var s midiStream
			buf := make([]byte, 256)
			for {
				n, err := f.Read(buf)
				if err != nil {
					return
				}
				t := clock.Now()
				for _, b := range buf[:n] {
					s.feed(b, func(st, d1, d2 byte) {
						if ev, ok := midiToRaw(t, dev, st, d1, d2); ok {
							emit(ev)
						}
					})
				}
			}
		}(i, f)
	}
	return m
}

func cardName(devPath string) string {
	// /dev/snd/midiC1D0 -> /proc/asound/card1/id
	base := filepath.Base(devPath)
	card := strings.TrimPrefix(base, "midiC")
	if i := strings.Index(card, "D"); i > 0 {
		card = card[:i]
	}
	if b, err := os.ReadFile("/proc/asound/card" + card + "/id"); err == nil {
		return strings.TrimSpace(string(b)) + " (" + base + ")"
	}
	return base
}

func (m *linuxMIDI) Devices() []DeviceInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DeviceInfo(nil), m.devices...)
}

func (m *linuxMIDI) Close() {
	for _, f := range m.files {
		f.Close()
	}
	m.files = nil
}
