package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/library"
	"github.com/mxygem/jam/internal/rich"
)

// The song list's model: sorting, grouping and filtering, independent of
// drawing so it can be tested on its own.

// sortKey is one way to order the song list.
type sortKey struct {
	id, label string
}

var sortKeys = []sortKey{
	{"artist", "Artist"},
	{"artist_album", "Artist, then album"},
	{"title", "Song title"},
	{"album", "Album"},
	{"charter", "Charter"},
	{"genre", "Genre"},
	{"year", "Year"},
	{"length", "Length"},
	{"difficulty", "Difficulty"},
	{"source", "Source"},
	{"folder", "Folder"},
	{"added", "Recently added"},
	{"played", "Most played"},
}

func sortLabel(id string) string {
	for _, k := range sortKeys {
		if k.id == id {
			return k.label
		}
	}
	return "Artist"
}

// filterFields are the fields a field filter can look at.
var filterFields = []sortKey{
	{"any", "Any field"},
	{"title", "Song title"},
	{"artist", "Artist"},
	{"album", "Album"},
	{"charter", "Charter"},
	{"genre", "Genre"},
	{"year", "Year"},
	{"source", "Source"},
	{"folder", "Folder"},
	{"playlist", "Playlist"},
}

func fieldLabel(id string) string {
	for _, k := range filterFields {
		if k.id == id {
			return k.label
		}
	}
	return id
}

// Filterable parts. Vocals means the chart has lyrics.
var filterParts = []string{"guitar", "bass", "rhythm", "guitar_coop", "keys", "drums", "vocals"}

func partLabel(id string) string {
	if id == "vocals" {
		return "Vocals"
	}
	if i, ok := chart.ParseInstrument(id); ok {
		return i.Title()
	}
	return id
}

// meta is a song's display fields as plain text (tags removed), cached.
type meta struct {
	title, artist, album, charter, genre, year, source, folder, playlist string
	lower                                                                [9]string
}

type browser struct {
	metas map[*chart.Song]*meta
}

func newBrowser() *browser { return &browser{metas: map[*chart.Song]*meta{}} }

func (b *browser) meta(s *chart.Song) *meta {
	if m, ok := b.metas[s]; ok {
		return m
	}
	m := &meta{
		title: rich.Plain(s.Name), artist: rich.Plain(s.Artist), album: rich.Plain(s.Album),
		charter: rich.Plain(s.Charter), genre: rich.Plain(s.Genre), year: rich.Plain(s.Year),
		source: rich.Plain(s.Icon), folder: folderOf(s), playlist: rich.Plain(s.Playlist),
	}
	for i, v := range []string{m.title, m.artist, m.album, m.charter, m.genre, m.year, m.source, m.folder, m.playlist} {
		m.lower[i] = strings.ToLower(v)
	}
	b.metas[s] = m
	return m
}

// field returns the lowercase plain value of a filter field.
func (m *meta) field(id string) string {
	switch id {
	case "title":
		return m.lower[0]
	case "artist":
		return m.lower[1]
	case "album":
		return m.lower[2]
	case "charter":
		return m.lower[3]
	case "genre":
		return m.lower[4]
	case "year":
		return m.lower[5]
	case "source":
		return m.lower[6]
	case "folder":
		return m.lower[7]
	case "playlist":
		return m.lower[8]
	}
	return strings.Join(m.lower[:], "\n")
}

// folderOf is the song's parent folder relative to its library root, e.g.
// "in-progress/Alexisonfire".
func folderOf(s *chart.Song) string {
	if s.Root == "" {
		return filepath.Base(filepath.Dir(s.Dir))
	}
	rel, err := filepath.Rel(s.Root, filepath.Dir(s.Dir))
	base := filepath.Base(s.Root)
	if err != nil || rel == "." {
		return base
	}
	return filepath.ToSlash(filepath.Join(base, rel))
}

// row is one line of the song list: a group header or a song.
type row struct {
	header string // set for group headers
	sub    bool   // second-level header (album under artist)
	song   *chart.Song
}

// browseCtx is what rating and play counts depend on.
type browseCtx struct {
	index  *library.Index
	played func(dir string) int
	inst   chart.Instrument // part used for difficulty sorting
	scale  string
	now    time.Time
	search string // quick search typed on the list
}

// ratingOf returns a song's difficulty on a scale for one part, and whether
// the song has that part (and a rating).
func ratingOf(in *library.Info, s *chart.Song, inst chart.Instrument, scale string) (float64, bool) {
	p := in.Part(inst)
	switch scale {
	case "charter":
		if v, ok := iniTier(s, inst); ok && (p != nil || in == nil) {
			return float64(v), true
		}
		return 0, false
	case "fretwork", "fwtier":
		if p == nil || p.Fw == nil {
			return 0, false
		}
		return p.Fw.D, true
	case "nps":
		if p == nil {
			return 0, false
		}
		return p.AvgNPS, true
	}
	return 0, false
}

