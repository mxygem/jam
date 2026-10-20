// Package profile keeps player profiles: a name and color, the player's own
// gameplay settings and calibration, their controller bindings, and their
// scores. Recordings name the profile that played them.
package profile

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mxygem/jam/internal/config"
)

// Palette is the colors new profiles get, in order. They're far apart so
// eight players side by side are easy to tell apart.
var Palette = []string{
	"#FF4D6D", // rose
	"#3DA5FF", // sky
	"#FFC23D", // amber
	"#4DE38A", // mint
	"#B07CFF", // violet
	"#FF8A3D", // orange
	"#3DE3E0", // teal
	"#FF7AD9", // pink
}

// MaxName is the longest a profile name can be.
const MaxName = 20

// Profile is one player.
type Profile struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Color   string    `json:"color"` // #RRGGBB
	Created time.Time `json:"created"`
	// Legacy marks the profile made from the settings that existed before
	// profiles: it keeps using bindings.json and scores.json in the config
	// folder, and owns the recordings made before profiles existed.
	Legacy bool                  `json:"legacy,omitempty"`
	Player config.PlayerSettings `json:"settings"`
	// Devices are the controllers this player last set up or joined with
	// ("xinput:0", "midi:1"...). A controller belongs to one profile at a
	// time; the controller setup uses it to guess whose controls are being
	// set up.
	Devices []string `json:"devices,omitempty"`

	dir string
}

// BindingsPath is where the profile's controller bindings live.
func (p *Profile) BindingsPath() string {
	if p.Legacy {
		return config.BindingsPath()
	}
	return filepath.Join(p.dir, p.ID+".bindings.json")
}

// ScoresPath is where the profile's best scores live.
func (p *Profile) ScoresPath() string {
	if p.Legacy {
		return filepath.Join(config.Dir(), "scores.json")
	}
	return filepath.Join(p.dir, p.ID+".scores.json")
}

// Owns reports whether a recording tagged with profile id belongs to p.
func (p *Profile) Owns(id string) bool {
	return id == p.ID || (id == "" && p.Legacy)
}

func (p *Profile) path() string { return filepath.Join(p.dir, p.ID+".json") }

// Store is the set of profiles on disk.
type Store struct {
	dir  string
	list []*Profile
}

// Load reads every profile in dir.
func Load(dir string) (*Store, []error) {
	s := &Store{dir: dir}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return s, []error{err}
	}
	var errs []error
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".json") || strings.Count(n, ".") != 1 {
			continue // skips <id>.bindings.json, <id>.scores.json and .tmp files
		}
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p := &Profile{}
		if err := json.Unmarshal(data, p); err != nil || p.ID == "" {
			errs = append(errs, fmt.Errorf("profile %s is invalid: %v", n, err))
			continue
		}
		p.dir = dir
		p.Player.Fix()
		if p.Name == "" {
			p.Name = "Player"
		}
		if p.Color == "" {
			p.Color = Palette[0]
		}
		s.list = append(s.list, p)
	}
	s.sort()
	return s, errs
}

func (s *Store) sort() {
	sort.SliceStable(s.list, func(i, j int) bool {
		if s.list[i].Legacy != s.list[j].Legacy {
			return s.list[i].Legacy
		}
		return s.list[i].Created.Before(s.list[j].Created)
	})
}

// All returns the profiles, oldest first.
func (s *Store) All() []*Profile { return s.list }

// Get finds a profile by id.
func (s *Store) Get(id string) *Profile {
	for _, p := range s.list {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Index is a profile's position in All, or -1.
func (s *Store) Index(p *Profile) int {
	for i, q := range s.list {
		if q == p {
			return i
		}
	}
	return -1
}

func newID() string {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("p%x", time.Now().UnixNano())
	}
	return "p" + hex.EncodeToString(b[:])
}

// Create adds a profile with the given settings and saves it. The name is
// made unique and the color is the first one no one has yet.
func (s *Store) Create(name string, player config.PlayerSettings) (*Profile, error) {
	p := &Profile{ID: newID(), Name: s.UniqueName(CleanName(name), nil), Color: s.NextColor(), Created: time.Now(), Player: player, dir: s.dir}
	p.Player.Fix()
	s.list = append(s.list, p)
	s.sort()
	return p, s.Save(p)
}

// Migrate makes the first profile out of the settings from before profiles
// existed. It keeps the old bindings and scores files where they are.
func (s *Store) Migrate(st *config.Settings) (*Profile, error) {
	p := &Profile{ID: newID(), Name: DefaultName(), Color: Palette[0], Created: time.Now(), Legacy: true, Player: st.Player(), dir: s.dir}
	s.list = append(s.list, p)
	s.sort()
	return p, s.Save(p)
}

// DefaultName is the first profile's name: the computer's user name.
func DefaultName() string {
	if u, err := user.Current(); err == nil {
		n := u.Username
		if i := strings.LastIndexAny(n, `\/`); i >= 0 {
			n = n[i+1:]
		}
		if n = CleanName(n); n != "" {
			r := []rune(n)
			r[0] = unicode.ToUpper(r[0])
			return string(r)
		}
	}
	return "Player 1"
}

// CleanName trims a typed name to something that fits on a highway.
func CleanName(n string) string {
	n = strings.Join(strings.Fields(n), " ")
	if r := []rune(n); len(r) > MaxName {
		n = strings.TrimSpace(string(r[:MaxName]))
	}
	return n
}

// UniqueName returns name, or name with a number, so no other profile
// (other than except) has it. An empty name becomes "Player N".
func (s *Store) UniqueName(name string, except *Profile) string {
	taken := func(n string) bool {
		for _, p := range s.list {
			if p != except && strings.EqualFold(p.Name, n) {
				return true
			}
		}
		return false
	}
	if name == "" {
		for i := len(s.list) + 1; ; i++ {
			if n := fmt.Sprintf("Player %d", i); !taken(n) {
				return n
			}
		}
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s %d", name, i); !taken(n) {
			return n
		}
	}
}

// NextColor is the first palette color no profile has.
func (s *Store) NextColor() string {
	for _, c := range Palette {
		used := false
		for _, p := range s.list {
			if strings.EqualFold(p.Color, c) {
				used = true
			}
		}
		if !used {
			return c
		}
	}
	return Palette[len(s.list)%len(Palette)]
}

// Save writes a profile.
func (s *Store) Save(p *Profile) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.path())
}

// Delete removes a profile and its bindings and scores files. Its
// recordings stay in the runs folder. The legacy profile can't be deleted.
func (s *Store) Delete(p *Profile) error {
	if p.Legacy {
		return fmt.Errorf("the first profile can't be deleted")
	}
	for i, q := range s.list {
		if q == p {
			s.list = append(s.list[:i], s.list[i+1:]...)
			break
		}
	}
	_ = os.Remove(p.BindingsPath())
	_ = os.Remove(p.ScoresPath())
	return os.Remove(p.path())
}

// Temp is a profile that's never saved (command line test bands).
func Temp(name, color string, player config.PlayerSettings) *Profile {
	p := &Profile{ID: "temp-" + strings.ToLower(strings.ReplaceAll(name, " ", "-")), Name: name, Color: color, Player: player, dir: os.TempDir()}
	p.Player.Fix()
	return p
}
