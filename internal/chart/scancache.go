package chart

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// scanCacheVersion changes whenever what a scan records from a folder changes
// (the fields below, or how fillMeta, pickChart and the stem rules work), so
// an old file is ignored instead of trusted.
const scanCacheVersion = 1

// ScanCache remembers what the last library scan found, in a gzipped JSON
// file. It does two jobs:
//
//   - Songs returns the previous result at once, so the song list can be
//     filled before the new scan has read a single folder.
//   - ScanLibraryWith uses it to skip reading and parsing a folder's song.ini
//     and chart when neither file has changed since they were recorded.
//
// A song is trusted only while its song.ini and chart file keep the same name,
// size and modification time. The folder listing is always read fresh, so new,
// removed and renamed files are noticed.
//
// A nil *ScanCache is valid and holds nothing.
type ScanCache struct {
	path string

	loadOnce sync.Once
	mu       sync.RWMutex
	recs     map[string]*songRec // by dirKey
	n        int                 // records on disk
	saveMu   sync.Mutex
}

// NewScanCache returns a cache stored at path. The file is read the first time
// it is needed; a missing or unreadable file is an empty cache.
func NewScanCache(path string) *ScanCache {
	return &ScanCache{path: path}
}

// songRec is one song as stored in the cache file.
type songRec struct {
	Dir  string `json:"d"`
	Root string `json:"r"`

	// What the files looked like when the metadata below was read.
	Chart  string `json:"c"`
	CSize  int64  `json:"cs"`
	CMod   int64  `json:"cm"`
	HasIni bool   `json:"hi,omitempty"`
	ISize  int64  `json:"is,omitempty"`
	IMod   int64  `json:"im,omitempty"`

	ParsedIni bool              `json:"pi,omitempty"` // Song.Ini was non-nil
	Ini       map[string]string `json:"ini,omitempty"`

	Name          string `json:"n,omitempty"`
	Artist        string `json:"a,omitempty"`
	Album         string `json:"al,omitempty"`
	Genre         string `json:"g,omitempty"`
	Year          string `json:"y,omitempty"`
	Charter       string `json:"ch,omitempty"`
	Icon          string `json:"ic,omitempty"`
	Playlist      string `json:"pl,omitempty"`
	LoadingPhrase string `json:"lp,omitempty"`
	LengthMs      int64  `json:"len,omitempty"`
	PreviewMs     int64  `json:"pv,omitempty"`
	AlbumTrack    int    `json:"tr,omitempty"`
	Added         int64  `json:"ad,omitempty"` // unix nanoseconds

	// File names inside the folder, used to show the song before the scan
	// has listed it again.
	Stems map[string]string `json:"st,omitempty"`
	Cover string            `json:"cv,omitempty"`
}

// scanStamp is what a scan saw of a folder's song.ini and chart file.
type scanStamp struct {
	ok        bool // sizes and times were read
	chart     string
	chartSize int64
	chartMod  int64
	hasIni    bool
	iniSize   int64
	iniMod    int64
	reused    bool // the metadata came from an unchanged cache record
}

// stampOf reads the size and modification time of the chosen chart and
// song.ini from a directory listing. On Windows the listing already has them.
func stampOf(entries []fs.DirEntry, iniName, chartName string) scanStamp {
	st := scanStamp{chart: chartName, hasIni: iniName != ""}
	gotChart, gotIni := false, !st.hasIni
	for _, e := range entries {
		n := e.Name()
		switch {
		case !gotChart && n == chartName:
			if fi, err := e.Info(); err == nil {
				st.chartSize, st.chartMod, gotChart = fi.Size(), fi.ModTime().UnixNano(), true
			}
		case !gotIni && n == iniName:
			if fi, err := e.Info(); err == nil {
				st.iniSize, st.iniMod, gotIni = fi.Size(), fi.ModTime().UnixNano(), true
			}
		}
	}
	st.ok = gotChart && gotIni
	return st
}