// iniTier reads the charter's difficulty tier (diff_guitar etc.) from song.ini.
func iniTier(s *chart.Song, inst chart.Instrument) (int, bool) {
	if s.Ini == nil {
		return 0, false
	}
	keys := map[chart.Instrument][]string{
		chart.Guitar: {"diff_guitar"}, chart.Bass: {"diff_bass"}, chart.Rhythm: {"diff_rhythm"},
		chart.GuitarCoop: {"diff_guitar_coop"}, chart.Keys: {"diff_keys"}, chart.Drums: {"diff_drums", "diff_drums_real"},
	}[inst]
	for _, k := range keys {
		if v, ok := s.Ini.Int(k); ok && v >= 0 {
			return int(v), true
		}
	}
	return 0, false
}

// hasPart reports whether a song has a part ("vocals" = lyrics). known is
// false while the library index hasn't reached the song yet.
func hasPart(in *library.Info, part string) (has, known bool) {
	if in == nil {
		return false, false
	}
	if part == "vocals" {
		return in.Lyrics, true
	}
	inst, ok := chart.ParseInstrument(part)
	if !ok {
		return false, true
	}
	return in.Part(inst) != nil, true
}

func partsOf(in *library.Info) map[string]bool {
	out := map[string]bool{}
	for _, p := range in.Parts {
		out[p.Inst.String()] = true
	}
	if in.Lyrics {
		out["vocals"] = true
	}
	return out
}

// keep applies the quick search and the filter.
func (b *browser) keep(s *chart.Song, f config.Filter, ctx *browseCtx) bool {
	m := b.meta(s)
	if q := strings.ToLower(strings.TrimSpace(ctx.search)); q != "" {
		if !strings.Contains(m.lower[0]+" "+m.lower[1]+" "+m.lower[2]+" "+m.lower[3], q) {
			return false
		}
	}
	if !f.Enabled {
		return true
	}
	for _, ff := range f.Fields {
		t := strings.ToLower(strings.TrimSpace(ff.Text))
		if t != "" && !strings.Contains(m.field(ff.Field), t) {
			return false
		}
	}
	if len(f.Instruments) == 0 {
		return true
	}
	in := ctx.index.Get(s)
	if in == nil {
		return true // not indexed yet: shown until we know
	}
	have := partsOf(in)
	any := false
	for _, p := range f.Instruments {
		if have[p] {
			any = true
		} else if f.InstMode != "any" {
			return false // all / only need every selected part
		}
	}
	if !any {
		return false
	}
	if f.InstMode == "only" {
		sel := map[string]bool{}
		for _, p := range f.Instruments {
			sel[p] = true
		}
		for p := range have {
			if !sel[p] {
				return false
			}
		}
	}
	return true
}

// lengthOf prefers the indexed chart length, then song.ini.
func lengthOf(in *library.Info, s *chart.Song) int64 {
	if in != nil && in.LengthMs > 0 {
		return in.LengthMs
	}
	return s.LengthMs
}

func lengthGroup(ms int64) (string, int) {
	m := ms / 60000
	switch {
	case ms <= 0:
		return "Unknown length", 99
	case m < 2:
		return "Under 2 minutes", 0
	case m < 7:
		return fmt.Sprintf("%d to %d minutes", m, m+1), int(m)
	default:
		return "7 minutes or longer", 7
	}
}

func initial(s string) string {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return strings.ToUpper(string(r))
		}
		if unicode.IsDigit(r) {
			return "#"
		}
	}
	return "#"
}

func addedGroup(t, now time.Time) (string, int64) {
	d := now.Sub(t)
	switch {
	case t.IsZero():
		return "Unknown", -1
	case d < 24*time.Hour:
		return "Today", 1 << 40
	case d < 7*24*time.Hour:
		return "This week", 1 << 39
	case d < 31*24*time.Hour:
		return "This month", 1 << 38
	}
	return t.Format("January 2006"), int64(t.Year()*12 + int(t.Month()))
}

func emptyOr(s, empty string) string {
	if strings.TrimSpace(s) == "" {
		return empty
	}
	return s
}

