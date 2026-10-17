package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/library"
	"github.com/mxygem/jam/internal/playback"
	"github.com/mxygem/jam/internal/render"
	"github.com/mxygem/jam/internal/rich"
)

// Song list previews. Once the selection has stayed on a song for a moment,
// a stretch of it is decoded in the background (every stem mixed) and
// looped. Moving on fades it out; the next song fades in when it's ready.
//
// Where a preview starts: a preview file in the song folder plays from its
// start; otherwise song.ini's preview_start_time (and preview_end_time); else
// the first chorus; else a spot 30-55% into the song, the same spot every
// time for a given song, moved back to a section start when one is close.

const (
	previewSettle  = 0.3  // seconds the selection stays put before loading
	previewLen     = 30.0 // seconds previewed
	previewMaxLen  = 60.0 // longest preview song.ini can ask for
	previewPreRoll = 0.4  // start this much before a chorus so its downbeat isn't faded
	previewFadeIn  = 0.25 // player volume ramps (clips have their own loop fades)
	previewFadeOut = 0.35
)

// clipPlayer is an *audio.Player, or a silent stand-in without a sound device.
type clipPlayer interface {
	Play()
	SetVolume(float64)
	Position() time.Duration
	Close() error
}

type previewPlay struct {
	song        *chart.Song
	clip        *playback.Clip
	player      clipPlayer
	vol, target float64
	applied     float64
}

type previewResult struct {
	gen  int
	song *chart.Song
	clip *playback.Clip
	why  string
	took time.Duration
	err  error
}

type previewer struct {
	want    *chart.Song
	wantAt  float64
	started bool // want is loading or playing
	gen     int
	cancel  context.CancelFunc
	results chan previewResult
	cur     *previewPlay
	old     []*previewPlay // fading out
	last    float64
	logged  map[string]bool // songs whose failure has been logged
}

func newPreviewer() *previewer {
	return &previewer{results: make(chan previewResult, 16), logged: map[string]bool{}}
}

// Select is called every frame with the song list's selection (nil for none).
func (p *previewer) Select(song *chart.Song) {
	if p == nil || song == p.want {
		return
	}
	p.want, p.wantAt, p.started = song, clock.Now(), false
	p.abort()
	if p.cur != nil {
		if p.cur.song == song && song != nil {
			p.cur.target = 1 // came straight back: fade it back in
			p.started = true
			return
		}
		p.cur.target = 0
	}
}

// Stop fades out whatever is playing.
func (p *previewer) Stop() { p.Select(nil) }

func (p *previewer) abort() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.gen++
}

// Update loads and fades previews; the app calls it every frame.
func (p *previewer) Update(a *App) {
	if p == nil {
		return
	}
	now := clock.Now()
	dt := max(0, min(0.1, now-p.last))
	p.last = now
	gain := a.Settings.Volume * a.Settings.PreviewVolume
	for done := false; !done; {
		select {
		case r := <-p.results:
			p.arrive(a, r)
		default:
			done = true
		}
	}
	if p.want != nil && !p.started && now-p.wantAt >= previewSettle && gain > 0 && len(p.want.Stems) > 0 {
		p.start(a, p.want)
	}
	if gain <= 0 && p.cur != nil {
		p.cur.target = 0
	}
	step := func(pp *previewPlay) {
		if pp.vol < pp.target {
			pp.vol = min(pp.target, pp.vol+dt/previewFadeIn)
		} else if pp.vol > pp.target {
			pp.vol = max(pp.target, pp.vol-dt/previewFadeOut)
		}
		if v := pp.vol * gain; v != pp.applied {
			pp.player.SetVolume(v)
			pp.applied = v
		}
	}
	kept := p.old[:0]
	for _, pp := range p.old {
		step(pp)
		if pp.vol > 0 {
			kept = append(kept, pp)
		} else {
			_ = pp.player.Close()
		}
	}
	p.old = kept
	if p.cur != nil {
		step(p.cur)
		if p.cur.target == 0 && p.cur.vol == 0 {
			_ = p.cur.player.Close()
			p.cur = nil
		}
	}
}

func (p *previewer) start(a *App, song *chart.Song) {
	p.abort()
	p.started = true
	gen := p.gen
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	info := a.Index.Get(song)
	ffmpeg := a.FFmpeg
	go func() {
		t0 := time.Now()
		r := previewResult{gen: gen, song: song}
		func() {
			defer func() {
				if v := recover(); v != nil {
					r.err = fmt.Errorf("panic: %v", v)
				}
			}()
			r.clip, r.why, r.err = loadPreview(ctx, song, info, ffmpeg)
		}()
		r.took = time.Since(t0)
		select {
		case p.results <- r:
		default: // the app has moved on and isn't reading; drop it
		}
	}()
}

func (p *previewer) arrive(a *App, r previewResult) {
	if r.gen != p.gen {
		return // the selection moved while this was loading
	}
	p.cancel = nil
	if r.err != nil {
		if !errors.Is(r.err, context.Canceled) && !p.logged[r.song.Dir] {
			p.logged[r.song.Dir] = true
			log.Printf("preview %s: %v", r.song.Dir, r.err)
		}
		return
	}
	pl, err := newClipPlayer(a, r.clip)
	if err != nil {
		log.Printf("preview %s: %v", r.song.Dir, err)
		return
	}
	if p.cur != nil {
		p.cur.target = 0
		p.old = append(p.old, p.cur)
	}
	pl.SetVolume(0)
	pl.Play()
	p.cur = &previewPlay{song: r.song, clip: r.clip, player: pl, target: 1}
	log.Printf("preview %q from %s (%s), %d stems, decoded in %d ms",
		plainTitle(r.song), fmtDur(r.clip.Start), r.why, r.clip.Stems, r.took.Milliseconds())
}