// matches reports whether the files are the ones rec was read from.
func (st scanStamp) matches(r *songRec) bool {
	if !st.ok || r.Chart != st.chart || r.CSize != st.chartSize || r.CMod != st.chartMod || r.HasIni != st.hasIni {
		return false
	}
	return !st.hasIni || (r.ISize == st.iniSize && r.IMod == st.iniMod)
}

// apply copies the recorded metadata onto s.
func (r *songRec) apply(s *Song) {
	if r.ParsedIni {
		vals := r.Ini
		if vals == nil {
			vals = map[string]string{}
		}
		s.Ini = &SongIni{Values: vals}
	}
	s.Name, s.Artist, s.Album, s.Genre, s.Year, s.Charter = r.Name, r.Artist, r.Album, r.Genre, r.Year, r.Charter
	s.Icon, s.Playlist, s.LoadingPhrase = r.Icon, r.Playlist, r.LoadingPhrase
	s.LengthMs, s.PreviewMs, s.AlbumTrack = r.LengthMs, r.PreviewMs, r.AlbumTrack
	if r.Added != 0 {
		s.Added = time.Unix(0, r.Added)
	}
}

// song rebuilds a Song from the record alone, without touching the disk.
func (r *songRec) song() *Song {
	s := &Song{
		Dir:       r.Dir,
		Root:      r.Root,
		ChartPath: filepath.Join(r.Dir, r.Chart),
		Stems:     make(map[string]string, len(r.Stems)),
	}
	for k, name := range r.Stems {
		s.Stems[k] = filepath.Join(r.Dir, name)
	}
	if r.Cover != "" {
		s.CoverArt = filepath.Join(r.Dir, r.Cover)
	}
	r.apply(s)
	s.stamp = scanStamp{
		ok: true, chart: r.Chart, chartSize: r.CSize, chartMod: r.CMod,
		hasIni: r.HasIni, iniSize: r.ISize, iniMod: r.IMod, reused: true,
	}
	return s
}

func recOf(s *Song) songRec {
	r := songRec{
		Dir: s.Dir, Root: s.Root,
		Chart: s.stamp.chart, CSize: s.stamp.chartSize, CMod: s.stamp.chartMod,
		HasIni: s.stamp.hasIni, ISize: s.stamp.iniSize, IMod: s.stamp.iniMod,
		Name: s.Name, Artist: s.Artist, Album: s.Album, Genre: s.Genre, Year: s.Year, Charter: s.Charter,
		Icon: s.Icon, Playlist: s.Playlist, LoadingPhrase: s.LoadingPhrase,
		LengthMs: s.LengthMs, PreviewMs: s.PreviewMs, AlbumTrack: s.AlbumTrack,
	}
	if !s.Added.IsZero() {
		r.Added = s.Added.UnixNano()
	}
	if s.Ini != nil {
		r.ParsedIni, r.Ini = true, s.Ini.Values
	}
	if len(s.Stems) > 0 {
		r.Stems = make(map[string]string, len(s.Stems))
		for k, p := range s.Stems {
			r.Stems[k] = filepath.Base(p)
		}
	}
	if s.CoverArt != "" {
		r.Cover = filepath.Base(s.CoverArt)
	}
	return r
}

func (c *ScanCache) load() {
	c.loadOnce.Do(func() {
		recs := map[string]*songRec{}
		n := 0
		defer func() {
			c.mu.Lock()
			c.recs, c.n = recs, n
			c.mu.Unlock()
		}()
		f, err := os.Open(c.path)
		if err != nil {
			return
		}
		defer f.Close()
		zr, err := gzip.NewReader(bufio.NewReaderSize(f, 1<<20))
		if err != nil {
			return
		}
		defer zr.Close()
		list, err := readRecs(zr)
		if err != nil {
			log.Printf("scan cache: %v (starting over)", err)
			return
		}
		for i := range list {
			recs[dirKey(list[i].Dir)] = &list[i]
		}
		n = len(list)
	})
}

