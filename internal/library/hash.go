package library

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mxygem/jam/internal/chart"
)

// FileSum identifies one file by content. Size and Mod (unix nanoseconds)
// say which version of the file was hashed, so it is only read again when
// it changes.
type FileSum struct {
	File   string `json:"file"` // base name, so a stem that changes format is noticed
	Size   int64  `json:"size"`
	Mod    int64  `json:"mod"`
	SHA256 string `json:"sha256"` // hex
}

// hashPath returns the hex SHA-256 of a file's contents.
func hashPath(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.CopyBuffer(h, f, make([]byte, 1<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sumFile hashes a file, reusing prev when the file is unchanged.
func sumFile(path string, prev *FileSum) (*FileSum, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	mod := st.ModTime().UnixNano()
	if prev != nil && prev.SHA256 != "" && prev.File == name && prev.Size == st.Size() && prev.Mod == mod {
		return prev, nil
	}
	sum, err := hashPath(path)
	if err != nil {
		return nil, err
	}
	return &FileSum{File: name, Size: st.Size(), Mod: mod, SHA256: sum}, nil
}

// addSums fills in the content hashes: the chart file, every audio stem, and
// one combined hash for the whole song. prev (may be nil) is the song's
// earlier entry; files that have not changed since are not read again. A file
// that cannot be read is logged and left out, and then the combined hash is
// left empty so the song is never reported as a duplicate on partial data.
func (in *Info) addSums(s *chart.Song, prev *Info) {
	in.ChartSHA256, in.SongSHA256, in.Audio = "", "", nil
	if prev != nil && prev.ChartSHA256 != "" && prev.ChartSize == in.ChartSize && prev.ChartMod == in.ChartMod {
		in.ChartSHA256 = prev.ChartSHA256
	} else if sum, err := hashPath(s.ChartPath); err == nil {
		in.ChartSHA256 = sum
	} else {
		log.Printf("library index: hash %s: %v", s.ChartPath, err)
	}
	complete := in.ChartSHA256 != ""
	if len(s.Stems) > 0 {
		in.Audio = make(map[string]*FileSum, len(s.Stems))
	}
	for name, path := range s.Stems {
		var old *FileSum
		if prev != nil {
			old = prev.Audio[name]
		}
		sum, err := sumFile(path, old)
		if err != nil {
			log.Printf("library index: hash %s: %v", path, err)
			complete = false
			continue
		}
		in.Audio[name] = sum
	}
	if complete {
		in.SongSHA256 = in.songSum()
	}
}

// songSum combines the chart hash and the stem hashes (by stem name) into one
// hash that is equal for two folders exactly when their chart and audio are.
func (in *Info) songSum() string {
	names := make([]string, 0, len(in.Audio))
	for n := range in.Audio {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	fmt.Fprintf(h, "chart %s\n", in.ChartSHA256)
	for _, n := range names {
		fmt.Fprintf(h, "stem %s %s\n", n, in.Audio[n].SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// sumsFresh reports whether the stored hashes still describe the files on
// disk. It only stats files; nothing is read.
func (in *Info) sumsFresh(s *chart.Song) bool {
	if _, err := os.Stat(s.ChartPath); err != nil {
		return true // can't tell; keep what we have
	}
	if in.ChartSHA256 == "" || len(in.Audio) != len(s.Stems) {
		return false
	}
	for name, path := range s.Stems {
		f := in.Audio[name]
		st, err := os.Stat(path)
		if f == nil || err != nil || f.File != filepath.Base(path) || f.Size != st.Size() || f.Mod != st.ModTime().UnixNano() {
			return false
		}
	}
	return true
}

// sumSafe keeps a hashing failure from taking the indexer down.
func sumSafe(s *chart.Song, in, prev *Info) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("library index: hash %s: panic %v", s.Dir, r)
		}
	}()
	in.addSums(s, prev)
}

// DupGroup is a set of song folders that share content.
type DupGroup struct {
	Kind   string   // "song" (chart and audio), "chart" (same chart file) or "audio" (one identical audio file)
	SHA256 string   // the shared hash
	Dirs   []string // folders, sorted
}

// Duplicates groups songs whose content is identical, using the hashes in the
// index (songs not hashed yet are skipped). "song" groups have the same chart
// and the same audio stems. "chart" and "audio" groups cover partial overlap,
// such as the same chart with different audio, or one stem shared between
// folders; a group that is exactly a "song" group is not repeated.
func (x *Index) Duplicates(songs []*chart.Song) []DupGroup {
	songBy := map[string]map[string]bool{}
	chartBy := map[string]map[string]bool{}
	audioBy := map[string]map[string]bool{}
	put := func(m map[string]map[string]bool, sum, dir string) {
		if m[sum] == nil {
			m[sum] = map[string]bool{}
		}
		m[sum][dir] = true
	}
	for _, s := range songs {
		in := x.Get(s)
		if in == nil {
			continue
		}
		if in.SongSHA256 != "" {
			put(songBy, in.SongSHA256, s.Dir)
		}
		if in.ChartSHA256 != "" {
			put(chartBy, in.ChartSHA256, s.Dir)
		}
		for _, f := range in.Audio {
			if f.SHA256 != "" && f.Size > 0 {
				put(audioBy, f.SHA256, s.Dir)
			}
		}
	}
	var out []DupGroup
	whole := map[string]bool{}
	collect := func(kind string, m map[string]map[string]bool) {
		for sum, set := range m {
			if len(set) < 2 {
				continue
			}
			dirs := make([]string, 0, len(set))
			for d := range set {
				dirs = append(dirs, d)
			}
			sort.Strings(dirs)
			id := strings.Join(dirs, "\x00")
			if kind == "song" {
				whole[id] = true
			} else if whole[id] {
				continue
			}
			out = append(out, DupGroup{Kind: kind, SHA256: sum, Dirs: dirs})
		}
	}
	collect("song", songBy)
	collect("chart", chartBy)
	collect("audio", audioBy)
	rank := map[string]int{"song": 0, "chart": 1, "audio": 2}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return rank[out[i].Kind] < rank[out[j].Kind]
		}
		if out[i].Dirs[0] != out[j].Dirs[0] {
			return out[i].Dirs[0] < out[j].Dirs[0]
		}
		return out[i].SHA256 < out[j].SHA256
	})
	return out
}
