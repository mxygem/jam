package playback

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2/audio/mp3"
	"github.com/hajimehoshi/ebiten/v2/audio/vorbis"
	"github.com/hajimehoshi/ebiten/v2/audio/wav"
)

// Song previews: a short stretch of a song, every stem mixed into one
// buffer, with fades baked in so it can loop.

// Clip is a decoded preview.
type Clip struct {
	PCM   []byte  // 16-bit stereo at SampleRate, fades baked in, ready to loop
	Start float64 // song time the clip starts at
	Stems int     // stems mixed in
}

// Seconds is the clip's length including the silence before it loops.
func (c *Clip) Seconds() float64 { return float64(len(c.PCM)) / bytesPerSecond }

// Clip fades.
const (
	clipFadeIn  = 0.4
	clipFadeOut = 1.5
	clipGap     = 0.5 // silence before the clip loops
)

// PreviewStems picks the files to preview: a dedicated preview file when the
// song has one, otherwise every stem except the crowd.
func PreviewStems(stems map[string]string) (paths []string, dedicated bool) {
	if p, ok := stems["preview"]; ok {
		return []string{p}, true
	}
	for name, p := range stems {
		if name != "crowd" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, false
}

// DecodeClip decodes dur seconds from start of every file, mixes them and
// bakes in the loop fades. Files that fail to decode are skipped unless all
// of them do. Cancelling ctx stops any ffmpeg still running.
func DecodeClip(ctx context.Context, paths []string, start, dur float64, ffmpeg string) (*Clip, error) {
	if len(paths) == 0 {
		return nil, errors.New("song has no audio files")
	}
	start = max(0, start)
	type result struct {
		pcm []byte
		err error
	}
	results := make([]result, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pcm, err := decodeSegment(ctx, p, start, dur, ffmpeg)
			results[i] = result{pcm, err}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var parts [][]byte
	var errs []error
	for i, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(paths[i]), r.err))
			continue
		}
		parts = append(parts, r.pcm)
	}
	if len(parts) == 0 {
		return nil, errors.Join(errs...)
	}
	pcm := Mix(parts)
	BakeLoopFades(&pcm)
	return &Clip{PCM: pcm, Start: start, Stems: len(parts)}, nil
}

// decodeSegment returns dur seconds of a file from start as 16-bit stereo.
// ogg/mp3/wav decode natively; anything else (or an .ogg holding Opus) goes
// through ffmpeg, which only decodes the stretch asked for.
func decodeSegment(ctx context.Context, path string, start, dur float64, ffmpeg string) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".ogg" || ext == ".mp3" || ext == ".wav" {
		if pcm, err := decodeNativeSegment(path, ext, start, dur); err == nil {
			return pcm, nil
		}
	}
	if ffmpeg == "" {
		return nil, ErrNeedFFmpeg
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin",
		"-ss", fmt.Sprintf("%.3f", start), "-i", path, "-t", fmt.Sprintf("%.3f", dur),
		"-f", "s16le", "-acodec", "pcm_s16le", "-ac", "2", "-ar", fmt.Sprint(SampleRate), "-")
	hideWindow(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg: %s", msg)
	}
	return out.Bytes(), nil
}

func decodeNativeSegment(path, ext string, start, dur float64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var s io.ReadSeeker
	switch ext {
	case ".ogg":
		s, err = vorbis.DecodeWithSampleRate(SampleRate, f)
	case ".mp3":
		s, err = mp3.DecodeWithSampleRate(SampleRate, f)
	case ".wav":
		s, err = wav.DecodeWithSampleRate(SampleRate, f)
	}
	if err != nil {
		return nil, err
	}
	off := int64(start*SampleRate) * 4
	if _, err := s.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, int64(dur*SampleRate)*4)
	n, err := io.ReadFull(s, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf[:n&^3], nil
}

// Mix sums 16-bit stereo buffers sample by sample, clipping at full scale.
// Stems are split from the same master, so their plain sum is the song.
func Mix(parts [][]byte) []byte {
	if len(parts) == 1 {
		return parts[0][:len(parts[0])&^3]
	}
	n := 0
	for _, p := range parts {
		n = max(n, len(p)&^3)
	}
	out := make([]byte, n)
	for i := 0; i+1 < n; i += 2 {
		sum := 0
		for _, p := range parts {
			if i+1 < len(p) {
				sum += int(int16(binary.LittleEndian.Uint16(p[i:])))
			}
		}
		sum = max(math.MinInt16, min(math.MaxInt16, sum))
		binary.LittleEndian.PutUint16(out[i:], uint16(int16(sum)))
	}
	return out
}

// BakeLoopFades fades the start in and the end out, then adds a short
// silence, so the clip loops without clicks.
func BakeLoopFades(pcm *[]byte) {
	p := *pcm
	frames := len(p) / 4
	in := min(frames/4, int(clipFadeIn*SampleRate))
	out := min(frames/3, int(clipFadeOut*SampleRate))
	scale := func(frame int, g float64) {
		for c := 0; c < 2; c++ {
			i := frame*4 + c*2
			v := float64(int16(binary.LittleEndian.Uint16(p[i:])))
			binary.LittleEndian.PutUint16(p[i:], uint16(int16(v*g)))
		}
	}
	for f := 0; f < in; f++ {
		x := float64(f) / float64(in)
		scale(f, x*x)
	}
	for f := 0; f < out; f++ {
		x := float64(f) / float64(out) // 0 at the end
		scale(frames-1-f, x*x)
	}
	*pcm = append(p, make([]byte, int(clipGap*SampleRate)*4)...)
}

// Level is the loudness (RMS, 0..1) of the window of pcm starting at off
// bytes, for meters.
func Level(pcm []byte, off, window int64) float64 {
	off &^= 3
	if off < 0 || off >= int64(len(pcm)) {
		return 0
	}
	end := min(int64(len(pcm)), off+window&^3)
	var sum float64
	n := 0
	for i := off; i+1 < end; i += 8 { // every other frame is plenty
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768
		sum += v * v
		n++
	}
	if n == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(n))
}
