package replay

import (
	"math"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/rich"
)

// Attempt is one play of a song part in a History.
type Attempt struct {
	Path       string    `json:"path"`
	Started    time.Time `json:"started"`
	Score      int64     `json:"score"`
	HitRate    float64   `json:"hit_rate"`
	Reached    int       `json:"reached"`
	Total      int       `json:"total"`
	MaxStreak  int       `json:"max_streak"`
	Overstrums int       `json:"overstrums"`
	Stars      int       `json:"stars"`
	Completed  bool      `json:"completed"`
	FullCombo  bool      `json:"full_combo"`
	MeanMs     float64   `json:"mean_ms"`
	StdDevMs   float64   `json:"spread_ms"`
	Hits       int       `json:"hits"`
	Mode       string    `json:"mode,omitempty"` // multiplayer mode it was played in
	// SectionAcc is hits over notes reached per section, -1 where the run
	// didn't reach any.
	SectionAcc []float64 `json:"section_acc"`
	// Hist counts hit offsets in History.BucketMs buckets from
	// History.HistStartMs.
	Hist []int `json:"hist"`
	// ChartChanged: recorded against an older version of the chart, so its
	// notes aren't counted in the per-note map.
	ChartChanged bool `json:"chart_changed,omitempty"`
	Current      bool `json:"current,omitempty"` // the run the history was opened from
	Best         bool `json:"best,omitempty"`    // highest score
}

// HistGem is one gem of the chart across every attempt.
type HistGem struct {
	Note    int     `json:"n"`
	Rows    []int   `json:"r"`
	Time    float64 `json:"t"`
	Kind    string  `json:"k"`
	Reached int     `json:"reached"`
	Missed  int     `json:"missed"`
	// Streak of attempts (most recent first) that hit it.
	HitStreak int `json:"hit_streak"`
}

// History is one player's attempts at one part of one song, oldest first.
type History struct {
	Song        string        `json:"song"`
	Artist      string        `json:"artist"`
	Instrument  string        `json:"instrument"`
	Difficulty  string        `json:"difficulty"`
	Player      string        `json:"player"`
	Drums       bool          `json:"drums"`
	Length      float64       `json:"length"`
	Rows        []LaneRow     `json:"rows"`
	Sections    []SectionCard `json:"sections"` // names and times only
	Attempts    []Attempt     `json:"attempts"`
	Gems        []HistGem     `json:"gems"`
	HistStartMs float64       `json:"hist_start_ms"`
	BucketMs    float64       `json:"bucket_ms"`
	Skipped     int           `json:"skipped"` // unreadable runs
}

// runHeader is the part of a recording FindRuns matches on.
type runHeader struct {
	mod  time.Time
	size int64
	h    *header
}

var headerCache sync.Map // path -> runHeader

func cachedHeader(path string) (*header, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if v, ok := headerCache.Load(path); ok {
		rh := v.(runHeader)
		if rh.mod.Equal(fi.ModTime()) && rh.size == fi.Size() {
			return rh.h, nil
		}
	}
	h, err := loadHeader(path)
	if err != nil {
		return nil, err
	}
	headerCache.Store(path, runHeader{fi.ModTime(), fi.Size(), h})
	return h, nil
}

// FindRuns lists the recordings in dir of one song part (song folder,
// instrument, difficulty) whose profile passes owns (nil: everyone's),
// oldest first. Recordings that don't reach a single note are left out.
func FindRuns(dir, songDir, instrument, difficulty string, owns func(profile string) bool) []string {
	files, _ := List(dir)
	want := ScoreKey(songDir, instrument, difficulty)
	var out []string
	for _, f := range files {
		h, err := cachedHeader(f)
		if err != nil || ScoreKey(h.Song.Dir, h.Instrument, h.Difficulty) != want {
			continue
		}
		if owns != nil && !owns(h.Profile) {
			continue
		}
		if h.Result.Reached() == 0 {
			continue
		}
		out = append(out, f)
	}
	sort.Strings(out) // file names start with the date
	return out
}

// histRange is the offset range history histograms cover.
const (
	histBucketMs = 5.0
	histMaxMs    = 150.0
)

