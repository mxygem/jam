package chart

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func put(tb testing.TB, path, content string) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		tb.Fatal(err)
	}
}

const chartText = "[Song]\n{\n  Name = \"From Chart\"\n  Artist = \"Chart Artist\"\n  Charter = \"cc\"\n}\n[SyncTrack]\n{\n  0 = TS 4\n  0 = B 120000\n}\n"

func ini(name, artist string, extra ...string) string {
	s := "[song]\nname = " + name + "\nartist = " + artist + "\n"
	for _, e := range extra {
		s += e + "\n"
	}
	return s
}

// smallLib builds a library that exercises the scan rules and returns its root.
func smallLib(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, filepath.Join(root, "A", "Song One", "song.ini"), ini("One", "Zed", "song_length = 61000", "icon = tst", "album_track = 3", "loading_phrase = hi"))
	put(t, filepath.Join(root, "A", "Song One", "notes.chart"), chartText)
	put(t, filepath.Join(root, "A", "Song One", "guitar.ogg"), "x")
	put(t, filepath.Join(root, "A", "Song One", "Album.PNG"), "x")
	put(t, filepath.Join(root, "A", "Song Two", "song.ini"), ini("Two", "Alpha"))
	put(t, filepath.Join(root, "A", "Song Two", "notes.mid"), "x")
	put(t, filepath.Join(root, "A", "Song Two", "song.opus"), "x")
	put(t, filepath.Join(root, "A", "Song Two", "song.ogg"), "x") // preferred over .opus
	put(t, filepath.Join(root, "A", "Song Two", "drums_1.ogg"), "x")
	put(t, filepath.Join(root, "A", "Song Two", "drums.ogg"), "x") // dropped: numbered stems win
	put(t, filepath.Join(root, "B", "pack", "Deep", "Song Three", "notes.chart"), chartText)
	put(t, filepath.Join(root, ".hidden", "Song Four", "notes.chart"), chartText)
	put(t, filepath.Join(root, "B", "NoChart", "song.ini"), ini("Nope", "Nope"))
	put(t, filepath.Join(root, "B", "Song Five", "notes.chart"), chartText)
	put(t, filepath.Join(root, "B", "Song Five", "Bonus", "notes.chart"), chartText) // a song inside a song
	put(t, filepath.Join(root, "B", "Song Five", ".git", "notes.chart"), chartText)  // hidden: skipped
	put(t, filepath.Join(root, "C", "Twin 1", "song.ini"), ini("Same", "Twin"))
	put(t, filepath.Join(root, "C", "Twin 1", "notes.chart"), chartText)
	put(t, filepath.Join(root, "C", "Twin 2", "song.ini"), ini("Same", "Twin"))
	put(t, filepath.Join(root, "C", "Twin 2", "notes.chart"), chartText)
	put(t, filepath.Join(root, "C", "Empty ini", "song.ini"), "")
	put(t, filepath.Join(root, "C", "Empty ini", "notes.chart"), chartText)
	put(t, filepath.Join(root, "C", "Ünïcode ꞉ Dir", "song.ini"), ini("Uni", "Ü"))
	put(t, filepath.Join(root, "C", "Ünïcode ꞉ Dir", "notes.chart"), chartText)
	for i := 0; i < 60; i++ {
		d := filepath.Join(root, "bulk", fmt.Sprintf("g%d", i%7), fmt.Sprintf("s%03d", i))
		put(t, filepath.Join(d, "song.ini"), ini(fmt.Sprintf("Bulk %03d", i), fmt.Sprintf("Band %d", i%5)))
		put(t, filepath.Join(d, "notes.chart"), chartText)
		put(t, filepath.Join(d, "song.ogg"), "x")
	}
	return root
}

// referenceScan is the original sequential scan, kept to check the parallel
// one against.
func referenceScan(roots []string) []*Song {
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
	return songs
}

func dirs(songs []*Song) []string {
	out := make([]string, len(songs))
	for i, s := range songs {
		out[i] = s.Dir
	}
	return out
}

func sameSong(t *testing.T, a, b *Song) {
	t.Helper()
	if !a.Added.Equal(b.Added) {
		t.Errorf("%s: added %v vs %v", a.Dir, a.Added, b.Added)
	}
	x, y := *a, *b
	x.Added, y.Added = time.Time{}, time.Time{}
	x.stamp, y.stamp = scanStamp{}, scanStamp{}
	if !reflect.DeepEqual(x, y) {
		t.Errorf("songs differ:\n%+v\n%+v", x, y)
	}
}

