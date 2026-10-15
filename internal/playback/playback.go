// Package playback loads a song's audio stems and plays them in sync.
package playback

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2/audio"

	"github.com/hajimehoshi/ebiten/v2/audio/mp3"
	"github.com/hajimehoshi/ebiten/v2/audio/vorbis"
	"github.com/hajimehoshi/ebiten/v2/audio/wav"
	"github.com/mxygem/jam/internal/clock"
)

// SampleRate of the audio context.
const SampleRate = 44100

const bytesPerSecond = SampleRate * 4 // 16-bit stereo

// Stem is one playing audio file.
type Stem struct {
	Name   string
	Path   string
	Player *audio.Player
	Length float64 // seconds, 0 if unknown

	closer io.Closer
	vol    float64
	target float64
	gain   float64 // user volume for this stem
}

// Song is a set of stems that start and stop together.
type Song struct {
	Stems []*Stem
	main  *Stem
	ctx   *audio.Context

	// Silent songs have no players; the position follows the wall clock.
	silent    bool
	length    float64
	playing   bool
	playedAt  float64 // clock.Now() when play started
	pausedPos float64
}

// Silent returns a song without audio (for headless runs and tests).
func Silent(length float64) *Song { return &Song{silent: true, length: length} }

// FindFFmpeg locates an ffmpeg binary: the configured path, PATH, then the
// WinGet links folder on Windows.
func FindFFmpeg(configured string) string {
	if configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			p := filepath.Join(la, "Microsoft", "WinGet", "Links", "ffmpeg.exe")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// ErrNeedFFmpeg is returned for formats that need ffmpeg when none is found.
var ErrNeedFFmpeg = errors.New("this song's audio needs ffmpeg (opus/flac); install it, e.g. `winget install ffmpeg`")

// Load opens every stem. ogg/mp3/wav stream natively; anything else (opus,
// flac, m4a, aiff) is decoded up front through ffmpeg.
func Load(ctx *audio.Context, stems map[string]string, ffmpeg string) (*Song, error) {
	names := make([]string, 0, len(stems))
	for n := range stems {
		if n == "preview" || n == "crowd" {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	// "song" first so it becomes the timing reference when present.
	sort.SliceStable(names, func(i, j int) bool { return names[i] == "song" && names[j] != "song" })

	s := &Song{ctx: ctx}
	type result struct {
		stem *Stem
		err  error
	}
	results := make([]result, len(names))
	var wg sync.WaitGroup
	for i, n := range names {
		wg.Add(1)
		go func(i int, name, path string) {
			defer wg.Done()
			st, err := openStem(ctx, name, path, ffmpeg)
			results[i] = result{st, err}
		}(i, n, stems[n])
	}
	wg.Wait()
	var errs []string
	for i, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", filepath.Base(stems[names[i]]), r.err))
			continue
		}
		s.Stems = append(s.Stems, r.stem)
	}
	if len(s.Stems) == 0 {
		if len(errs) == 0 {
			return nil, errors.New("song has no audio files")
		}
		return nil, errors.New(strings.Join(errs, "; "))
	}
	s.main = s.Stems[0]
	return s, nil
}

func openStem(ctx *audio.Context, name, path, ffmpeg string) (*Stem, error) {
	st := &Stem{Name: name, Path: path, vol: 1, target: 1, gain: 1}
	ext := strings.ToLower(filepath.Ext(path))
	var native io.ReadSeeker
	var length int64
	if ext == ".ogg" || ext == ".mp3" || ext == ".wav" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		var derr error
		switch ext {
		case ".ogg":
			var d *vorbis.Stream
			d, derr = vorbis.DecodeWithSampleRate(SampleRate, f)
			if derr == nil {
				native, length = d, d.Length()
			}
		case ".mp3":
			var d *mp3.Stream
			d, derr = mp3.DecodeWithSampleRate(SampleRate, f)
			if derr == nil {
				native, length = d, d.Length()
			}
		case ".wav":
			var d *wav.Stream
			d, derr = wav.DecodeWithSampleRate(SampleRate, f)
			if derr == nil {
				native, length = d, d.Length()
			}
		}
		if derr == nil {
			st.closer = f
		} else {
			f.Close() // e.g. an .ogg that actually holds Opus: fall back to ffmpeg
		}
	}
	if native == nil {
		if ffmpeg == "" {
			return nil, ErrNeedFFmpeg
		}
		pcm, err := decodeFFmpeg(ffmpeg, path)
		if err != nil {
			return nil, err
		}
		native, length = bytes.NewReader(pcm), int64(len(pcm))
	}
	p, err := ctx.NewPlayer(native)
	if err != nil {
		if st.closer != nil {
			st.closer.Close()
		}
		return nil, err
	}
	p.SetBufferSize(40 * time.Millisecond)
	st.Player = p
	if length > 0 {
		st.Length = float64(length) / bytesPerSecond
	}
	return st, nil
}

