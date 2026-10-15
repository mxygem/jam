// Package library indexes the song library: what each chart contains
// (parts, difficulties, note counts, length, lyrics) and how hard it is,
// cached on disk so only changed charts are parsed again.
package library

import (
	"compress/gzip"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mxygem/jam/internal/chart"
)

// Part summarizes one instrument of a song.
type Part struct {
	Inst    chart.Instrument   `json:"inst"`
	Diffs   []chart.Difficulty `json:"diffs"`    // hardest first
	Notes   int                `json:"notes"`    // notes on the hardest difficulty
	AvgNPS  float64            `json:"avg_nps"`  // hardest difficulty
	PeakNPS float64            `json:"peak_nps"` // busiest second
	Fw      *Fretwork          `json:"fw,omitempty"`
	Mode    string             `json:"mode,omitempty"` // drums: 4-lane, pro, 5-lane
}

// Info is what the index knows about one song.
type Info struct {
	ChartSize int64  `json:"chart_size"`
	ChartMod  int64  `json:"chart_mod"` // unix nanoseconds
	Parts     []Part `json:"parts"`
	LengthMs  int64  `json:"length_ms"` // last note or sustain end
	Lyrics    bool   `json:"lyrics"`
	Sections  int    `json:"sections"`
	Format    string `json:"format"` // chart or mid
	Err       string `json:"err,omitempty"`
}

// Part returns the part for an instrument, or nil.
func (in *Info) Part(inst chart.Instrument) *Part {
	if in == nil {
		return nil
	}
	for i := range in.Parts {
		if in.Parts[i].Inst == inst {
			return &in.Parts[i]
		}
	}
	return nil
}

// Analyze parses a song's chart and summarizes it.
func Analyze(s *chart.Song) *Info {
	in := &Info{}
	if st, err := os.Stat(s.ChartPath); err == nil {
		in.ChartSize, in.ChartMod = st.Size(), st.ModTime().UnixNano()
	}
	c, err := s.LoadChart()
	if err != nil {
		in.Err = err.Error()
		return in
	}
	in.Lyrics = len(c.Lyrics) > 0
	in.Sections = len(c.Sections)
	in.Format = c.Source
	for _, inst := range chart.Instruments() {
		diffs := c.Available(inst)
		if len(diffs) == 0 {
			continue
		}
		p := Part{Inst: inst, Diffs: diffs}
		t := c.Track(inst, diffs[0])
		p.Notes = len(t.Notes)
		if n := len(t.Notes); n > 0 {
			if e := int64(t.Notes[n-1].SustainEnd() * 1000); e > in.LengthMs {
				in.LengthMs = e
			}
		}
		var fw Fretwork
		if inst.IsDrums() {
			p.Mode = t.DrumMode.String()
			fw, p.AvgNPS, p.PeakNPS = rateDrums(t)
		} else {
			fw, p.AvgNPS, p.PeakNPS = rateFret(t, inst)
		}
		if diffs[0] == chart.Expert { // Fretwork rates Expert only
			p.Fw = &fw
		}
		in.Parts = append(in.Parts, p)
	}
	return in
}

// Index holds Info for every song, keyed by folder.
type Index struct {
	path string

	mu      sync.Mutex
	infos   map[string]*Info
	done    int
	total   int
	running bool
	gen     int // bumps whenever new info arrives (for UI refresh)
}

func key(dir string) string { return strings.ToLower(filepath.Clean(dir)) }

// Open loads the cache file (a missing or broken cache is fine).
func Open(path string) *Index {
	x := &Index{path: path, infos: map[string]*Info{}}
	f, err := os.Open(path)
	if err != nil {
		return x
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return x
	}
	defer zr.Close()
	var m map[string]*Info
	if json.NewDecoder(zr).Decode(&m) == nil && m != nil {
		x.infos = m
	}
	return x
}

// Get returns what is known about a song (nil until it has been indexed).
func (x *Index) Get(s *chart.Song) *Info {
	if x == nil || s == nil {
		return nil
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.infos[key(s.Dir)]
}

// Put records info for a song (e.g. after loading it to play).
func (x *Index) Put(s *chart.Song, in *Info) {
	x.mu.Lock()
	x.infos[key(s.Dir)] = in
	x.gen++
	x.mu.Unlock()
}

// Progress reports indexing progress and a generation counter that changes
// whenever info is added.
func (x *Index) Progress() (done, total int, running bool, gen int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.done, x.total, x.running, x.gen
}

func fresh(in *Info, s *chart.Song) bool {
	if in == nil {
		return false
	}
	st, err := os.Stat(s.ChartPath)
	if err != nil {
		return true // can't tell; keep what we have
	}
	return st.Size() == in.ChartSize && st.ModTime().UnixNano() == in.ChartMod
}

// Update indexes songs whose charts are new or changed, in the background,
// and saves the cache when done. Songs no longer in the library are dropped.
func (x *Index) Update(songs []*chart.Song) {
	x.mu.Lock()
	if x.running {
		x.mu.Unlock()
		return
	}
	var todo []*chart.Song
	keep := map[string]*Info{}
	for _, s := range songs {
		k := key(s.Dir)
		if in := x.infos[k]; fresh(in, s) {
			keep[k] = in
		} else {
			todo = append(todo, s)
		}
	}
	x.infos = keep
	x.done, x.total, x.running = 0, len(todo), len(todo) > 0
	x.gen++
	x.mu.Unlock()
	if len(todo) == 0 {
		return
	}
	go func() {
		start := time.Now()
		jobs := make(chan *chart.Song)
		var wg sync.WaitGroup
		workers := max(1, min(4, runtime.NumCPU()-1))
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for s := range jobs {
					in := analyzeSafe(s)
					x.mu.Lock()
					x.infos[key(s.Dir)] = in
					x.done++
					if x.done%25 == 0 {
						x.gen++
					}
					x.mu.Unlock()
				}
			}()
		}
		for _, s := range todo {
			jobs <- s
		}
		close(jobs)
		wg.Wait()
		x.mu.Lock()
		x.running = false
		x.gen++
		x.mu.Unlock()
		log.Printf("library index: %d charts analyzed in %.1fs", len(todo), time.Since(start).Seconds())
		if err := x.Save(); err != nil {
			log.Printf("library index: save: %v", err)
		}
	}()
}

// analyzeSafe keeps one broken chart from taking the indexer down.
func analyzeSafe(s *chart.Song) (in *Info) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("library index: %s: panic %v", s.ChartPath, r)
			in = &Info{Err: "could not read chart"}
		}
	}()
	return Analyze(s)
}

// Save writes the cache file.
func (x *Index) Save() error {
	x.mu.Lock()
	data, err := json.Marshal(x.infos)
	x.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(x.path), 0o755); err != nil {
		return err
	}
	tmp := x.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, x.path)
}
