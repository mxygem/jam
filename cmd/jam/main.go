// Command jam is a rhythm game for guitar, bass and drums controllers.
//
//	jam                       start the game
//	jam analyze <run file>    print the analysis of a recorded playthrough
//	jam probe [seconds]       print raw XInput / MIDI events (no window)
//	jam check <folder>...     parse every chart in the folders and report problems
//	jam demo <folder>         write the built-in demo song
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mxygem/jam/internal/app"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/demo"
	"github.com/mxygem/jam/internal/engine"
	"github.com/mxygem/jam/internal/input"
	"github.com/mxygem/jam/internal/library"
	"github.com/mxygem/jam/internal/replay"
	"github.com/mxygem/jam/internal/rich"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "analyze":
			os.Exit(analyze(os.Args[2:]))
		case "probe":
			os.Exit(probe(os.Args[2:]))
		case "check":
			os.Exit(check(os.Args[2:]))
		case "demo":
			os.Exit(writeDemo(os.Args[2:]))
		case "version":
			fmt.Println("jam", app.Version)
			return
		}
	}
	var o app.Options
	flag.StringVar(&o.Song, "song", "", "song folder to play immediately")
	flag.StringVar(&o.Instrument, "instrument", "guitar", "instrument for --song (guitar, bass, rhythm, keys, drums)")
	flag.StringVar(&o.Difficulty, "difficulty", "expert", "difficulty for --song")
	flag.BoolVar(&o.Autoplay, "autoplay", false, "let a bot play the song perfectly")
	flag.StringVar(&o.Screenshot, "screenshot", "", "write a PNG of the screen and exit")
	flag.Float64Var(&o.ShotAt, "at", 10, "song time (seconds) for --screenshot")
	flag.StringVar(&o.ShotScene, "scene", "", "scene for --screenshot without --song: menu, select, wizard")
	flag.BoolVar(&o.Windowed, "windowed", false, "ignore the fullscreen setting")
	flag.StringVar(&o.WindowSize, "window", "", "window size, e.g. 3840x1080 (for screenshots of other screen shapes)")
	flag.Parse()
	if o.Screenshot != "" && o.Song == "" && o.ShotScene == "" {
		o.ShotScene = "menu"
	}
	if err := app.Run(o); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func analyze(args []string) int {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	window := fs.Float64("window", 0, "re-simulate with this hit window in ms (each side)")
	chartDir := fs.String("chart", "", "song folder to analyze against (default: the folder the run was played from)")
	fs.Parse(args)
	if fs.NArg() == 0 {
		files, _ := replay.List(config.RunsDir())
		if len(files) == 0 {
			fmt.Fprintln(os.Stderr, "usage: jam analyze <run file>   (no runs found in", config.RunsDir()+")")
			return 2
		}
		fmt.Println("Most recent run:", files[0])
		fs = flag.NewFlagSet("analyze", flag.ExitOnError)
		args = append(args, files[0])
		fs.Float64Var(window, "window", *window, "")
		fs.StringVar(chartDir, "chart", *chartDir, "")
		fs.Parse(args)
	}
	rec, err := replay.Load(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	dir := rec.Song.Dir
	if *chartDir != "" {
		dir = *chartDir
	}
	song, err := chart.LoadSongFolder(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chart folder %s unavailable (%v); showing the summary only (use --chart <folder>)\n", dir, err)
		fmt.Printf("%+v\n", rec.Result)
		return 0
	}
	if h := song.ChartHash(); h != "" && rec.Song.ChartHash != "" && h != rec.Song.ChartHash {
		fmt.Println("Note: the chart has changed since this run was recorded; analysis uses the current file.")
	}
	c, err := song.LoadChart()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	inst, _ := chart.ParseInstrument(rec.Instrument)
	diff, _ := chart.ParseDifficulty(rec.Difficulty)
	t := c.Track(inst, diff)
	if t == nil {
		fmt.Fprintln(os.Stderr, "track not found in chart")
		return 1
	}
	fmt.Print(replay.Analyze(rec, c, t).Text())
	if *window > 0 {
		set := rec.Settings
		set.EarlyWindow, set.LateWindow = *window/1000, *window/1000
		e := replay.Resimulate(c, t, set, rec.Events(), rec.EndTime)
		fmt.Printf("\nWith a ±%.0f ms window the same inputs would hit %d/%d (was %d) and score %.0f (was %d).\n",
			*window, e.Stats().NotesHit, e.Stats().NotesTotal, rec.Result.NotesHit, e.Stats().Score, rec.Result.Score)
	}
	return 0
}

func probe(args []string) int {
	secs := 20.0
	if len(args) > 0 {
		fmt.Sscan(args[0], &secs)
	}
	m := input.NewBackground()
	defer m.Close()
	time.Sleep(300 * time.Millisecond)
	fmt.Println("Devices:")
	for _, d := range m.Devices() {
		if d.Src != input.SrcKeyboard {
			fmt.Println("  ", d)
		}
	}
	fmt.Printf("Listening for %.0fs (XInput and MIDI only)...\n", secs)
	end := clock.Now() + secs
	var evs []input.RawEvent
	last := 0.0
	for clock.Now() < end {
		evs = m.PollQueue(evs[:0])
		for _, e := range evs {
			gap := ""
			if last > 0 {
				gap = fmt.Sprintf("+%7.2fms", (e.Time-last)*1000)
			}
			last = e.Time
			fmt.Printf("%9.4f %s  %s\n", e.Time, gap, m.Describe(e))
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 0
}

func check(args []string) int {
	roots := args
	if len(roots) == 0 {
		s, _ := config.Load()
		roots = s.LibraryPaths
	}
	songs := chart.ScanLibrary(roots)
	fmt.Printf("%d song folders under %s\n", len(songs), strings.Join(roots, ", "))
	insts := map[string]int{}
	modes := map[string]int{}
	exts := map[string]int{}
	bad, lyrics, oddSyl, rated, art := 0, 0, 0, 0, 0
	start := time.Now()
	for _, s := range songs {
		c, err := s.LoadChart()
		if err != nil {
			bad++
			fmt.Printf("ERROR %s: %v\n", s.ChartPath, err)
			continue
		}
		for _, i := range c.Instruments() {
			insts[i.String()]++
		}
		if t := c.Track(chart.Drums, chart.Expert); t != nil {
			modes[t.DrumMode.String()]++
		}
		for _, p := range s.Stems {
			exts[strings.ToLower(filepath.Ext(p))]++
		}
		if len(s.Stems) == 0 {
			fmt.Printf("WARN  %s: no audio files\n", s.Dir)
		}
		if len(c.Lyrics) > 0 {
			lyrics++
			for _, ph := range c.Lyrics {
				for _, sy := range ph.Syllables {
					if strings.ContainsAny(rich.Plain(sy.Text), "+#^*%$/§_=") {
						oddSyl++
						if oddSyl <= 10 {
							fmt.Printf("LYRIC %s: syllable %q at %.2f\n", s.Dir, sy.Text, sy.Time)
						}
					}
				}
			}
		}
		if in := library.Analyze(s); in.Err != "" {
			fmt.Printf("ERROR %s: index: %s\n", s.Dir, in.Err)
		} else {
			for _, p := range in.Parts {
				if p.Fw != nil {
					rated++
				}
			}
		}
		if s.CoverArt != "" {
			art++
		}
		// Make sure every track plays through the engine without trouble.
		for _, t := range c.Tracks {
			e := engine.New(c, t, engine.DefaultSettings())
			e.Advance(t.Notes[len(t.Notes)-1].SustainEnd() + 1)
			if e.Stats().NotesMissed != e.Stats().NotesTotal {
				fmt.Printf("ERROR %s %s: engine resolved %d of %d notes\n", s.Dir, t.Key, e.Stats().NotesMissed, e.Stats().NotesTotal)
			}
		}
	}
	fmt.Printf("parsed %d/%d charts in %v\n", len(songs)-bad, len(songs), time.Since(start).Round(time.Millisecond))
	printMap := func(title string, m map[string]int) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
		}
		fmt.Printf("%s: %s\n", title, strings.Join(parts, ", "))
	}
	fmt.Printf("lyrics in %d charts (%d syllables with leftover symbols), album art in %d folders, %d parts with a Fretwork rating\n", lyrics, oddSyl, art, rated)
	printMap("instruments", insts)
	printMap("drum types", modes)
	printMap("audio formats", exts)
	if bad > 0 {
		return 1
	}
	return 0
}

func writeDemo(args []string) int {
	dir := config.SongsDir()
	if len(args) > 0 {
		dir = args[0]
	}
	f, err := demo.Write(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(f)
	return 0
}