// readRecs decodes {"v":N,"songs":[...]} one song at a time.
func readRecs(r io.Reader) ([]songRec, error) {
	dec := json.NewDecoder(r)
	if _, err := dec.Token(); err != nil { // {
		return nil, err
	}
	var list []songRec
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch keyTok {
		case "v":
			var v int
			if err := dec.Decode(&v); err != nil {
				return nil, err
			}
			if v != scanCacheVersion {
				return nil, nil // another version's file: ignore it quietly
			}
		case "songs":
			if _, err := dec.Token(); err != nil { // [
				return nil, err
			}
			for dec.More() {
				var rec songRec
				if err := dec.Decode(&rec); err != nil {
					return nil, err
				}
				list = append(list, rec)
			}
			if _, err := dec.Token(); err != nil { // ]
				return nil, err
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, err
			}
		}
	}
	return list, nil
}

func (c *ScanCache) lookup(dir string) *songRec {
	if c == nil {
		return nil
	}
	c.load()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.recs[dirKey(dir)]
}

// Songs returns the songs recorded by the last saved scan that sit under one
// of roots, sorted like a scan result, without reading any song folder. They
// may be out of date: a folder could have been removed or edited since.
func (c *ScanCache) Songs(roots []string) []*Song {
	if c == nil {
		return nil
	}
	c.load()
	want := make(map[string]bool, len(roots))
	for _, r := range roots {
		if r != "" {
			want[dirKey(r)] = true
		}
	}
	c.mu.RLock()
	songs := make([]*Song, 0, len(c.recs))
	for _, r := range c.recs {
		if want[dirKey(r.Root)] {
			songs = append(songs, r.song())
		}
	}
	c.mu.RUnlock()
	sortSongs(songs)
	return songs
}

// Len is the number of songs in the cache.
func (c *ScanCache) Len() int {
	if c == nil {
		return 0
	}
	c.load()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.recs)
}

// Save records a scan result, replacing what the cache held. Nothing is
// written when the result is the same as what is already stored.
func (c *ScanCache) Save(songs []*Song) error {
	if c == nil || c.path == "" {
		return nil
	}
	c.load()
	c.saveMu.Lock()
	defer c.saveMu.Unlock()

	c.mu.RLock()
	same := len(songs) == c.n
	c.mu.RUnlock()
	for _, s := range songs {
		if !s.stamp.ok {
			continue // not from a scan; nothing to remember
		}
		if !s.stamp.reused {
			same = false
		}
	}
	list := make([]songRec, 0, len(songs))
	for _, s := range songs {
		if s.stamp.ok {
			list = append(list, recOf(s))
		}
	}
	if len(list) != len(songs) {
		same = false
	}
	if same {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw, _ := gzip.NewWriterLevel(f, gzip.BestSpeed)
	bw := bufio.NewWriterSize(zw, 1<<20)
	werr := func() error {
		if _, err := bw.WriteString(`{"v":1,"songs":[`); err != nil {
			return err
		}
		enc := json.NewEncoder(bw)
		for i := range list {
			if i > 0 {
				if err := bw.WriteByte(','); err != nil {
					return err
				}
			}
			if err := enc.Encode(&list[i]); err != nil {
				return err
			}
		}
		if _, err := bw.WriteString("]}"); err != nil {
			return err
		}
		return bw.Flush()
	}()
	if werr == nil {
		werr = zw.Close()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	if err := os.Rename(tmp, c.path); err != nil {
		os.Remove(tmp)
		return err
	}

	recs := make(map[string]*songRec, len(list))
	for i := range list {
		recs[dirKey(list[i].Dir)] = &list[i]
	}
	c.mu.Lock()
	c.recs, c.n = recs, len(list)
	c.mu.Unlock()
	return nil
}
