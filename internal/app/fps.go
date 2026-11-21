package app

import (
	"fmt"
	"log"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/render"
)

// fpsMeter measures frame pacing from one Draw to the next. It always runs
// (it's a few float operations a frame) so that a run's summary in jam.log
// is there even with the on-screen readout off.
type fpsMeter struct {
	last float64 // clock time of the previous frame

	// The readout covers the last half second.
	winStart float64
	frames   int
	worst    float64
	text     string

	// Frame intervals since startRun, for the summary when the run ends.
	recording bool
	run       []float32

	loggedGfx bool
}

const fpsWindow = 0.5

// frame records a frame drawn at now (seconds on the game clock).
func (m *fpsMeter) frame(now float64) {
	if !m.loggedGfx {
		m.loggedGfx = true
		logGraphics()
	}
	if m.last > 0 {
		dt := now - m.last
		m.frames++
		if dt > m.worst {
			m.worst = dt
		}
		if m.recording {
			m.run = append(m.run, float32(dt))
		}
	}
	m.last = now
	if m.winStart == 0 {
		m.winStart = now
	}
	if span := now - m.winStart; span >= fpsWindow {
		m.text = fpsText(float64(m.frames)/span, m.worst)
		m.winStart, m.frames, m.worst = now, 0, 0
	}
}

func fpsText(fps, worst float64) string {
	return fmt.Sprintf("%.0f FPS   max %.1f ms", fps, worst*1000)
}

// startRun begins collecting frame intervals for a song.
func (m *fpsMeter) startRun() {
	m.recording = true
	m.run = m.run[:0]
}

// endRun stops collecting and returns a one line summary for the log, or
// "" if nothing was recorded.
func (m *fpsMeter) endRun() string {
	if !m.recording {
		return ""
	}
	m.recording = false
	return frameSummary(m.run)
}

// frameSummary describes a run's frame intervals: the average rate, the
// "1% low" (the rate the slowest 1% of frames ran at), the median and worst
// frame, and how many frames took over twice the median (visible hitches).
func frameSummary(run []float32) string {
	if len(run) < 2 {
		return ""
	}
	s := append([]float32(nil), run...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	var total float64
	for _, v := range s {
		total += float64(v)
	}
	med := float64(s[len(s)/2])
	p99 := float64(s[(len(s)*99)/100])
	worst := float64(s[len(s)-1])
	hitches := 0
	for _, v := range s {
		if float64(v) > 2*med {
			hitches++
		}
	}
	return fmt.Sprintf("frames: %d in %.1f s, avg %.1f fps, 1%% low %.1f fps, median %.1f ms, worst %.1f ms, %d over 2x median",
		len(s), total, float64(len(s))/total, 1/p99, med*1000, worst*1000, hitches)
}

// logGraphics notes what the game is drawing with, once, on the first frame
// (Ebitengine only knows its graphics library once the game is running).
func logGraphics() {
	var d ebiten.DebugInfo
	ebiten.ReadDebugInfo(&d)
	mon := ebiten.Monitor()
	mw, mh := mon.Size()
	ww, wh := ebiten.WindowSize()
	log.Printf("graphics: %v, monitor %q %dx%d at scale %.2f, window %dx%d, fullscreen %v, vsync %v",
		d.GraphicsLibrary, mon.Name(), mw, mh, mon.DeviceScaleFactor(), ww, wh, ebiten.IsFullscreen(), ebiten.IsVsyncEnabled())
}

// Draw puts the readout in the window's top left corner, straight onto the
// window like the wall clock.
func (m *fpsMeter) Draw(screen *ebiten.Image) {
	if m.text == "" {
		return
	}
	const (
		size   = 22.0
		padX   = 12.0
		pillH  = 34.0
		margin = 12.0
	)
	tw, _ := render.Measure(m.text, render.CondBold, size)
	pw := tw + padX*2
	render.RoundRect(screen, margin, margin, pw, pillH, pillH/2, render.WithAlpha(th.Panel, 0.8))
	render.Text(screen, m.text, render.CondBold, size, margin+pw/2, margin+(pillH-size)/2-2, th.Text, render.Center)
}
