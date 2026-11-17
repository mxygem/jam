package chart

import (
	"bufio"
	"compress/gzip"
	"encoding/gob"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// A library scan lists every folder under the library roots once. Song
// folders are fingerprinted from that listing alone: a hash of the folder's
// own modification time plus every file's name, size and modification time.
// When a fingerprint matches the saved scan, the song is rebuilt from the
// cache instead of reading its song.ini and chart again. Editing, adding,
// removing or renaming any file in a song folder changes its fingerprint.
//
// The fingerprints of every song (sorted by folder) roll up into one library
// hash. When it matches the saved one, nothing in the library changed and
// the caller can keep the list it already shows.

// ScanCacheVersion changes whenever the cache format or the meaning of a
// fingerprint changes; older caches are ignored.
const ScanCacheVersion = 3

// ScanCache is the saved result of a scan.
type ScanCache struct {
	Version int
	Hash    uint64       // rollup of every song's folder and fingerprint
	Songs   []CachedSong // sorted like the song list

	byKey map[string]*CachedSong
}

// CachedSong is a Song as saved in the scan cache.
type CachedSong struct {
	Print uint64

	Dir, ChartPath, Root, CoverArt                  string
	Ini, Stems                                      []string // key, value, key, value...
	Name, Artist, Album, Genre, Year, Charter, Icon string
	Playlist, LoadingPhrase                         string
	LengthMs, PreviewMs, Added                      int64
	AlbumTrack                                      int
	ChartSize, ChartMod, ProjMod                    int64
}

// ScanStats describes one scan.
type ScanStats struct {
	Dirs    int // folders listed
	Songs   int
	Reused  int // songs taken from the cache unchanged
	Parsed  int // songs read from disk
	Elapsed time.Duration
	Changed bool // the song list differs from the cache's
}

func (st ScanStats) String() string {
	return strconv.Itoa(st.Songs) + " songs (" + strconv.Itoa(st.Reused) + " unchanged, " +
		strconv.Itoa(st.Parsed) + " read) in " + strconv.Itoa(st.Dirs) + " folders, " +
		strconv.FormatFloat(st.Elapsed.Seconds(), 'f', 2, 64) + "s"
}

func dirKey(dir string) string { return strings.ToLower(filepath.Clean(dir)) }

func cacheSong(s *Song, print uint64) CachedSong {
	c := CachedSong{
		Print: print, Dir: s.Dir, ChartPath: s.ChartPath, Root: s.Root, CoverArt: s.CoverArt,
		Stems: pairs(s.Stems), Name: s.Name, Artist: s.Artist, Album: s.Album, Genre: s.Genre,
		Year: s.Year, Charter: s.Charter, Icon: s.Icon, Playlist: s.Playlist,
		LoadingPhrase: s.LoadingPhrase, LengthMs: s.LengthMs, PreviewMs: s.PreviewMs,
		AlbumTrack: s.AlbumTrack, ChartSize: s.ChartSize, ChartMod: s.ChartMod, ProjMod: s.ProjMod,
	}
	if s.Ini != nil {
		c.Ini = pairs(s.Ini.Values)
	}
	if !s.Added.IsZero() {
		c.Added = s.Added.UnixNano()
	}
	return c
}

// Song rebuilds the song.
func (c *CachedSong) Song() *Song {
	s := &Song{
		Dir: c.Dir, ChartPath: c.ChartPath, Root: c.Root, CoverArt: c.CoverArt,
		Stems: unpairs(c.Stems), Name: c.Name, Artist: c.Artist, Album: c.Album, Genre: c.Genre,
		Year: c.Year, Charter: c.Charter, Icon: c.Icon, Playlist: c.Playlist,
		LoadingPhrase: c.LoadingPhrase, LengthMs: c.LengthMs, PreviewMs: c.PreviewMs,
		AlbumTrack: c.AlbumTrack, ChartSize: c.ChartSize, ChartMod: c.ChartMod, ProjMod: c.ProjMod,
	}
	if c.Ini != nil {
		s.Ini = &SongIni{Values: unpairs(c.Ini)}
	}
	if c.Added != 0 {
		s.Added = time.Unix(0, c.Added)
	}
	return s
}

// pairs flattens a map into sorted key, value pairs (gob decodes slices
// much faster than maps).
func pairs(m map[string]string) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, 2*len(m))
	for _, k := range keys {
		out = append(out, k, m[k])
	}
	return out
}

func unpairs(p []string) map[string]string {
	m := make(map[string]string, len(p)/2)
	for i := 0; i+1 < len(p); i += 2 {
		m[p[i]] = p[i+1]
	}
	return m
}

