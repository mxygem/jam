// Package replay records every input and judgment of a playthrough and
// analyses recordings afterwards.
package replay

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mxygem/jam/internal/action"
	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/engine"
	"github.com/mxygem/jam/internal/rich"
)

// Version of the recording format.
const Version = 1

// SongRef identifies the chart a run was played against.
type SongRef struct {
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	Charter   string `json:"charter,omitempty"`
	Dir       string `json:"dir"`
	ChartPath string `json:"chart_path"`
	ChartHash string `json:"chart_hash"`
}

// Input is one action event as the engine saw it.
type Input struct {
	T   float64 `json:"t"`             // song time (seconds)
	A   string  `json:"a"`             // action name
	D   bool    `json:"d,omitempty"`   // down
	V   float32 `json:"v,omitempty"`   // analog value / velocity
	Src string  `json:"src,omitempty"` // raw source, e.g. "xinput0:A" or "midi:38"
}

// Summary is the final scoreboard.
type Summary struct {
	Score       int64   `json:"score"`
	NotesTotal  int     `json:"notes_total"`
	NotesHit    int     `json:"notes_hit"`
	NotesMissed int     `json:"notes_missed"`
	Overstrums  int     `json:"overstrums"`
	MaxStreak   int     `json:"max_streak"`
	Accuracy    float64 `json:"accuracy"`
	SPPhrases   int     `json:"sp_phrases"`
	SPHit       int     `json:"sp_phrases_hit"`
	SPActivated int     `json:"sp_activations"`
	Stars       int     `json:"stars"`
}

// Reached is how many notes the run got to (hit or missed).
func (s Summary) Reached() int { return s.NotesHit + s.NotesMissed }

// HitRate is hits over notes reached. Unlike the stored Accuracy of runs
// recorded before that fix, it never counts an unplayed rest of the song.
func (s Summary) HitRate() float64 {
	if s.Reached() == 0 {
		return 0
	}
	return float64(s.NotesHit) / float64(s.Reached())
}

// Recording is a complete playthrough.
type Recording struct {
	Version     int             `json:"version"`
	App         string          `json:"app"`
	Started     time.Time       `json:"started"`
	Song        SongRef         `json:"song"`
	Instrument  string          `json:"instrument"`
	Difficulty  string          `json:"difficulty"`
	Settings    engine.Settings `json:"settings"`
	AudioOffset float64         `json:"audio_offset_ms"`
	VideoOffset float64         `json:"video_offset_ms"`
	Speed       float64         `json:"speed"`
	Device      string          `json:"device"`
	Completed   bool            `json:"completed"`
	EndTime     float64         `json:"end_time"`

	// Who played and with whom. Recordings from before profiles have no
	// profile (they belong to the first one).
	Profile string `json:"profile,omitempty"` // profile id
	Player  string `json:"player,omitempty"`  // profile name at the time
	Mode    string `json:"mode,omitempty"`    // multiplayer: band, h2h, party
	Session string `json:"session,omitempty"` // runs played together share this
	Players int    `json:"players,omitempty"` // multiplayer: how many played
	// FirstNote is the first note of the part the player played: a player
	// who joined a party song partway through starts later. Note indexes
	// in Judgments count from it (see TrackFor).
	FirstNote int `json:"first_note,omitempty"`
	// Revives are the song times a bandmate's star power brought the player
	// back after failing (band mode).
	Revives []float64 `json:"revives,omitempty"`

	Inputs    []Input           `json:"inputs"`
	Judgments []engine.Judgment `json:"judgments"`
	Result    Summary           `json:"result"`
}

// Recorder accumulates a recording during play.
type Recorder struct {
	Rec *Recording
}

// NewRecorder starts a recording.
func NewRecorder(song *chart.Song, inst chart.Instrument, diff chart.Difficulty, set engine.Settings) *Recorder {
	r := &Recording{
		Version:    Version,
		App:        "jam",
		Started:    time.Now(),
		Instrument: inst.String(),
		Difficulty: diff.String(),
		Settings:   set,
		Speed:      1,
	}
	if song != nil {
		r.Song = SongRef{Name: song.Name, Artist: song.Artist, Charter: song.Charter, Dir: song.Dir, ChartPath: song.ChartPath, ChartHash: song.ChartHash()}
	}
	return &Recorder{Rec: r}
}

// Input records an engine event.
func (r *Recorder) Input(ev action.Event, src string) {
	r.Rec.Inputs = append(r.Rec.Inputs, Input{T: ev.Time, A: ev.Action.String(), D: ev.Down, V: ev.Value, Src: src})
}

