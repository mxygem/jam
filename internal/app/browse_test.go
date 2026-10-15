package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/library"
)

type testSong struct {
	name, artist, album, charter, genre, year, icon string
	parts                                           []chart.Instrument
	lyrics                                          bool
	lengthMs                                        int64
	fwD                                             float64
	track                                           int
	sub                                             string // folder under the root
}

func testLibrary(t *testing.T, defs []testSong) ([]*chart.Song, *library.Index) {
	t.Helper()
	x := library.Open(filepath.Join(t.TempDir(), "none.json.gz"))
	root := filepath.FromSlash("/lib/in-progress")
	var songs []*chart.Song
	for _, d := range defs {
		s := &chart.Song{Name: d.name, Artist: d.artist, Album: d.album, Charter: d.charter, Genre: d.genre, Year: d.year,
			Icon: d.icon, AlbumTrack: d.track, Root: root, Dir: filepath.Join(root, d.sub, d.artist+" - "+d.name)}
		in := &library.Info{LengthMs: d.lengthMs, Lyrics: d.lyrics}
		for _, p := range d.parts {
			in.Parts = append(in.Parts, library.Part{Inst: p, Diffs: []chart.Difficulty{chart.Expert}, Fw: &library.Fretwork{D: d.fwD}})
		}
		x.Put(s, in)
		songs = append(songs, s)
	}
	return songs, x
}

func rowsText(rows []row) string {
	var b strings.Builder
	for _, r := range rows {
		switch {
		case r.header != "" && r.sub:
			b.WriteString("  [" + r.header + "] ")
		case r.header != "":
			b.WriteString("[" + r.header + "] ")
		default:
			b.WriteString(r.song.Name + " ")
		}
	}
	return strings.TrimSpace(b.String())
}

var gtr, bass, drums = chart.Guitar, chart.Bass, chart.Drums

func defaultLib(t *testing.T) ([]*chart.Song, *library.Index) {
	return testLibrary(t, []testSong{
		{name: "Neon", artist: "John Mayer", album: "Room for Squares", charter: "<color=#33ccff><b>gem</b></color>", genre: "Pop", year: "2001", icon: "custom",
			parts: []chart.Instrument{gtr, bass, drums}, lyrics: true, lengthMs: 264000, fwD: 9.5, track: 4, sub: "Mayer"},
		{name: "Why Georgia", artist: "John Mayer", album: "Room for Squares", charter: "someone", genre: "Pop", year: "2001",
			parts: []chart.Instrument{gtr}, lengthMs: 270000, fwD: 8, track: 2, sub: "Mayer"},
		{name: "Only Human", artist: "Tides of Man", album: "Empire Theory", charter: "XEntombmentX", genre: "Post-hardcore", year: "2010", icon: "rb3",
			parts: []chart.Instrument{gtr, chart.Rhythm}, lengthMs: 274000, fwD: 13},
		{name: "Crow", artist: "김진산", charter: "gem", parts: []chart.Instrument{bass}, lengthMs: 95000, fwD: 6},
		{name: "Untitled", artist: "", year: "", parts: []chart.Instrument{drums}, lengthMs: 0},
	})
}

func browse(songs []*chart.Song, x *library.Index, set config.Browse, search string) string {
	b := newBrowser()
	ctx := &browseCtx{index: x, inst: chart.Guitar, scale: set.DiffScale, now: time.Now(), search: search}
	if ctx.scale == "" {
		ctx.scale = "fretwork"
	}
	return rowsText(b.buildRows(songs, set, ctx))
}

