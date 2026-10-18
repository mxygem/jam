// Package config stores user settings, bindings and recordings under the
// OS config directory (%AppData%\jam on Windows, ~/.config/jam on Linux).
package config

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mxygem/jam/internal/engine"
	"github.com/mxygem/jam/internal/render"
)

// Settings are the user's preferences.
type Settings struct {
	LibraryPaths   []string             `json:"library_paths"`
	AudioOffsetMs  float64              `json:"audio_offset_ms"` // audio output latency
	VideoOffsetMs  float64              `json:"video_offset_ms"` // display latency
	Engine         engine.Settings      `json:"engine"`
	Highway        render.HighwayConfig `json:"highway"`
	Volume         float64              `json:"volume"`
	PreviewVolume  float64              `json:"preview_volume"` // song list previews, relative to Volume; 0 is off
	ErrorSounds    string               `json:"error_sounds"`   // which mistakes make a noise: off, overstrum, extra, all
	ErrorVolume    float64              `json:"error_volume"`   // relative to Volume
	ShowTiming     bool                 `json:"show_timing"`
	MuteOnMiss     bool                 `json:"mute_on_miss"`
	Fullscreen     bool                 `json:"fullscreen"`
	VSync          bool                 `json:"vsync"`
	FFmpegPath     string               `json:"ffmpeg_path,omitempty"`
	LastSong       string               `json:"last_song,omitempty"`
	LastInstrument string               `json:"last_instrument,omitempty"`
	LastDifficulty string               `json:"last_difficulty,omitempty"`
	LeadIn         float64              `json:"lead_in"` // seconds before the first note
	HitFx          string               `json:"hit_fx"`  // hit effects: full, reduced, off
	ShowLyrics     bool                 `json:"show_lyrics"`
	BackingVocals  string               `json:"backing_vocals"`     // harmony/backing parts: below, above, off
	BackingSize    float64              `json:"backing_vocal_size"` // relative to the lead's text
	LyricFx        string               `json:"lyric_fx"`           // charted lyric effects: full, calm, off
	HighwayBounce  float64              `json:"highway_bounce"`     // 0..1: open notes and kicks bounce the highway
	// Background is "" (built in), "random", or an image path relative to
	// the customs folder.
	Background      string  `json:"background"`
	BackgroundDim   float64 `json:"background_dim"`   // 0..1 darkening over the image
	BackgroundMenus bool    `json:"background_menus"` // show the image behind menus too
	Browse          Browse  `json:"browse"`
	Look            Look    `json:"look"`
}

// Look is the chosen theme and note size (see render.ThemeSpec).
type Look struct {
	// Theme is a built-in theme's name, "file:<name>" for a file in the
	// themes folder, or "custom" for Custom. "" is the Jam theme.
	Theme    string            `json:"theme"`
	Custom   *render.ThemeSpec `json:"custom,omitempty"` // the player's own look
	NoteSize float64           `json:"note_size"`        // 1 is normal
}

// Browse is how the song list is sorted, filtered and rated.
type Browse struct {
	Sort      string `json:"sort"`       // see app.sortKeys
	Desc      bool   `json:"desc"`       // reverse order
	DiffScale string `json:"diff_scale"` // charter, fretwork, fwtier, nps
	Filter    Filter `json:"filter"`
}

// Filter narrows the song list. It does nothing unless Enabled.
type Filter struct {
	Enabled     bool          `json:"enabled"`
	Instruments []string      `json:"instruments"` // guitar, bass, rhythm, guitar_coop, keys, drums, vocals
	InstMode    string        `json:"inst_mode"`   // all, any, only
	Fields      []FieldFilter `json:"fields"`
}

// FieldFilter keeps songs whose field contains the text (any case).
type FieldFilter struct {
	Field string `json:"field"` // title, artist, album, charter, genre, year, source, folder, playlist, any
	Text  string `json:"text"`
}