func byDir(songs []*Song) map[string]*Song {
	m := map[string]*Song{}
	for _, s := range songs {
		m[s.Dir] = s
	}
	return m
}

func TestScanMatchesReference(t *testing.T) {
	root := smallLib(t)
	want := referenceScan([]string{root})
	got := ScanLibrary([]string{root})
	if len(want) != len(got) {
		t.Fatalf("found %d songs, reference found %d", len(got), len(want))
	}
	w := byDir(want)
	for _, s := range got {
		r := w[s.Dir]
		if r == nil {
			t.Fatalf("unexpected song %s", s.Dir)
		}
		sameSong(t, r, s)
	}
	for _, skip := range []string{".hidden", "NoChart", ".git"} {
		for _, s := range got {
			if strings.Contains(s.Dir, skip) {
				t.Errorf("%s should not be a song", s.Dir)
			}
		}
	}
	if w[filepath.Join(root, "B", "Song Five", "Bonus")] == nil {
		t.Fatal("a song inside a song folder should be found")
	}
}

func TestScanSongFields(t *testing.T) {
	root := smallLib(t)
	m := byDir(ScanLibrary([]string{root}))
	one := m[filepath.Join(root, "A", "Song One")]
	if one.Name != "One" || one.Artist != "Zed" || one.LengthMs != 61000 || one.Icon != "tst" || one.AlbumTrack != 3 || one.LoadingPhrase != "hi" {
		t.Errorf("song one: %+v", one)
	}
	if one.Root != root || one.ChartPath != filepath.Join(one.Dir, "notes.chart") || one.CoverArt != filepath.Join(one.Dir, "Album.PNG") || one.Stems["guitar"] == "" {
		t.Errorf("song one files: %+v", one)
	}
	two := m[filepath.Join(root, "A", "Song Two")]
	if filepath.Base(two.ChartPath) != "notes.mid" || filepath.Base(two.Stems["song"]) != "song.ogg" || two.Stems["drums"] != "" || two.Stems["drums_1"] == "" {
		t.Errorf("song two: %+v", two)
	}
	three := m[filepath.Join(root, "B", "pack", "Deep", "Song Three")]
	if three.Name != "From Chart" || three.Artist != "Chart Artist" || three.Charter != "cc" || three.Ini != nil {
		t.Errorf("song three (no ini): %+v", three)
	}
}

func TestScanIndependentOfWorkers(t *testing.T) {
	root := smallLib(t)
	base := dirs(ScanLibraryWith([]string{root}, ScanOptions{Workers: 1}))
	if len(base) != 69 {
		t.Fatalf("only %d songs", len(base))
	}
	for _, w := range []int{2, 4, 16} {
		for i := 0; i < 10; i++ {
			got := dirs(ScanLibraryWith([]string{root}, ScanOptions{Workers: w}))
			if !reflect.DeepEqual(base, got) {
				t.Fatalf("workers=%d run %d: order or contents differ", w, i)
			}
		}
	}
	// Equal artist and name are ordered by folder.
	i1 := indexOf(base, filepath.Join(root, "C", "Twin 1"))
	i2 := indexOf(base, filepath.Join(root, "C", "Twin 2"))
	if i1 < 0 || i2 != i1+1 {
		t.Errorf("twins at %d and %d", i1, i2)
	}
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func TestScanOverlappingRoots(t *testing.T) {
	root := smallLib(t)
	inner := filepath.Join(root, "A")
	songs := ScanLibrary([]string{root, inner, filepath.Join(root, "missing"), ""})
	seen := map[string]bool{}
	for _, s := range songs {
		if seen[s.Dir] {
			t.Errorf("%s found twice", s.Dir)
		}
		seen[s.Dir] = true
		if s.Root != root {
			t.Errorf("%s belongs to %s, want the first root", s.Dir, s.Root)
		}
	}
	// And the other way round: the inner root goes first and keeps its songs.
	songs = ScanLibrary([]string{inner, root})
	for _, s := range songs {
		if strings.HasPrefix(s.Dir, inner+string(filepath.Separator)) && s.Root != inner {
			t.Errorf("%s belongs to %s, want %s", s.Dir, s.Root, inner)
		}
	}
}

func TestRootThatIsASong(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "notes.chart"), chartText)
	put(t, filepath.Join(dir, "kid", "notes.chart"), chartText)
	songs := ScanLibrary([]string{dir})
	if len(songs) != 2 {
		t.Fatalf("got %d songs: %v", len(songs), dirs(songs))
	}
}

