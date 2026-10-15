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
	ShowTiming     bool                 `json:"show_timing"`
	MuteOnMiss     bool                 `json:"mute_on_miss"`
	Fullscreen     bool                 `json:"fullscreen"`
	VSync          bool                 `json:"vsync"`
	FFmpegPath     string               `json:"ffmpeg_path,omitempty"`
	LastSong       string               `json:"last_song,omitempty"`
	LastInstrument string               `json:"last_instrument,omitempty"`
	LastDifficulty string               `json:"last_difficulty,omitempty"`
	LeadIn         float64              `json:"lead_in"` // seconds before the first note
}

// Default returns fresh settings.
func Default() *Settings {
	return &Settings{
		Engine:     engine.DefaultSettings(),
		Highway:    render.DefaultHighway(),
		Volume:     0.9,
		ShowTiming: true,
		MuteOnMiss: true,
		VSync:      true,
		LeadIn:     2.5,
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
	if s.Engine.EarlyWindow == 0 {
		s.Engine = engine.DefaultSettings()
	}
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