// SongsUnder returns the cached songs found under any of the given roots,
// in list order: what to show while a fresh scan runs.
func (c *ScanCache) SongsUnder(roots []string) []*Song {
	if c == nil {
		return nil
	}
	want := map[string]bool{}
	for _, r := range roots {
		want[dirKey(r)] = true
	}
	out := make([]*Song, 0, len(c.Songs))
	for i := range c.Songs {
		if want[dirKey(c.Songs[i].Root)] {
			out = append(out, c.Songs[i].Song())
		}
	}
	return out
}

func (c *ScanCache) lookup(key string) *CachedSong {
	if c == nil {
		return nil
	}
	if c.byKey == nil {
		c.byKey = make(map[string]*CachedSong, len(c.Songs))
		for i := range c.Songs {
			c.byKey[dirKey(c.Songs[i].Dir)] = &c.Songs[i]
		}
	}
	return c.byKey[key]
}

// folderPrint fingerprints a song folder from its listing (FNV-1a, 64 bit).
func folderPrint(dirMod time.Time, entries []fs.DirEntry, infos []fs.FileInfo) uint64 {
	h := fnv.New64a()
	var b [8]byte
	put := func(v int64) {
		for i := range b {
			b[i] = byte(v >> (8 * i))
		}
		h.Write(b[:])
	}
	put(dirMod.UnixNano())
	for i, e := range entries {
		if e.IsDir() {
			continue
		}
		h.Write([]byte(e.Name()))
		h.Write([]byte{0})
		if fi := infos[i]; fi != nil {
			put(fi.Size())
			put(fi.ModTime().UnixNano())
		} else {
			put(-1)
		}
	}
	return h.Sum64()
}

// ScanLibrary walks the given roots and returns every song folder found,
// sorted by artist then name. Errors in individual folders are skipped.
func ScanLibrary(roots []string) []*Song {
	songs, _, _ := Scan(roots, nil, nil)
	return songs
}

// scanWorkers is how many folders are listed at once. Listing is I/O bound,
// so this is above the CPU count; it stays modest for spinning disks.
var scanWorkers = max(4, min(16, 2*runtime.NumCPU()))

// Scan walks the roots, reusing songs from prev whose folders haven't
// changed. found, if set, counts songs as they are found. It returns the
// sorted song list and the cache to save for next time.
func Scan(roots []string, prev *ScanCache, found *atomic.Int64) ([]*Song, *ScanCache, ScanStats) {
	start := time.Now()
	type hit struct {
		s     *Song
		print uint64
		key   string
	}
	var (
		mu     sync.Mutex
		seen   = map[string]bool{}
		hits   []hit
		dirs   atomic.Int64
		reused atomic.Int64
	)
	if prev != nil && prev.Version != ScanCacheVersion {
		prev = nil
	}
	prev.lookup("") // build the lookup map before the workers share it

	// visit lists one folder, records it if it's a song and returns its
	// subfolders.
	visit := func(t scanTask, out []scanTask) []scanTask {
		key := dirKey(t.dir)
		mu.Lock()
		if seen[key] {
			mu.Unlock()
			return out
		}
		seen[key] = true
		mu.Unlock()

		entries, err := os.ReadDir(t.dir)
		if err != nil {
			return out
		}
		dirs.Add(1)
		if hasChart(entries) {
			infos := fileInfos(entries)
			print := folderPrint(t.mod, entries, infos)
			var s *Song
			if c := prev.lookup(key); c != nil && c.Print == print && c.Root == t.root {
				s = c.Song()
				reused.Add(1)
			} else {
				s, err = songFromEntries(t.dir, t.mod, entries, infos)
			}
			if err == nil {
				s.Root = t.root
				mu.Lock()
				hits = append(hits, hit{s, print, key})
				mu.Unlock()
				if found != nil {
					found.Add(1)
				}
			}
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			var mod time.Time
			if fi, err := e.Info(); err == nil {
				mod = fi.ModTime()
			}
			out = append(out, scanTask{filepath.Join(t.dir, e.Name()), t.root, mod})
		}
		return out
	}

	// Roots are walked one after another so a folder reachable from two
	// roots belongs to the first, as before.
	for _, root := range roots {
		if root == "" {
			continue
		}
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			continue
		}
		walkParallel(scanTask{root, root, st.ModTime()}, visit)
	}

	// Roll the fingerprints up into one library hash (folder order, so the
	// result doesn't depend on which worker finished first).
	sort.Slice(hits, func(i, j int) bool { return hits[i].key < hits[j].key })
	h := fnv.New64a()
	var b [8]byte
	for _, x := range hits {
		h.Write([]byte(x.key))
		h.Write([]byte{0})
		h.Write([]byte(dirKey(x.s.Root)))
		h.Write([]byte{0})
		for i := range b {
			b[i] = byte(x.print >> (8 * i))
		}
		h.Write(b[:])
	}

	sort.SliceStable(hits, func(i, j int) bool { return songLess(hits[i].s, hits[j].s) })
	songs := make([]*Song, len(hits))
	next := &ScanCache{Version: ScanCacheVersion, Hash: h.Sum64(), Songs: make([]CachedSong, len(hits))}
	for i, x := range hits {
		songs[i] = x.s
		next.Songs[i] = cacheSong(x.s, x.print)
	}
	stats := ScanStats{
		Dirs: int(dirs.Load()), Songs: len(songs), Reused: int(reused.Load()),
		Elapsed: time.Since(start), Changed: prev == nil || prev.Hash != next.Hash,
	}
	stats.Parsed = stats.Songs - stats.Reused
	return songs, next, stats
}