func TestWalkParallelVisitsEverythingOnce(t *testing.T) {
	// A deep chain, a wide fan-out and a mix, with more workers than work.
	for _, workers := range []int{1, 3, 8, 64} {
		var visits atomic.Int64
		counts := make([]atomic.Int32, 2000)
		walkParallel("0", workers, func(dir string) []string {
			visits.Add(1)
			var n int
			fmt.Sscan(dir, &n)
			counts[n].Add(1)
			var kids []string
			switch {
			case n == 0:
				for i := 1; i <= 500; i++ { // fan-out
					kids = append(kids, fmt.Sprint(i))
				}
				kids = append(kids, "501") // start of a chain
			case n >= 501 && n < 1500:
				kids = append(kids, fmt.Sprint(n+1))
			}
			return kids
		})
		if visits.Load() != 1500+1 {
			t.Fatalf("workers=%d: %d visits", workers, visits.Load())
		}
		for i := 0; i <= 1500; i++ {
			if counts[i].Load() != 1 {
				t.Fatalf("workers=%d: folder %d visited %d times", workers, i, counts[i].Load())
			}
		}
	}
}

func cachePath(t *testing.T) string { return filepath.Join(t.TempDir(), "sub", "songs.json.gz") }

func TestScanCacheRoundTrip(t *testing.T) {
	root := smallLib(t)
	path := cachePath(t)

	c1 := NewScanCache(path)
	first := ScanLibraryWith([]string{root}, ScanOptions{Cache: c1})
	for _, s := range first {
		if s.stamp.reused {
			t.Fatalf("%s reused from an empty cache", s.Dir)
		}
	}
	if err := c1.Save(first); err != nil {
		t.Fatal(err)
	}

	// A new process: the saved list is available without scanning.
	c2 := NewScanCache(path)
	cached := c2.Songs([]string{root})
	if len(cached) != len(first) {
		t.Fatalf("cache has %d songs, scan found %d", len(cached), len(first))
	}
	for i := range first {
		if cached[i].Dir != first[i].Dir {
			t.Fatalf("order differs at %d: %s vs %s", i, cached[i].Dir, first[i].Dir)
		}
		sameSong(t, first[i], cached[i])
	}
	if got := c2.Songs([]string{filepath.Join(root, "elsewhere")}); len(got) != 0 {
		t.Errorf("songs from another root: %d", len(got))
	}

	// Scanning again reuses every record and gives the same songs.
	second := ScanLibraryWith([]string{root}, ScanOptions{Cache: c2})
	if len(second) != len(first) {
		t.Fatalf("rescan found %d, want %d", len(second), len(first))
	}
	for i, s := range second {
		if !s.stamp.reused {
			t.Errorf("%s was read again", s.Dir)
		}
		sameSong(t, first[i], s)
	}
}

func TestScanCacheSaveIsANoOpWhenNothingChanged(t *testing.T) {
	root := smallLib(t)
	path := cachePath(t)
	c := NewScanCache(path)
	if err := c.Save(ScanLibraryWith([]string{root}, ScanOptions{Cache: c})); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	c = NewScanCache(path)
	if err := c.Save(ScanLibraryWith([]string{root}, ScanOptions{Cache: c})); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.ModTime().After(old.Add(time.Hour)) {
		t.Fatal("cache file was rewritten though nothing changed")
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("temp file left behind")
	}
}