// Finish copies the engine's judgments and stats into the recording.
func (r *Recorder) Finish(e engine.Engine, endTime float64, completed bool) *Recording {
	rec := r.Rec
	rec.Judgments = append([]engine.Judgment(nil), e.Judgments()...)
	rec.EndTime = endTime
	rec.Completed = completed
	st := e.Stats()
	rec.Result = Summary{
		Score:       int64(st.Score + 0.5),
		NotesTotal:  st.NotesTotal,
		NotesHit:    st.NotesHit,
		NotesMissed: st.NotesMissed,
		Overstrums:  st.Overstrums,
		MaxStreak:   st.MaxStreak,
		SPPhrases:   st.SPPhrases,
		SPHit:       st.SPPhrasesHit,
		SPActivated: st.SPActivations,
	}
	// Accuracy covers the notes the run reached, so ending a run early
	// doesn't count the rest of the song as missed.
	if judged := st.NotesHit + st.NotesMissed; judged > 0 {
		rec.Result.Accuracy = float64(st.NotesHit) / float64(judged)
	}
	rec.Result.Stars = Stars(rec.Result.Accuracy, st.Score, e.Track())
	return rec
}

// Stars gives a 0-5 rating based on score relative to the base score
// (every gem at the streak-limited multiplier, no star power).
func Stars(acc float64, score float64, t *chart.Track) int {
	if t == nil || len(t.Notes) == 0 {
		return 0
	}
	base := 0.0
	streak := 0
	for i := range t.Notes {
		m := 1 + streak/10
		if m > 4 {
			m = 4
		}
		base += float64(50 * t.Notes[i].Count() * m)
		streak++
	}
	ratio := score / base
	switch {
	case acc >= 1:
		return 5
	case ratio >= 1.0:
		return 5
	case ratio >= 0.8:
		return 4
	case ratio >= 0.55:
		return 3
	case ratio >= 0.3:
		return 2
	case ratio > 0:
		return 1
	}
	return 0
}

// Events converts recorded inputs back into engine events.
func (r *Recording) Events() []action.Event {
	out := make([]action.Event, 0, len(r.Inputs))
	for _, in := range r.Inputs {
		a, ok := action.Parse(in.A)
		if !ok {
			continue
		}
		out = append(out, action.Event{Time: in.T, Action: a, Down: in.D, Value: in.V})
	}
	return out
}

// Resimulate replays the recorded inputs through a fresh engine, optionally
// with different settings (e.g. to see what wider hit windows would do).
// revives are the times a failed player was brought back (see Recording).
func Resimulate(c *chart.Chart, t *chart.Track, set engine.Settings, events []action.Event, end float64, revives ...float64) engine.Engine {
	e := engine.New(c, t, set)
	ri := 0
	revive := func(upTo float64) {
		for ri < len(revives) && revives[ri] <= upTo {
			e.Advance(revives[ri])
			e.Revive()
			ri++
		}
	}
	for _, ev := range events {
		revive(ev.Time)
		e.Process(ev)
	}
	revive(end)
	e.Advance(end)
	return e
}

// TrackFor returns the part a recording was played against: t itself, or
// its tail when the player joined partway through.
func TrackFor(r *Recording, t *chart.Track) *chart.Track {
	if r == nil || t == nil || r.FirstNote <= 0 {
		return t
	}
	return t.Tail(r.FirstNote)
}

// Characters Windows doesn't allow in file names (plus control characters).
var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

// Save writes the recording (gzip-compressed JSON) into dir and returns the
// file path.
func (r *Recording) Save(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	title := strings.TrimSpace(rich.Plain(r.Song.Artist) + " - " + rich.Plain(r.Song.Name))
	title = unsafeName.ReplaceAllString(title, "")
	if rs := []rune(title); len(rs) > 60 {
		title = strings.TrimSpace(string(rs[:60]))
	}
	who := ""
	if r.Player != "" && (r.Players > 1 || r.Mode != "") {
		who = " " + unsafeName.ReplaceAllString(r.Player, "")
	}
	name := fmt.Sprintf("%s %s %s %s%s.jamrun.json.gz", r.Started.Format("2006-01-02 150405"), title, r.Instrument, r.Difficulty, who)
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := gzip.NewWriter(f)
	enc := json.NewEncoder(zw)
	enc.SetIndent("", " ")
	if err := enc.Encode(r); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return path, f.Close()
}

// Load reads a recording written by Save (plain JSON also accepted).
func Load(path string) (*Recording, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rd io.Reader = f
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		rd = zr
	}
	var r Recording
	if err := json.NewDecoder(rd).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// List returns recordings in dir, newest first.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), ".jamrun.json") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}
