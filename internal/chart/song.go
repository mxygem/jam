package chart

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Stem names, following the reserved audio file names used by Clone Hero.
const (
	StemSong    = "song"
	StemGuitar  = "guitar"
	StemBass    = "bass"
	StemRhythm  = "rhythm"
	StemKeys    = "keys"
	StemDrums   = "drums"
	StemDrums1  = "drums_1"
	StemDrums2  = "drums_2"
	StemDrums3  = "drums_3"
	StemDrums4  = "drums_4"
	StemVocals  = "vocals"
	StemVocals1 = "vocals_1"
	StemVocals2 = "vocals_2"
	StemCrowd   = "crowd"
	StemPreview = "preview"
)

var stemNames = map[string]bool{
	StemSong: true, StemGuitar: true, StemBass: true, StemRhythm: true, StemKeys: true,
	StemDrums: true, StemDrums1: true, StemDrums2: true, StemDrums3: true, StemDrums4: true,
	StemVocals: true, StemVocals1: true, StemVocals2: true, StemCrowd: true, StemPreview: true,
}

// AudioExts lists the audio extensions recognised in song folders, in order
// of preference when a stem exists in several formats.
var AudioExts = []string{".ogg", ".opus", ".mp3", ".wav", ".flac", ".m4a", ".aif", ".aiff"}

// Song is a song folder on disk. Chart data is loaded on demand.
type Song struct {
	Dir       string
	ChartPath string
	Ini       *SongIni

	Name, Artist, Album, Genre, Year, Charter string
	Icon                                      string // song.ini icon: the source game or pack
	Playlist                                  string // song.ini playlist / sub_playlist
	LoadingPhrase                             string
	LengthMs                                  int64
	PreviewMs                                 int64
	AlbumTrack                                int
	Root                                      string    // library folder it was found under
	ProjectErr                                error     // set by LoadChart when chart.jam.json couldn't be read
	Added                                     time.Time // when the folder was created or last changed

	Stems    map[string]string // stem name -> file path
	CoverArt string
}

// ID is a stable identifier for the song folder.
func (s *Song) ID() string {
	h := sha1.Sum([]byte(strings.ToLower(filepath.Clean(s.Dir))))
	return hex.EncodeToString(h[:8])
}

// Title returns "Artist - Name".
func (s *Song) Title() string {
	if s.Artist == "" {
		return s.Name
	}
	return s.Artist + " - " + s.Name
}

// LoadChart parses the song's chart file. Vocal parts from a jam project
// file in the same folder (chart.jam.json) replace the chart's own lyrics.
func (s *Song) LoadChart() (*Chart, error) {
	var c *Chart
	var err error
	if strings.EqualFold(filepath.Ext(s.ChartPath), ".mid") {
		c, err = LoadMidiChart(s.ChartPath, s.Ini)
	} else {
		c, err = LoadDotChart(s.ChartPath, s.Ini)
	}
	if err != nil {
		return nil, err
	}
	if perr := applyProjectVocals(c, s.Dir, s.Ini); perr != nil {
		s.ProjectErr = perr
	}
	return c, nil
}