// Active reports whether the filter narrows anything.
func (f Filter) Active() bool {
	if !f.Enabled {
		return false
	}
	if len(f.Instruments) > 0 {
		return true
	}
	for _, ff := range f.Fields {
		if ff.Text != "" {
			return true
		}
	}
	return false
}

// Default returns fresh settings.
func Default() *Settings {
	return &Settings{
		Engine:          engine.DefaultSettings(),
		Highway:         render.DefaultHighway(),
		Volume:          0.9,
		PreviewVolume:   0.8,
		ErrorSounds:     "overstrum",
		ErrorVolume:     0.5,
		ShowTiming:      true,
		MuteOnMiss:      true,
		VSync:           true,
		LeadIn:          2.5,
		HitFx:           "full",
		ShowLyrics:      true,
		BackingVocals:   "below",
		BackingSize:     0.75,
		LyricFx:         "full",
		HighwayBounce:   0.3,
		BackgroundDim:   0.45,
		BackgroundMenus: true,
		Browse:          Browse{Sort: "artist", DiffScale: "charter", Filter: Filter{InstMode: "all"}},
		Look:            Look{Theme: "Jam", NoteSize: 1},
	}
}

// Dir is the app's config directory.
func Dir() string {
	if d := os.Getenv("JAM_HOME"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "jam")
}

// SettingsPath, BindingsPath and RunsDir locate the app's files.
func SettingsPath() string { return filepath.Join(Dir(), "settings.json") }
func BindingsPath() string { return filepath.Join(Dir(), "bindings.json") }
func RunsDir() string      { return filepath.Join(Dir(), "runs") }
func SongsDir() string     { return filepath.Join(Dir(), "songs") }
func ThemesDir() string    { return filepath.Join(Dir(), "themes") }

// Load reads settings, falling back to defaults. On first run the Clone Hero
// song folders are imported.
func Load() (*Settings, bool) {
	s := Default()
	data, err := os.ReadFile(SettingsPath())
	if err != nil {
		s.LibraryPaths = append(DetectCloneHeroPaths(), SongsDir())
		return s, true
	}
	if err := json.Unmarshal(data, s); err != nil {
		s = Default()
		s.LibraryPaths = append(DetectCloneHeroPaths(), SongsDir())
		return s, true
	}
	if s.Highway.NearWidth == 0 {
		s.Highway = render.DefaultHighway()
	}
	if s.Look.NoteSize <= 0 {
		s.Look.NoteSize = 1
	}
	if s.Engine.EarlyWindow == 0 {
		s.Engine = engine.DefaultSettings()
	}
	// Settings files written before the HOPO leniency default moved to
	// 80 ms (Gem's YARG preset) still carry the old default.
	if s.Engine.StrumLenience == 0.06 {
		s.Engine.StrumLenience = engine.DefaultSettings().StrumLenience
	}
	// The ruleset version isn't a preference: new runs always use the
	// current rules (recordings keep the version they were played with).
	s.Engine.Rules = engine.CurrentRules
	return s, false
}

// Save writes settings.
func (s *Settings) Save() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := SettingsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, SettingsPath())
}

// DetectCloneHeroPaths reads song folders from Clone Hero's settings.ini.
func DetectCloneHeroPaths() []string {
	var candidates []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		candidates = append(candidates, filepath.Join(home, "Documents", "Clone Hero", "settings.ini"))
	case "linux":
		candidates = append(candidates, filepath.Join(home, ".clonehero", "settings.ini"), filepath.Join(home, ".config", "unity3d", "srylain Inc_", "Clone Hero", "settings.ini"))
	case "darwin":
		candidates = append(candidates, filepath.Join(home, "Clone Hero", "settings.ini"))
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range candidates {
		f, err := os.Open(c)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(strings.ToLower(k))
			v = strings.TrimSpace(v)
			if strings.HasPrefix(k, "path") && v != "" && !seen[strings.ToLower(v)] {
				if st, err := os.Stat(v); err == nil && st.IsDir() {
					seen[strings.ToLower(v)] = true
					out = append(out, v)
				}
			}
		}
		f.Close()
	}
	return out
}
