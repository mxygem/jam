// Package playlist stores named lists of songs to play through.
package playlist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Entry is one song in a playlist, with the part to play.
type Entry struct {
	Dir        string `json:"dir"`   // song folder
	Title      string `json:"title"` // "Artist - Name" (plain), shown if the folder is gone
	Instrument string `json:"instrument"`
	Difficulty string `json:"difficulty"`
}

// Playlist is a named, ordered list of entries.
type Playlist struct {
	Name    string    `json:"name"`
	Entries []Entry   `json:"entries"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// Move moves entry i by d places (clamped).
func (p *Playlist) Move(i, d int) int {
	j := i + d
	if i < 0 || i >= len(p.Entries) || j < 0 || j >= len(p.Entries) {
		return i
	}
	p.Entries[i], p.Entries[j] = p.Entries[j], p.Entries[i]
	return j
}

// Remove deletes entry i.
func (p *Playlist) Remove(i int) {
	if i >= 0 && i < len(p.Entries) {
		p.Entries = append(p.Entries[:i], p.Entries[i+1:]...)
	}
}

var unsafe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

func fileName(name string) string {
	n := strings.TrimSpace(unsafe.ReplaceAllString(name, "_"))
	n = strings.TrimRight(n, ". ")
	if n == "" {
		n = "playlist"
	}
	return n + ".json"
}

// ErrNoName is returned when saving a playlist without a name.
var ErrNoName = errors.New("playlist needs a name")

// Save writes the playlist into dir (named after the playlist).
func (p *Playlist) Save(dir string) error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrNoName
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	now := time.Now()
	if p.Created.IsZero() {
		p.Created = now
	}
	p.Updated = now
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fileName(p.Name))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads a playlist file.
func Load(path string) (*Playlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Playlist
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		p.Name = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	return &p, nil
}

// Saved lists the playlists in dir, most recently updated first.
func Saved(dir string) []*Playlist {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []*Playlist
	for _, p := range paths {
		if pl, err := Load(p); err == nil {
			out = append(out, pl)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// Delete removes a saved playlist.
func Delete(dir, name string) error {
	return os.Remove(filepath.Join(dir, fileName(name)))
}

// Rename saves the playlist under a new name and removes the old file.
func (p *Playlist) Rename(dir, name string) error {
	old := p.Name
	p.Name = name
	if err := p.Save(dir); err != nil {
		p.Name = old
		return err
	}
	if old != "" && fileName(old) != fileName(name) {
		_ = Delete(dir, old)
	}
	return nil
}