// ChartHash is the SHA-1 of the chart file, used to tie recordings to the
// exact chart they were played against.
func (s *Song) ChartHash() string {
	f, err := os.Open(s.ChartPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ErrNoChart is returned when a folder has no chart file.
var ErrNoChart = errors.New("no chart file in folder")

// LoadSongFolder inspects a song folder.
func LoadSongFolder(dir string) (*Song, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	s := &Song{Dir: dir, Stems: map[string]string{}}
	var charts []string
	stemExt := map[string]int{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		ext := filepath.Ext(lower)
		base := strings.TrimSuffix(lower, ext)
		switch {
		case lower == "song.ini":
			if ini, err := ReadSongIni(filepath.Join(dir, name)); err == nil {
				s.Ini = ini
			}
		case ext == ".chart" || lower == "notes.mid":
			charts = append(charts, name)
		case stemNames[base]:
			if rank := extRank(ext); rank >= 0 {
				if prev, ok := stemExt[base]; !ok || rank < prev {
					stemExt[base] = rank
					s.Stems[base] = filepath.Join(dir, name)
				}
			}
		case base == "album" || base == "cover" || base == "folder":
			if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
				if s.CoverArt == "" || base == "album" {
					s.CoverArt = filepath.Join(dir, name)
				}
			}
		}
	}
	if len(charts) == 0 {
		return nil, ErrNoChart
	}
	s.ChartPath = filepath.Join(dir, pickChart(charts))
	// Numbered stems replace the single stem of the same instrument.
	if _, ok := s.Stems[StemDrums1]; ok {
		delete(s.Stems, StemDrums)
	}
	if _, ok := s.Stems[StemVocals1]; ok {
		delete(s.Stems, StemVocals)
	}
	s.fillMeta()
	return s, nil
}

func extRank(ext string) int {
	for i, e := range AudioExts {
		if e == ext {
			return i
		}
	}
	return -1
}

// pickChart prefers notes.mid, then notes.chart, then [Y]/[F] suffixed charts.
func pickChart(names []string) string {
	score := func(n string) int {
		l := strings.ToLower(n)
		switch {
		case l == "notes.mid":
			return 0
		case l == "notes.chart":
			return 1
		case strings.Contains(l, "[y]") || strings.Contains(l, "[f]") || strings.Contains(l, "(y)") || strings.Contains(l, "(f)"):
			return 2
		case strings.Contains(l, "[n]") || strings.Contains(l, "(n)"):
			return 4
		}
		return 3
	}
	sort.SliceStable(names, func(i, j int) bool { return score(names[i]) < score(names[j]) })
	return names[0]
}

func (s *Song) fillMeta() {
	ini := s.Ini
	s.Name = ini.String("name")
	s.Artist = ini.String("artist")
	s.Album = ini.String("album")
	s.Genre = ini.String("genre")
	s.Year = ini.String("year")
	s.Charter = ini.String("charter")
	if s.Charter == "" {
		s.Charter = ini.String("frets")
	}
	if v, ok := ini.Int("song_length"); ok {
		s.LengthMs = v
	}
	if v, ok := ini.Int("preview_start_time"); ok {
		s.PreviewMs = v
	}
	s.Icon = ini.String("icon")
	s.Playlist = ini.String("playlist")
	if s.Playlist == "" {
		s.Playlist = ini.String("sub_playlist")
	}
	s.LoadingPhrase = ini.String("loading_phrase")
	if v, ok := ini.Int("album_track"); ok {
		s.AlbumTrack = int(v)
	} else if v, ok := ini.Int("track"); ok {
		s.AlbumTrack = int(v)
	}
	if st, err := os.Stat(s.Dir); err == nil {
		s.Added = st.ModTime()
	}
	if s.Name == "" || s.Artist == "" {
		// Fall back to the .chart [Song] section, then the folder name.
		if !strings.EqualFold(filepath.Ext(s.ChartPath), ".mid") {
			if f, err := os.Open(s.ChartPath); err == nil {
				if dc, err := readDotChart(io.LimitReader(f, 64*1024)); err == nil {
					meta := dc.named["Song"]
					if s.Name == "" {
						s.Name = meta["name"]
					}
					if s.Artist == "" {
						s.Artist = meta["artist"]
					}
					if s.Charter == "" {
						s.Charter = meta["charter"]
					}
				}
				f.Close()
			}
		}
	}
	if s.Name == "" {
		base := filepath.Base(s.Dir)
		if a, n, ok := strings.Cut(base, " - "); ok && s.Artist == "" {
			s.Artist = strings.TrimSpace(a)
			base = n
		}
		s.Name = strings.TrimSpace(base)
	}
	if s.Name == "" {
		s.Name = filepath.Base(s.Dir)
	}
}

// ScanLibrary walks the given roots and returns every song folder found,
// sorted by artist then name. Errors in individual folders are skipped.
func ScanLibrary(roots []string) []*Song {
	var songs []*Song
	seen := map[string]bool{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return fs.SkipDir
			}
			key := strings.ToLower(filepath.Clean(path))
			if seen[key] {
				return fs.SkipDir
			}
			s, err := LoadSongFolder(path)
			if err == nil {
				seen[key] = true
				s.Root = root
				songs = append(songs, s)
			}
			return nil
		})
	}
	sort.SliceStable(songs, func(i, j int) bool {
		a, b := strings.ToLower(songs[i].Artist), strings.ToLower(songs[j].Artist)
		if a != b {
			return a < b
		}
		return strings.ToLower(songs[i].Name) < strings.ToLower(songs[j].Name)
	})
	return songs
}