// BuildHistory gathers the runs in paths (one song part, see FindRuns)
// into a History. full is the whole part (not a late joiner's tail);
// chartHash is the current chart's hash; current is the path of the run
// it was opened from (may be empty). loaded supplies recordings already in
// memory (may be nil).
func BuildHistory(paths []string, c *chart.Chart, full *chart.Track, chartHash, current string, loaded map[string]*Recording) *History {
	l := newRowLayout(full)
	h := &History{
		Instrument: full.Key.Inst.String(), Difficulty: full.Key.Diff.String(),
		Drums: l.drums, Rows: l.rows, BucketMs: histBucketMs,
	}
	// Gems of the whole part, keyed by note (and pad on drums).
	type key struct{ note, row int }
	gemAt := map[key]int{}
	for i := range full.Notes {
		n := &full.Notes[i]
		if e := n.SustainEnd(); e > h.Length {
			h.Length = e
		}
		if !l.drums {
			gemAt[key{i, -1}] = len(h.Gems)
			h.Gems = append(h.Gems, HistGem{Note: i, Rows: l.guitarRows(n), Time: n.Time, Kind: noteKind(n)})
			continue
		}
		for lane := 0; lane < chart.NumLanes; lane++ {
			if n.Lanes&(1<<lane) == 0 {
				continue
			}
			if r := l.drumRow(n, lane); r >= 0 {
				if _, dup := gemAt[key{i, r}]; dup {
					continue
				}
				gemAt[key{i, r}] = len(h.Gems)
				h.Gems = append(h.Gems, HistGem{Note: i, Rows: []int{r}, Time: n.Time, Kind: l.rows[r].Name})
			}
		}
	}
	// Per-gem results, most recent attempt first, for the hit streaks.
	results := make([][]uint8, len(h.Gems))

	maxWin := 0.0
	var views []*RunView
	var firsts []int // each view's first note in the whole part
	for _, p := range paths {
		rec := loaded[p]
		if rec == nil {
			var err error
			if rec, err = Load(p); err != nil {
				h.Skipped++
				continue
			}
		}
		t := TrackFor(rec, full)
		v := BuildRunView(rec, c, t, nil)
		views = append(views, v)
		firsts = append(firsts, rec.FirstNote)
		if h.Song == "" || p == current {
			h.Song, h.Artist = rich.Plain(rec.Song.Name), rich.Plain(rec.Song.Artist)
		}
		if p == current || h.Player == "" {
			h.Player = rec.Player
		}
		if h.Sections == nil {
			for _, s := range v.Sections {
				h.Sections = append(h.Sections, SectionCard{SectionStat: SectionStat{Name: s.Name, Start: s.Start, End: s.End, Total: s.Total}})
			}
		}
		maxWin = math.Max(maxWin, math.Max(v.EarlyMs, v.LateMs))
		res := rec.Result
		a := Attempt{
			Path: p, Started: rec.Started, Score: res.Score, HitRate: res.HitRate(),
			Reached: res.Reached(), Total: res.NotesTotal, MaxStreak: res.MaxStreak,
			Overstrums: res.Overstrums, Stars: res.Stars, Completed: rec.Completed,
			FullCombo: rec.Completed && res.NotesMissed == 0 && res.Overstrums == 0 && res.NotesHit == res.NotesTotal,
			MeanMs:    v.MeanMs, StdDevMs: v.StdDevMs, Mode: rec.Mode, Current: p == current,
			ChartChanged: chartHash != "" && rec.Song.ChartHash != "" && rec.Song.ChartHash != chartHash,
		}
		for _, s := range v.Sections {
			acc := -1.0
			if s.Judged() > 0 {
				acc = s.Accuracy()
			}
			a.SectionAcc = append(a.SectionAcc, acc)
		}
		h.Attempts = append(h.Attempts, a)
	}
	h.HistStartMs = -math.Ceil(math.Max(maxWin, 50)/histBucketMs) * histBucketMs
	if h.HistStartMs < -histMaxMs {
		h.HistStartMs = -histMaxMs
	}
	nb := int(-2*h.HistStartMs/histBucketMs) + 1
	best := -1
	for i, v := range views {
		a := &h.Attempts[i]
		a.Hist = make([]int, nb)
		for _, g := range v.Gems {
			if g.State == GemHit {
				a.Hits++
				b := int(math.Floor((g.OffMs - h.HistStartMs) / histBucketMs))
				a.Hist[min(max(b, 0), nb-1)]++
			}
		}
		if best < 0 || a.Score > h.Attempts[best].Score {
			best = i
		}
	}
	if best >= 0 {
		h.Attempts[best].Best = true
	}
	// Per-gem counts, newest attempt first so hit streaks count back from
	// the latest.
	for i := len(views) - 1; i >= 0; i-- {
		v, a := views[i], h.Attempts[i]
		if a.ChartChanged {
			continue
		}
		// v.Gems index the part the run played (its tail when the player
		// joined late); map back to the whole part by the note offset.
		first := firsts[i]
		for _, g := range v.Gems {
			row := -1
			if l.drums {
				row = g.Rows[0]
			}
			gi, ok := gemAt[key{g.Note + first, row}]
			if !ok {
				continue
			}
			switch g.State {
			case GemHit:
				h.Gems[gi].Reached++
				results[gi] = append(results[gi], GemHit)
			case GemMissed:
				h.Gems[gi].Reached++
				h.Gems[gi].Missed++
				results[gi] = append(results[gi], GemMissed)
			}
		}
	}
	for gi, rs := range results {
		n := 0
		for _, r := range rs {
			if r != GemHit {
				break
			}
			n++
		}
		h.Gems[gi].HitStreak = n
	}
	return h
}
