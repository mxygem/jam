package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mxygem/jam/internal/chart"
)

// discoveryChartSrc is a dense expert guitar chart with streams, chords,
// sustains and star power phrases, roughly the shape of a real 5-minute song.
func discoveryChartSrc(bars int) string {
	var sb strings.Builder
	sb.WriteString("[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = TS 4\n  0 = B 150000\n}\n[ExpertSingle]\n{\n")
	const res = 192
	const bar = res * 4
	stream := []int{0, 1, 2, 3, 2, 1, 0, 1}
	chords := [][]int{{0, 2}, {1, 3}, {0, 1, 2}, {2, 3, 4}}
	for b := 0; b < bars; b++ {
		base := b * bar
		switch b % 4 {
		case 0:
			for i := 0; i < 16; i++ {
				fmt.Fprintf(&sb, "  %d = N %d 0\n", base+i*res/4, stream[i%len(stream)])
			}
		case 1:
			for i := 0; i < 8; i++ {
				fmt.Fprintf(&sb, "  %d = N %d 0\n", base+i*res/2, stream[(i+3)%len(stream)])
			}
		case 2:
			sh := chords[(b/4)%len(chords)]
			for _, l := range sh {
				fmt.Fprintf(&sb, "  %d = N %d %d\n", base, l, res*2)
			}
			for i := 0; i < 3; i++ {
				for _, l := range sh {
					fmt.Fprintf(&sb, "  %d = N %d 0\n", base+(i+1)*res, l)
				}
			}
		case 3:
			for i := 0; i < 8; i++ {
				fmt.Fprintf(&sb, "  %d = N %d 0\n", base+i*res/2, (i*2)%5)
			}
		}
		if b%8 == 4 {
			fmt.Fprintf(&sb, "  %d = S 2 %d\n", base, bar*2)
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

func discoveryChart(b *testing.B, bars int) (*chart.Chart, *chart.Track) {
	b.Helper()
	c, err := chart.ParseDotChart(strings.NewReader(discoveryChartSrc(bars)), nil)
	if err != nil {
		b.Fatal(err)
	}
	return c, c.Track(chart.Guitar, chart.Expert)
}

// playFrames replays a script the way the game does: Advance at 60 fps
// between input events, then to the end of the song.
func playFrames(e Engine, evs script, end float64) {
	const dt = 1.0 / 60
	frame := 0.0
	for _, ev := range evs {
		for frame < ev.Time {
			e.Advance(frame)
			frame += dt
		}
		e.Process(ev)
	}
	for frame < end {
		e.Advance(frame)
		frame += dt
	}
	e.Advance(end)
}

func BenchmarkPlaySong(b *testing.B) {
	c, tr := discoveryChart(b, 200)
	evs := perfect(tr, 0.010)
	end := tr.Notes[len(tr.Notes)-1].Time + 2
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e := New(c, tr, DefaultSettings())
		playFrames(e, evs, end)
	}
}

func BenchmarkNewEngine(b *testing.B) {
	c, tr := discoveryChart(b, 200)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		New(c, tr, DefaultSettings())
	}
}

func BenchmarkParseDotChart(b *testing.B) {
	src := discoveryChartSrc(200)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := chart.ParseDotChart(strings.NewReader(src), nil); err != nil {
			b.Fatal(err)
		}
	}
}
