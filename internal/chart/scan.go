package chart

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ScanWorkers is how many folders ScanLibrary reads at once. Most of a scan
// is waiting on the disk, so a few readers hide that latency without making a
// spinning drive thrash.
var ScanWorkers = 4

// ScanOptions tunes ScanLibraryWith.
type ScanOptions struct {
	Workers int        // folders read at once; 0 means ScanWorkers
	Cache   *ScanCache // results of an earlier scan to reuse; nil reads everything
}

// ScanLibrary walks the given roots and returns every song folder found,
// sorted by artist then name. Errors in individual folders are skipped.
func ScanLibrary(roots []string) []*Song {
	return ScanLibraryWith(roots, ScanOptions{})
}

// ScanLibraryWith is ScanLibrary with options. Roots are scanned one after
// another, so when roots overlap the earlier root keeps the song. Inside a
// root, folders are read by several workers at once. The result does not
// depend on the number of workers.
func ScanLibraryWith(roots []string, o ScanOptions) []*Song {
	workers := o.Workers
	if workers <= 0 {
		workers = ScanWorkers
	}
	if workers <= 0 {
		workers = 1
	}
	var songs []*Song
	seen := map[string]bool{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		found := scanRoot(root, workers, o.Cache, seen)
		for _, s := range found {
			seen[dirKey(s.Dir)] = true
		}
		songs = append(songs, found...)
	}
	sortSongs(songs)
	return songs
}

// scanRoot finds the songs under one root. seen holds the folders already
// taken by earlier roots and is not written to here.
func scanRoot(root string, workers int, cache *ScanCache, seen map[string]bool) []*Song {
	var mu sync.Mutex
	var found []*Song
	visit := func(dir string) (kids []string) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("library scan: %s: panic %v", dir, r)
				kids = nil
			}
		}()
		if seen[dirKey(dir)] {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		if s := loadSong(dir, root, entries, cache); s != nil {
			mu.Lock()
			found = append(found, s)
			mu.Unlock()
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				kids = append(kids, filepath.Join(dir, e.Name()))
			}
		}
		return kids
	}
	walkParallel(root, workers, visit)
	return found
}

// walkParallel calls visit on root and then on every folder visit returns,
// using up to workers goroutines, and returns when none are left.
func walkParallel(root string, workers int, visit func(dir string) []string) {
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	stack := []string{root}
	active := 0 // folders being visited right now; they may still add more
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				for len(stack) == 0 && active > 0 {
					cond.Wait()
				}
				if len(stack) == 0 {
					mu.Unlock()
					cond.Broadcast()
					return
				}
				dir := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				active++
				mu.Unlock()

				kids := visit(dir)

				mu.Lock()
				stack = append(stack, kids...)
				active--
				mu.Unlock()
				cond.Broadcast()
			}
		}()
	}
	wg.Wait()
}

func dirKey(path string) string { return strings.ToLower(filepath.Clean(path)) }

// sortSongs orders songs by artist, then name, then folder, so the order is
// the same however the scan was split up.
func sortSongs(songs []*Song) {
	type key struct {
		artist, name, dir string
		s                 *Song
	}
	keys := make([]key, len(songs))
	for i, s := range songs {
		keys[i] = key{strings.ToLower(s.Artist), strings.ToLower(s.Name), dirKey(s.Dir), s}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := &keys[i], &keys[j]
		if a.artist != b.artist {
			return a.artist < b.artist
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.dir < b.dir
	})
	for i := range keys {
		songs[i] = keys[i].s
	}
}