func bump(t *testing.T, path string) {
	t.Helper()
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func TestScanCacheNoticesChanges(t *testing.T) {
	root := smallLib(t)
	path := cachePath(t)
	c := NewScanCache(path)
	first := ScanLibraryWith([]string{root}, ScanOptions{Cache: c})
	if err := c.Save(first); err != nil {
		t.Fatal(err)
	}

	one := filepath.Join(root, "A", "Song One")
	two := filepath.Join(root, "A", "Song Two")
	put(t, filepath.Join(one, "song.ini"), ini("One Renamed", "Zed", "song_length = 99000")) // ini edited
	bump(t, filepath.Join(one, "song.ini"))                                                  //   (new mtime, new size)
	put(t, filepath.Join(two, "bass.ogg"), "x")                                              // stem added, ini untouched
	put(t, filepath.Join(root, "A", "Song Six", "notes.chart"), chartText)                   // new folder
	three := filepath.Join(root, "B", "pack", "Deep", "Song Three")                          // chart edited
	put(t, filepath.Join(three, "notes.chart"), strings.Replace(chartText, "From Chart", "Edited", 1))
	bump(t, filepath.Join(three, "notes.chart"))
	if err := os.RemoveAll(filepath.Join(root, "C", "Twin 2")); err != nil { // folder removed
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "C", "Empty ini", "song.ini")); err != nil { // ini removed
		t.Fatal(err)
	}

	c = NewScanCache(path)
	second := ScanLibraryWith([]string{root}, ScanOptions{Cache: c})
	m := byDir(second)
	if _, ok := m[filepath.Join(root, "C", "Twin 2")]; ok {
		t.Error("removed folder still listed")
	}
	if s := m[one]; s == nil || s.stamp.reused || s.Name != "One Renamed" || s.LengthMs != 99000 {
		t.Errorf("edited ini: %+v", s)
	}
	if s := m[two]; s == nil || !s.stamp.reused || s.Stems["bass"] == "" {
		t.Errorf("stem added: reused=%v stems=%v", s != nil && s.stamp.reused, s.Stems)
	}
	if s := m[filepath.Join(root, "A", "Song Six")]; s == nil || s.stamp.reused {
		t.Errorf("new folder: %+v", s)
	}
	if s := m[three]; s == nil || s.stamp.reused || s.Name != "Edited" {
		t.Errorf("edited chart: %+v", s)
	}
	if s := m[filepath.Join(root, "C", "Empty ini")]; s == nil || s.stamp.reused || s.Ini != nil {
		t.Errorf("ini removed: %+v", s)
	}
	reused := 0
	for _, s := range second {
		if s.stamp.reused {
			reused++
		}
	}
	if reused < len(second)-5 {
		t.Errorf("only %d of %d songs reused", reused, len(second))
	}

	// The changes are saved and the next scan reads nothing.
	if err := c.Save(second); err != nil {
		t.Fatal(err)
	}
	c = NewScanCache(path)
	third := ScanLibraryWith([]string{root}, ScanOptions{Cache: c})
	if len(third) != len(second) {
		t.Fatalf("%d vs %d", len(third), len(second))
	}
	for i, s := range third {
		if !s.stamp.reused {
			t.Errorf("%s read again after save", s.Dir)
		}
		sameSong(t, second[i], s)
	}
}

func TestScanCacheIgnoresBadFiles(t *testing.T) {
	root := smallLib(t)
	for name, content := range map[string]string{
		"garbage":       "not gzip at all",
		"empty":         "",
		"other version": "", // filled below
	} {
		path := cachePath(t)
		put(t, path, content)
		if name == "other version" {
			c := NewScanCache(path)
			if err := c.Save(ScanLibraryWith([]string{root}, ScanOptions{})); err != nil {
				t.Fatal(err)
			}
			rewriteVersion(t, path, 99)
		}
		c := NewScanCache(path)
		if got := c.Songs([]string{root}); len(got) != 0 {
			t.Errorf("%s: %d songs from a bad file", name, len(got))
		}
		songs := ScanLibraryWith([]string{root}, ScanOptions{Cache: c})
		if len(songs) < 69 {
			t.Errorf("%s: scan found %d songs", name, len(songs))
		}
		if err := c.Save(songs); err != nil {
			t.Errorf("%s: save: %v", name, err)
		}
		if got := NewScanCache(path).Songs([]string{root}); len(got) != len(songs) {
			t.Errorf("%s: cache not rebuilt (%d songs)", name, len(got))
		}
	}
}

func TestNilScanCache(t *testing.T) {
	var c *ScanCache
	if c.Songs([]string{"x"}) != nil || c.Len() != 0 || c.Save(nil) != nil || c.lookup("x") != nil {
		t.Fatal("nil cache should be empty")
	}
}

func TestSaveSkipsSongsNotFromAScan(t *testing.T) {
	c := NewScanCache(cachePath(t))
	if err := c.Save([]*Song{{Dir: "/x", Root: "/r", ChartPath: "/x/notes.chart"}}); err != nil {
		t.Fatal(err)
	}
	if got := NewScanCache(c.path).Songs([]string{"/r"}); len(got) != 0 {
		t.Fatalf("%d songs", len(got))
	}
}

