package replay

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Best is the best run of one part of one song.
type Best struct {
	Score     int64     `json:"score"`
	HitRate   float64   `json:"hit_rate"` // of the notes reached
	Stars     int       `json:"stars"`
	FullCombo bool      `json:"full_combo"`
	When      time.Time `json:"when"`
	Plays     int       `json:"plays"`
	Completed bool      `json:"completed"`
}

// Scores indexes best runs by song folder, instrument and difficulty.
type Scores struct {
	path string
	mu   sync.Mutex
	best map[string]*Best
}

// ScoreKey identifies one chart part.
func ScoreKey(songDir, instrument, difficulty string) string {
	return strings.ToLower(filepath.Clean(songDir)) + "|" + instrument + "|" + difficulty
}

// LoadScores reads the score index, building it from the run files in
// runsDir the first time.
func LoadScores(path, runsDir string) *Scores {
	s := &Scores{path: path, best: map[string]*Best{}}
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &s.best) == nil {
		return s
	}
	files, _ := List(runsDir)
	for _, f := range files {
		if r, err := loadHeader(f); err == nil {
			s.add(r)
		}
	}
	_ = s.Save()
	return s
}

// header is a recording without its (large) input and judgment arrays.
type header struct {
	Started    time.Time `json:"started"`
	Song       SongRef   `json:"song"`
	Instrument string    `json:"instrument"`
	Difficulty string    `json:"difficulty"`
	Completed  bool      `json:"completed"`
	Result     Summary   `json:"result"`
}

func loadHeader(path string) (*header, error) {
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
	var h header
	return &h, json.NewDecoder(rd).Decode(&h)
}

func (s *Scores) add(h *header) {
	if h.Result.Reached() == 0 {
		return
	}
	k := ScoreKey(h.Song.Dir, h.Instrument, h.Difficulty)
	b := s.best[k]
	if b == nil {
		b = &Best{}
		s.best[k] = b
	}
	b.Plays++
	better := h.Result.Score > b.Score ||
		(h.Result.Score == b.Score && h.Result.HitRate() > b.HitRate)
	if b.Plays == 1 || better {
		plays := b.Plays
		*b = Best{
			Score: h.Result.Score, HitRate: h.Result.HitRate(), Stars: h.Result.Stars,
			FullCombo: h.Completed && h.Result.NotesMissed == 0 && h.Result.Overstrums == 0 && h.Result.NotesHit == h.Result.NotesTotal,
			When:      h.Started, Plays: plays, Completed: h.Completed,
		}
	}
}

// Add records a finished run and saves the index.
func (s *Scores) Add(r *Recording) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.add(&header{Started: r.Started, Song: r.Song, Instrument: r.Instrument, Difficulty: r.Difficulty, Completed: r.Completed, Result: r.Result})
	s.mu.Unlock()
	_ = s.Save()
}

// Best returns the best run of a part, or nil.
func (s *Scores) Best(songDir, instrument, difficulty string) *Best {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.best[ScoreKey(songDir, instrument, difficulty)]
}

// ForSong returns every part of a song that has been played, best first.
func (s *Scores) ForSong(songDir string) map[string]*Best {
	if s == nil {
		return nil
	}
	prefix := strings.ToLower(filepath.Clean(songDir)) + "|"
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]*Best{}
	for k, b := range s.best {
		if strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = b
		}
	}
	return out
}

// Played reports how many times any part of a song was played.
func (s *Scores) Played(songDir string) int {
	n := 0
	for _, b := range s.ForSong(songDir) {
		n += b.Plays
	}
	return n
}

// Save writes the index.
func (s *Scores) Save() error {
	s.mu.Lock()
	data, err := json.MarshalIndent(s.best, "", " ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