func TestSortAndGroup(t *testing.T) {
	songs, x := defaultLib(t)
	cases := []struct {
		set  config.Browse
		want string
	}{
		{config.Browse{Sort: "artist"}, "[John Mayer] Neon Why Georgia [Tides of Man] Only Human [김진산] Crow [Unknown artist] Untitled"},
		// reversing reverses the groups; songs inside a group stay A to Z
		{config.Browse{Sort: "artist", Desc: true}, "[김진산] Crow [Tides of Man] Only Human [John Mayer] Neon Why Georgia [Unknown artist] Untitled"},
		// album track order inside an album
		{config.Browse{Sort: "artist_album"}, "[John Mayer]   [Room for Squares] Why Georgia Neon [Tides of Man]   [Empire Theory] Only Human [김진산]   [Unknown album] Crow [Unknown artist]   [Unknown album] Untitled"},
		{config.Browse{Sort: "title"}, "[C] Crow [N] Neon [O] Only Human [U] Untitled [W] Why Georgia"},
		{config.Browse{Sort: "year"}, "[2001] Neon Why Georgia [2010] Only Human [Unknown year] Crow Untitled"},
		{config.Browse{Sort: "year", Desc: true}, "[2010] Only Human [2001] Neon Why Georgia [Unknown year] Crow Untitled"},
		// charter tags don't affect grouping
		{config.Browse{Sort: "charter"}, "[gem] Crow Neon [someone] Why Georgia [XEntombmentX] Only Human [Unknown charter] Untitled"},
		{config.Browse{Sort: "length"}, "[Under 2 minutes] Crow [4 to 5 minutes] Neon Why Georgia Only Human [Unknown length] Untitled"},
		{config.Browse{Sort: "difficulty", DiffScale: "fretwork"}, "[Fretwork 1] Why Georgia [Fretwork 2] Neon [Fretwork 4] Only Human [No Guitar rating] Crow Untitled"},
		{config.Browse{Sort: "source"}, "[custom] Neon [rb3] Only Human [Unknown source] Crow Untitled Why Georgia"},
		{config.Browse{Sort: "folder"}, "[in-progress] Crow Only Human Untitled [in-progress/Mayer] Neon Why Georgia"},
	}
	for _, c := range cases {
		if got := browse(songs, x, c.set, ""); got != c.want {
			t.Errorf("%s desc=%v:\n got  %s\n want %s", c.set.Sort, c.set.Desc, got, c.want)
		}
	}
}

func TestFilters(t *testing.T) {
	songs, x := defaultLib(t)
	f := func(fl config.Filter) string {
		fl.Enabled = true
		return browse(songs, x, config.Browse{Sort: "title", Filter: fl}, "")
	}
	if got := f(config.Filter{Instruments: []string{"guitar"}, InstMode: "all"}); got != "[N] Neon [O] Only Human [W] Why Georgia" {
		t.Errorf("guitar: %s", got)
	}
	if got := f(config.Filter{Instruments: []string{"guitar", "bass"}, InstMode: "all"}); got != "[N] Neon" {
		t.Errorf("guitar+bass all: %s", got)
	}
	if got := f(config.Filter{Instruments: []string{"bass", "drums"}, InstMode: "any"}); got != "[C] Crow [N] Neon [U] Untitled" {
		t.Errorf("bass or drums: %s", got)
	}
	if got := f(config.Filter{Instruments: []string{"guitar"}, InstMode: "only"}); got != "[W] Why Georgia" {
		t.Errorf("only guitar: %s", got)
	}
	if got := f(config.Filter{Instruments: []string{"vocals"}, InstMode: "all"}); got != "[N] Neon" {
		t.Errorf("vocals: %s", got)
	}
	if got := f(config.Filter{Fields: []config.FieldFilter{{Field: "title", Text: "GEOR"}}}); got != "[W] Why Georgia" {
		t.Errorf("title contains: %s", got)
	}
	// tags are not searchable text: "color" isn't in any charter name
	if got := f(config.Filter{Fields: []config.FieldFilter{{Field: "charter", Text: "color"}}}); got != "" {
		t.Errorf("charter tags matched: %s", got)
	}
	if got := f(config.Filter{Fields: []config.FieldFilter{{Field: "charter", Text: "gem"}, {Field: "artist", Text: "mayer"}}}); got != "[N] Neon" {
		t.Errorf("two fields: %s", got)
	}
	if got := f(config.Filter{Fields: []config.FieldFilter{{Field: "any", Text: "squares"}}}); got != "[N] Neon [W] Why Georgia" {
		t.Errorf("any field: %s", got)
	}
	if got := f(config.Filter{Fields: []config.FieldFilter{{Field: "folder", Text: "mayer"}}}); got != "[N] Neon [W] Why Georgia" {
		t.Errorf("folder: %s", got)
	}
	// disabled filter does nothing
	if got := browse(songs, x, config.Browse{Sort: "title", Filter: config.Filter{Instruments: []string{"keys"}}}, ""); strings.Count(got, "[") != 5 {
		t.Errorf("disabled filter applied: %s", got)
	}
	// quick search
	if got := browse(songs, x, config.Browse{Sort: "title"}, "tides"); got != "[O] Only Human" {
		t.Errorf("search: %s", got)
	}
}