// BenchmarkScan scans a generated library: JAM_BENCH_SONGS songs (default
// 3000), read by 1 and 4 workers, and again with a warm cache.
func BenchmarkScan(b *testing.B) {
	n := 3000
	if v := os.Getenv("JAM_BENCH_SONGS"); v != "" {
		fmt.Sscan(v, &n)
	}
	root := b.TempDir()
	for i := 0; i < n; i++ {
		d := filepath.Join(root, fmt.Sprintf("pack%03d", i/100), fmt.Sprintf("Band %d - Song %d", i%400, i))
		put(b, filepath.Join(d, "song.ini"), ini(fmt.Sprintf("Song %d", i), fmt.Sprintf("Band %d", i%400),
			"album = Some Album", "genre = Rock", "year = 2010", "charter = someone", "song_length = 200000",
			"diff_guitar = 3", "diff_bass = 2", "diff_drums = 4", "pro_drums = True", "preview_start_time = 30000"))
		put(b, filepath.Join(d, "notes.chart"), chartText)
		put(b, filepath.Join(d, "song.ogg"), "x")
		put(b, filepath.Join(d, "guitar.ogg"), "x")
		put(b, filepath.Join(d, "album.jpg"), "x")
	}
	for _, w := range []int{1, 4} {
		b.Run(fmt.Sprintf("workers=%d", w), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if got := ScanLibraryWith([]string{root}, ScanOptions{Workers: w}); len(got) != n {
					b.Fatalf("found %d of %d", len(got), n)
				}
			}
		})
	}
	path := filepath.Join(b.TempDir(), "songs.json.gz")
	c := NewScanCache(path)
	if err := c.Save(ScanLibraryWith([]string{root}, ScanOptions{Cache: c})); err != nil {
		b.Fatal(err)
	}
	b.Run("warm cache, 4 workers", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			c := NewScanCache(path)
			if got := ScanLibraryWith([]string{root}, ScanOptions{Workers: 4, Cache: c}); len(got) != n {
				b.Fatalf("found %d of %d", len(got), n)
			}
		}
	})
	b.Run("load saved list only", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			c := NewScanCache(path)
			if got := c.Songs([]string{root}); len(got) != n {
				b.Fatalf("loaded %d of %d", len(got), n)
			}
		}
	})
}

// rewriteVersion changes the version number in a saved cache file.
func rewriteVersion(t *testing.T, path string, v int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.Replace(b, []byte(`{"v":1,`), []byte(fmt.Sprintf(`{"v":%d,`, v)), 1)
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(out)
	zw.Write(b)
	zw.Close()
	out.Close()
}

// A file counts as changed when either its size or its modification time
// differs, so copying tools that keep times, and edits that keep size, are
// both noticed.
func TestScanCacheSizeAndTimeEachMatter(t *testing.T) {
	root := smallLib(t)
	path := cachePath(t)
	c := NewScanCache(path)
	if err := c.Save(ScanLibraryWith([]string{root}, ScanOptions{Cache: c})); err != nil {
		t.Fatal(err)
	}
	ini1 := filepath.Join(root, "A", "Song One", "song.ini")
	ini2 := filepath.Join(root, "A", "Song Two", "song.ini")
	st1, _ := os.Stat(ini1)
	st2, _ := os.Stat(ini2)

	// Longer content, modification time put back: only the size differs.
	put(t, ini1, ini("Longer Name Here", "Zed"))
	if err := os.Chtimes(ini1, st1.ModTime(), st1.ModTime()); err != nil {
		t.Fatal(err)
	}
	// Same size, different content, new time: only the time differs.
	body, _ := os.ReadFile(ini2)
	put(t, ini2, strings.Replace(string(body), "Two", "Too", 1))
	bump(t, ini2)
	if st, _ := os.Stat(ini2); st.Size() != st2.Size() {
		t.Fatalf("size changed: %d vs %d", st.Size(), st2.Size())
	}

	m := byDir(ScanLibraryWith([]string{root}, ScanOptions{Cache: NewScanCache(path)}))
	if s := m[filepath.Dir(ini1)]; s.stamp.reused || s.Name != "Longer Name Here" {
		t.Errorf("size-only change missed: reused=%v name=%q", s.stamp.reused, s.Name)
	}
	if s := m[filepath.Dir(ini2)]; s.stamp.reused || s.Name != "Too" {
		t.Errorf("time-only change missed: reused=%v name=%q", s.stamp.reused, s.Name)
	}
}

// Lines far longer than the parser's starting buffer still parse.
func TestParseSongIniLongLines(t *testing.T) {
	long := strings.Repeat("x", 300*1024)
	got, err := ParseSongIni(strings.NewReader("[song]\nname = Start\nloading_phrase = " + long + "\nartist = After\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.String("name") != "Start" || got.String("loading_phrase") != long || got.String("artist") != "After" {
		t.Fatalf("name=%q artist=%q phrase=%d bytes", got.String("name"), got.String("artist"), len(got.String("loading_phrase")))
	}
	if _, err := ParseSongIni(strings.NewReader("name = " + strings.Repeat("y", 5*1024*1024))); err == nil {
		t.Error("a line over the 4 MB cap should still be an error")
	}
}