type scanTask struct {
	dir, root string
	mod       time.Time // the folder's modification time, from its parent's listing
}

// walkParallel runs visit on start and every folder it returns, with
// scanWorkers folders in flight at once.
func walkParallel(start scanTask, visit func(scanTask, []scanTask) []scanTask) {
	var (
		mu      sync.Mutex
		cond    = sync.NewCond(&mu)
		queue   = []scanTask{start}
		working int
		wg      sync.WaitGroup
	)
	for w := 0; w < scanWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var buf []scanTask
			mu.Lock()
			for {
				for len(queue) == 0 && working > 0 {
					cond.Wait()
				}
				if len(queue) == 0 {
					mu.Unlock()
					cond.Broadcast()
					return
				}
				// Depth first (take from the end) keeps the queue short.
				t := queue[len(queue)-1]
				queue = queue[:len(queue)-1]
				working++
				mu.Unlock()
				buf = visit(t, buf[:0])
				mu.Lock()
				working--
				queue = append(queue, buf...)
				cond.Broadcast()
			}
		}()
	}
	wg.Wait()
}

// songLess orders songs by artist, then name, then folder.
func songLess(a, b *Song) bool {
	aa, ba := strings.ToLower(a.Artist), strings.ToLower(b.Artist)
	if aa != ba {
		return aa < ba
	}
	an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
	if an != bn {
		return an < bn
	}
	return dirKey(a.Dir) < dirKey(b.Dir)
}

// The cache file is gzip'd gob: a header, then the songs in chunks (one
// big message makes gob copy the whole thing as it grows).
type scanCacheHeader struct {
	Version int
	Hash    uint64
	Songs   int
}

const scanCacheChunk = 1000

// LoadScanCache reads a saved scan. A missing, broken or outdated file
// returns nil.
func LoadScanCache(path string) *ScanCache {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	zr, err := gzip.NewReader(bufio.NewReaderSize(f, 256*1024))
	if err != nil {
		return nil
	}
	defer zr.Close()
	dec := gob.NewDecoder(bufio.NewReaderSize(zr, 256*1024))
	var h scanCacheHeader
	if err := dec.Decode(&h); err != nil || h.Version != ScanCacheVersion || h.Songs < 0 {
		return nil
	}
	c := &ScanCache{Version: h.Version, Hash: h.Hash, Songs: make([]CachedSong, 0, h.Songs)}
	for len(c.Songs) < h.Songs {
		var chunk []CachedSong
		if err := dec.Decode(&chunk); err != nil || len(chunk) == 0 {
			return nil
		}
		c.Songs = append(c.Songs, chunk...)
	}
	if len(c.Songs) != h.Songs {
		return nil
	}
	return c
}

// Save writes the cache atomically.
func (c *ScanCache) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	bw := bufio.NewWriterSize(f, 256*1024)
	zw, _ := gzip.NewWriterLevel(bw, gzip.BestSpeed)
	enc := gob.NewEncoder(zw)
	if err := enc.Encode(scanCacheHeader{c.Version, c.Hash, len(c.Songs)}); err != nil {
		return fail(err)
	}
	for i := 0; i < len(c.Songs); i += scanCacheChunk {
		if err := enc.Encode(c.Songs[i:min(i+scanCacheChunk, len(c.Songs))]); err != nil {
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := bw.Flush(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
