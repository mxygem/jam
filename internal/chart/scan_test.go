package chart

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const testChart = "[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = B 120000\n}\n[ExpertSingle]\n{\n  0 = N 0 0\n}\n"

func writeSong(t *testing.T, dir, name, artist string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ini := "[song]\nname = " + name + "\nartist = " + artist + "\n"
	for f, body := range map[string]string{"song.ini": ini, "notes.chart": testChart, "song.ogg": "", "album.png": ""} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// bump makes sure a rewrite gets a different modification time even on
// file systems with coarse timestamps.
func bump(t *testing.T, path string) {
	t.Helper()
	later := time.Now().Add(time.Duration(time.Now().UnixNano()%1000+1) * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func names(songs []*Song) []string {
	var out []string
	for _, s := range songs {
		out = append(out, s.Artist+" - "+s.Name)
	}
	return out
}

func TestScanReusesUnchangedFolders(t *testing.T) {
	root := t.TempDir()
	writeSong(t, filepath.Join(root, "B", "b1"), "Beta", "Band B")
	writeSong(t, filepath.Join(root, "A", "a1"), "Alpha", "Band A")
	writeSong(t, filepath.Join(root, "A", "a2"), "Another", "Band A")
	writeSong(t, filepath.Join(root, ".hidden", "h"), "Hidden", "Nobody")
	if err := os.MkdirAll(filepath.Join(root, "empty", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}

	songs, c1, st := Scan([]string{root}, nil, nil)
	want := []string{"Band A - Alpha", "Band A - Another", "Band B - Beta"}
	if !reflect.DeepEqual(names(songs), want) {
		t.Fatalf("first scan: %v, want %v", names(songs), want)
	}
	if st.Parsed != 3 || st.Reused != 0 || !st.Changed {
		t.Fatalf("first scan stats: %+v", st)
	}

	// Through the saved file, as on the next launch.
	path := filepath.Join(t.TempDir(), "songs.cache")
	if err := c1.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded := LoadScanCache(path)
	if loaded == nil || loaded.Hash != c1.Hash {
		t.Fatal("cache didn't round trip")
	}
	fromCache := loaded.SongsUnder([]string{root})
	for i := range songs {
		a, b := *songs[i], *fromCache[i]
		a.Added, b.Added = a.Added.Round(0), b.Added.Round(0)
		if !a.Added.Equal(b.Added) {
			t.Fatalf("added: %v vs %v", a.Added, b.Added)
		}
		a.Added, b.Added = time.Time{}, time.Time{}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("cached song differs:\n%+v\n%+v", a, b)
		}
	}

	songs2, c2, st := Scan([]string{root}, loaded, nil)
	if st.Reused != 3 || st.Parsed != 0 || st.Changed || c2.Hash != c1.Hash {
		t.Fatalf("rescan of an unchanged library: %+v", st)
	}
	if !reflect.DeepEqual(names(songs2), want) {
		t.Fatalf("rescan: %v", names(songs2))
	}

	// Edit one song.ini in place.
	ini := filepath.Join(root, "A", "a2", "song.ini")
	if err := os.WriteFile(ini, []byte("[song]\nname = Again\nartist = Band A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bump(t, ini)
	songs3, c3, st := Scan([]string{root}, c2, nil)
	if st.Reused != 2 || st.Parsed != 1 || !st.Changed {
		t.Fatalf("after an edit: %+v", st)
	}
	if got := names(songs3)[0]; got != "Band A - Again" {
		t.Fatalf("edited song reads %q", got)
	}

	// Add one, remove one.
	writeSong(t, filepath.Join(root, "C", "c1"), "Gamma", "Band C")
	if err := os.RemoveAll(filepath.Join(root, "B")); err != nil {
		t.Fatal(err)
	}
	songs4, _, st := Scan([]string{root}, c3, nil)
	want = []string{"Band A - Again", "Band A - Alpha", "Band C - Gamma"}
	if !reflect.DeepEqual(names(songs4), want) || st.Parsed != 1 || !st.Changed {
		t.Fatalf("after add/remove: %v %+v", names(songs4), st)
	}
}

func TestScanStemChangeIsNoticed(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	writeSong(t, dir, "Song", "Band")
	_, c, _ := Scan([]string{root}, nil, nil)
	if err := os.WriteFile(filepath.Join(dir, "drums.opus"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	songs, _, st := Scan([]string{root}, c, nil)
	if st.Parsed != 1 || songs[0].Stems[StemDrums] == "" {
		t.Fatalf("new stem not picked up: %+v %v", st, songs[0].Stems)
	}
}

func TestScanOverlappingRoots(t *testing.T) {
	root := t.TempDir()
	writeSong(t, filepath.Join(root, "inner", "x"), "X", "Band")
	inner := filepath.Join(root, "inner")
	songs, _, _ := Scan([]string{root, inner}, nil, nil)
	if len(songs) != 1 || songs[0].Root != root {
		t.Fatalf("overlap: %d songs, root %q", len(songs), songs[0].Root)
	}
	// A cache made with other roots doesn't leak songs into a new list.
	_, c, _ := Scan([]string{inner}, nil, nil)
	if got := c.SongsUnder([]string{root}); len(got) != 0 {
		t.Fatalf("songs from another root: %d", len(got))
	}
	songs, _, st := Scan([]string{root}, c, nil)
	if len(songs) != 1 || songs[0].Root != root || !st.Changed {
		t.Fatalf("root change: %+v", st)
	}
}

func TestLoadSongFolderMatchesScan(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Band - Name only")
	writeSong(t, dir, "", "")
	s, err := LoadSongFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	songs, _, _ := Scan([]string{root}, nil, nil)
	songs[0].Root = ""
	if !reflect.DeepEqual(s, songs[0]) {
		t.Fatalf("differ:\n%+v\n%+v", s, songs[0])
	}
	if s.Artist != "Band" || s.Name != "Name only" || s.ChartMod == 0 || s.ChartSize == 0 {
		t.Fatalf("meta: %+v", s)
	}
}

func TestScanCacheChunksAndCorruption(t *testing.T) {
	c := &ScanCache{Version: ScanCacheVersion, Hash: 42}
	for i := 0; i < 2*scanCacheChunk+7; i++ {
		s := &Song{Dir: filepath.Join("lib", "song", string(rune('a'+i%26))), Name: "n", Stems: map[string]string{"song": "x.ogg"},
			Ini: &SongIni{Values: map[string]string{"name": "n", "delay": "0"}}}
		c.Songs = append(c.Songs, cacheSong(s, uint64(i)))
	}
	path := filepath.Join(t.TempDir(), "songs.cache")
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	got := LoadScanCache(path)
	if got == nil || len(got.Songs) != len(c.Songs) || got.Hash != 42 || got.Songs[len(got.Songs)-1].Print != uint64(len(c.Songs)-1) {
		t.Fatal("chunked cache didn't round trip")
	}
	if s := got.Songs[3].Song(); s.Ini.String("delay") != "0" || s.Stems["song"] != "x.ogg" {
		t.Fatalf("maps: %+v", s)
	}
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if LoadScanCache(path) != nil {
		t.Fatal("truncated cache accepted")
	}
}