// buildRows filters, sorts and groups songs.
func (b *browser) buildRows(songs []*chart.Song, set config.Browse, ctx *browseCtx) []row {
	type item struct {
		s      *chart.Song
		m      *meta
		group  string // header text
		gsort  string // group order key
		gnum   float64
		sub    string // second-level header (artist_album)
		num    float64
		hasNum bool
	}
	items := make([]item, 0, len(songs))
	for _, s := range songs {
		if !b.keep(s, set.Filter, ctx) {
			continue
		}
		m := b.meta(s)
		it := item{s: s, m: m}
		in := ctx.index.Get(s)
		switch set.Sort {
		case "artist_album":
			it.group, it.gsort = emptyOr(m.artist, "Unknown artist"), m.lower[1]
			it.sub = emptyOr(m.album, "Unknown album")
		case "title":
			it.group = initial(m.title)
			it.gsort = it.group
		case "album":
			it.group, it.gsort = emptyOr(m.album, "Unknown album"), m.lower[2]
		case "charter":
			it.group, it.gsort = emptyOr(m.charter, "Unknown charter"), m.lower[3]
		case "genre":
			it.group, it.gsort = emptyOr(m.genre, "Unknown genre"), m.lower[4]
		case "year":
			it.group = emptyOr(m.year, "Unknown year")
			if y, err := strconv.Atoi(strings.TrimSpace(m.year)); err == nil {
				it.gnum = float64(y)
			} else {
				it.gnum = -1
			}
		case "length":
			ms := lengthOf(in, s)
			g, n := lengthGroup(ms)
			it.group, it.gnum = g, float64(n)
			it.num, it.hasNum = float64(ms), true
		case "difficulty":
			v, ok := ratingOf(in, s, ctx.inst, ctx.scale)
			if !ok {
				it.group, it.gnum = "No "+ctx.inst.Title()+" rating", -1
			} else {
				it.group, it.gnum = ratingGroup(v, ctx.scale, ctx.inst)
				it.num, it.hasNum = v, true
			}
		case "source":
			it.group, it.gsort = emptyOr(m.source, "Unknown source"), m.lower[6]
		case "folder":
			it.group, it.gsort = m.folder, m.lower[7]
		case "added":
			g, n := addedGroup(s.Added, ctx.now)
			it.group, it.gnum = g, float64(n)
			it.num, it.hasNum = float64(s.Added.Unix()), true
		case "played":
			n := 0
			if ctx.played != nil {
				n = ctx.played(s.Dir)
			}
			it.num, it.hasNum = float64(n), true
			it.gnum = float64(n)
			switch {
			case n == 0:
				it.group = "Never played"
			case n == 1:
				it.group = "Played once"
			case n < 5:
				it.group = "Played 2 to 4 times"
			default:
				it.group, it.gnum = "Played 5 times or more", 5
			}
			if n > 1 && n < 5 {
				it.gnum = 2
			}
		default: // artist
			it.group, it.gsort = emptyOr(m.artist, "Unknown artist"), m.lower[1]
		}
		items = append(items, it)
	}
	numericGroups := map[string]bool{"year": true, "length": true, "difficulty": true, "added": true, "played": true}[set.Sort]
	// Newest/most first reads better for these when not reversed.
	naturalDesc := set.Sort == "added" || set.Sort == "played"
	desc := set.Desc != naturalDesc
	less := func(a, b item) bool {
		if numericGroups {
			if (a.gnum < 0) != (b.gnum < 0) { // unknowns last either way
				return b.gnum < 0
			}
			if a.gnum != b.gnum {
				return (a.gnum < b.gnum) != desc
			}
		} else if a.gsort != b.gsort {
			if a.gsort == "" || b.gsort == "" { // unknowns last either way
				return b.gsort == ""
			}
			return (a.gsort < b.gsort) != desc
		}
		if a.sub != b.sub {
			return (strings.ToLower(a.sub) < strings.ToLower(b.sub)) != desc
		}
		if a.hasNum && a.num != b.num {
			return (a.num < b.num) != desc
		}
		if set.Sort == "artist_album" && a.s.AlbumTrack != b.s.AlbumTrack && a.s.AlbumTrack > 0 && b.s.AlbumTrack > 0 {
			return a.s.AlbumTrack < b.s.AlbumTrack
		}
		if a.m.lower[0] != b.m.lower[0] {
			return a.m.lower[0] < b.m.lower[0]
		}
		return a.m.lower[1] < b.m.lower[1]
	}
	sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j]) })

	rows := make([]row, 0, len(items)+len(items)/4)
	group, sub := "\x00", "\x00"
	for _, it := range items {
		if it.group != group {
			group, sub = it.group, "\x00"
			rows = append(rows, row{header: it.group})
		}
		if set.Sort == "artist_album" && it.sub != sub {
			sub = it.sub
			rows = append(rows, row{header: it.sub, sub: true})
		}
		rows = append(rows, row{song: it.s})
	}
	return rows
}

// ratingGroup names the group a rating falls in.
func ratingGroup(v float64, scale string, inst chart.Instrument) (string, float64) {
	switch scale {
	case "charter":
		return fmt.Sprintf("Tier %d", int(v)), v
	case "fretwork":
		r := library.RemapFor(v, inst)
		return fmt.Sprintf("Fretwork %d", r), float64(r)
	case "fwtier":
		t := library.TierFor(v, inst)
		return fmt.Sprintf("Fretwork tier %d", t), float64(t)
	case "nps":
		lo := int(v/2) * 2
		return fmt.Sprintf("%d to %d notes per second", lo, lo+2), float64(lo)
	}
	return "", 0
}

// diffScales are the difficulty scales the song list can show.
var diffScales = []sortKey{
	{"charter", "Charter tiers (song.ini)"},
	{"fretwork", "Fretwork 0-6"},
	{"fwtier", "Fretwork tier"},
	{"nps", "Notes per second"},
}

func scaleLabel(id string) string {
	for _, k := range diffScales {
		if k.id == id {
			return k.label
		}
	}
	return diffScales[0].label
}