func decodeFFmpeg(ffmpeg, path string) ([]byte, error) {
	cmd := exec.Command(ffmpeg, "-v", "error", "-nostdin", "-i", path, "-f", "s16le", "-acodec", "pcm_s16le", "-ac", "2", "-ar", fmt.Sprint(SampleRate), "-")
	hideWindow(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg: %s", msg)
	}
	return out.Bytes(), nil
}

// Duration is the longest stem length in seconds.
func (s *Song) Duration() float64 {
	d := s.length
	for _, st := range s.Stems {
		if st.Length > d {
			d = st.Length
		}
	}
	return d
}

// Play starts (or resumes) every stem.
func (s *Song) Play() {
	if s.silent && !s.playing {
		s.playing = true
		s.playedAt = clock.Now()
	}
	for _, st := range s.Stems {
		st.Player.Play()
	}
}

// Pause pauses every stem.
func (s *Song) Pause() {
	if s.silent && s.playing {
		s.pausedPos = s.Position()
		s.playing = false
	}
	for _, st := range s.Stems {
		st.Player.Pause()
	}
}

// Playing reports whether the reference stem is playing.
func (s *Song) Playing() bool {
	if s.silent {
		return s.playing
	}
	return s.main.Player.IsPlaying()
}

// Seek moves every stem to t seconds.
func (s *Song) Seek(t float64) {
	if t < 0 {
		t = 0
	}
	d := time.Duration(t * float64(time.Second))
	if s.silent {
		s.pausedPos = t
		s.playedAt = clock.Now()
	}
	for _, st := range s.Stems {
		_ = st.Player.SetPosition(d)
	}
}

// Position of the reference stem in seconds.
func (s *Song) Position() float64 {
	if s.silent {
		if !s.playing {
			return s.pausedPos
		}
		return s.pausedPos + clock.Now() - s.playedAt
	}
	return s.main.Player.Position().Seconds()
}

// SetTarget fades a stem toward a volume (0..1). Unknown names are ignored.
func (s *Song) SetTarget(name string, v float64) {
	for _, st := range s.Stems {
		if st.Name == name {
			st.target = v
		}
	}
}

// Has reports whether a stem exists.
func (s *Song) Has(name string) bool {
	for _, st := range s.Stems {
		if st.Name == name {
			return true
		}
	}
	return false
}

// SetMaster sets overall volume.
func (s *Song) SetMaster(v float64) {
	for _, st := range s.Stems {
		st.gain = v
		st.Player.SetVolume(st.vol * st.gain)
	}
}

// Update applies fades; dt is seconds since the last call.
func (s *Song) Update(dt float64) {
	const rate = 1 / 0.06 // full fade in 60 ms
	for _, st := range s.Stems {
		if st.vol == st.target {
			continue
		}
		step := rate * dt
		if st.vol < st.target {
			st.vol = min(st.target, st.vol+step)
		} else {
			st.vol = max(st.target, st.vol-step)
		}
		st.Player.SetVolume(st.vol * st.gain)
	}
}

// Close releases players and files.
func (s *Song) Close() {
	for _, st := range s.Stems {
		st.Player.Pause()
		st.Player.Close()
		if st.closer != nil {
			st.closer.Close()
		}
	}
	s.Stems = nil
}

// StemFor returns the stem that should cut out when the player misses on an
// instrument, or "" when the song has no separate stem for it.
func StemFor(s *Song, inst string) []string {
	switch inst {
	case "guitar", "guitar_coop":
		if s.Has("guitar") {
			return []string{"guitar"}
		}
	case "bass":
		if s.Has("bass") {
			return []string{"bass"}
		}
		if s.Has("rhythm") {
			return []string{"rhythm"}
		}
	case "rhythm":
		if s.Has("rhythm") {
			return []string{"rhythm"}
		}
	case "keys":
		if s.Has("keys") {
			return []string{"keys"}
		}
	case "drums":
		var out []string
		for _, n := range []string{"drums", "drums_1", "drums_2", "drums_3", "drums_4"} {
			if s.Has(n) {
				out = append(out, n)
			}
		}
		return out
	}
	return nil
}