func newClipPlayer(a *App, clip *playback.Clip) (clipPlayer, error) {
	if a.Audio == nil {
		return &silentPlayer{}, nil
	}
	loop := audio.NewInfiniteLoop(bytes.NewReader(clip.PCM), int64(len(clip.PCM)))
	return a.Audio.NewPlayer(loop)
}

// silentPlayer stands in for a player when there's no sound device; its
// position follows the clock so the meter still moves.
type silentPlayer struct {
	start   float64
	playing bool
}

func (s *silentPlayer) Play() {
	if !s.playing {
		s.playing, s.start = true, clock.Now()
	}
}
func (s *silentPlayer) SetVolume(float64) {}
func (s *silentPlayer) Close() error      { return nil }
func (s *silentPlayer) Position() time.Duration {
	if !s.playing {
		return 0
	}
	return time.Duration((clock.Now() - s.start) * float64(time.Second))
}

// Playing reports what is previewing and from where (for tests and the log).
func (p *previewer) Playing() (*chart.Song, float64) {
	if p == nil || p.cur == nil || p.cur.target == 0 {
		return nil, 0
	}
	return p.cur.song, p.cur.clip.Start
}

// meter returns n loudness bars (0..1) for song's preview, nil when it isn't
// the one playing.
func (p *previewer) meter(song *chart.Song, n int) []float64 {
	if p == nil || p.cur == nil || p.cur.song != song || p.cur.vol == 0 {
		return nil
	}
	clip := p.cur.clip
	pos := math.Mod(p.cur.player.Position().Seconds(), clip.Seconds())
	const window = 0.03
	out := make([]float64, n)
	for i := range out {
		t := pos - float64(n-1-i)*window
		if t < 0 {
			t += clip.Seconds()
		}
		lv := playback.Level(clip.PCM, int64(t*playback.SampleRate)*4, int64(window*playback.SampleRate)*4)
		out[i] = math.Min(1, math.Sqrt(lv*3)) * p.cur.vol
	}
	return out
}

// drawMeter draws the little equalizer that shows a preview is playing,
// bars standing on y+h.
func drawMeter(dst *ebiten.Image, x, y, h float64, levels []float64) {
	const w, gap = 5.0, 3.0
	for i, lv := range levels {
		bh := max(3, h*lv)
		render.RoundRect(dst, x+float64(i)*(w+gap), y+h-bh, w, bh, 2, render.WithAlpha(th.Accent, 0.9))
	}
}

// loadPreview works out where the song's preview starts and decodes it.
func loadPreview(ctx context.Context, song *chart.Song, info *library.Info, ffmpeg string) (*playback.Clip, string, error) {
	paths, dedicated := playback.PreviewStems(song.Stems)
	if dedicated {
		clip, err := playback.DecodeClip(ctx, paths, 0, previewMaxLen, ffmpeg)
		return clip, "preview file", err
	}
	length := float64(song.LengthMs) / 1000
	if length <= 0 && info != nil {
		length = float64(info.LengthMs) / 1000
	}
	start, dur, ok := iniPreview(song, length)
	why := "song.ini"
	if !ok {
		var sections []chart.Section
		if c, err := song.LoadChart(); err == nil {
			sections = c.Sections
			if length <= 0 {
				length = c.LastTime()
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		start, why = fallbackSpot(song, sections, length)
		dur = previewLen
	}
	clip, err := playback.DecodeClip(ctx, paths, start, dur, ffmpeg)
	if err == nil && start > 0 && clip.Seconds() < 3 {
		// the start was past the end of the audio
		clip, err = playback.DecodeClip(ctx, paths, 0, dur, ffmpeg)
		why += ", past the end of the audio so from the start"
	}
	return clip, why, err
}

// iniPreview reads song.ini's preview_start_time and preview_end_time (ms).
// Zero or negative means unset; a start past the end of the song is ignored.
func iniPreview(song *chart.Song, length float64) (start, dur float64, ok bool) {
	if song.PreviewMs <= 0 {
		return 0, 0, false
	}
	start = float64(song.PreviewMs) / 1000
	if length > 0 && start > length-3 {
		return 0, 0, false
	}
	dur = previewLen
	if end, ok := song.Ini.Int("preview_end_time"); ok && end > song.PreviewMs+3000 {
		dur = min(previewMaxLen, float64(end-song.PreviewMs)/1000)
	}
	return start, dur, true
}

// fallbackSpot picks a start when song.ini doesn't give one: the first
// chorus, else a spot 30-55% in (the same for a song every time), moved back
// to a section start within 12 s.
func fallbackSpot(song *chart.Song, sections []chart.Section, length float64) (float64, string) {
	for _, s := range sections {
		if isChorus(s.Name) && s.Time > 0 && (length <= 0 || s.Time < length-10) {
			return max(0, s.Time-previewPreRoll), "first chorus"
		}
	}
	if length <= 0 {
		length = 180 // unknown: assume about three minutes
	}
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(song.Dir)))
	t := length * (0.30 + 0.25*float64(h.Sum32())/math.MaxUint32)
	for i := len(sections) - 1; i >= 0; i-- {
		if s := sections[i]; s.Time <= t {
			if t-s.Time <= 12 && s.Time > 0 {
				return max(0, s.Time-previewPreRoll), "section " + rich.Plain(s.Name)
			}
			break
		}
	}
	return t, fmt.Sprintf("%.0f%% in", 100*t/length)
}

// isChorus matches "Chorus 2", "chorus_1a", "CHORUS" but not pre- or
// post-choruses.
func isChorus(name string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(rich.Plain(name)) {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		}
	}
	n := b.String()
	return strings.Contains(n, "chorus") && !strings.Contains(n, "prechorus") && !strings.Contains(n, "postchorus")
}
