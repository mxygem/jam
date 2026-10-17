package playback

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// stairs is a signal whose value says what second it is: level*(1+floor(t)).
func stairs(seconds int, level float32) PCM {
	p := NewPCM(float64(seconds))
	for i := range p {
		p[i] = level * float32(1+i/SampleRate)
	}
	return p
}

// sampleAt reads the left channel at t seconds into a 16-bit stereo buffer.
func sampleAt(pcm []byte, t float64) float64 {
	i := int(t*SampleRate) * 4
	return float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32767
}

func writeWAV(t *testing.T, dir, name string, p PCM) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, p.WAV(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDecodeClipMixesStemsFromStart(t *testing.T) {
	dir := t.TempDir()
	a := writeWAV(t, dir, "song.wav", stairs(60, 0.004))
	b := writeWAV(t, dir, "guitar.wav", stairs(60, 0.002))
	clip, err := DecodeClip(context.Background(), []string{a, b}, 20, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if clip.Stems != 2 || clip.Start != 20 {
		t.Fatalf("stems %d start %v", clip.Stems, clip.Start)
	}
	if got, want := clip.Seconds(), 10+clipGap; math.Abs(got-want) > 0.01 {
		t.Fatalf("length %.3f, want %.3f", got, want)
	}
	// 3.5 s into the clip is 23.5 s into the song: second 24 of the stairs.
	if got, want := sampleAt(clip.PCM, 3.5), 0.006*24; math.Abs(got-want) > 0.001 {
		t.Fatalf("mixed sample %.4f, want %.4f", got, want)
	}
	// faded in from silence, faded out to silence, then the gap
	if v := sampleAt(clip.PCM, 0); v != 0 {
		t.Fatalf("first sample %v, want 0", v)
	}
	if v := sampleAt(clip.PCM, 9.999); math.Abs(v) > 0.001 {
		t.Fatalf("last sample before the gap %v, want ~0", v)
	}
	if v := Level(clip.PCM, int64(10.1*SampleRate)*4, 4096); v != 0 {
		t.Fatalf("gap level %v, want 0", v)
	}
}

func TestDecodeClipPastTheEndIsShort(t *testing.T) {
	dir := t.TempDir()
	a := writeWAV(t, dir, "song.wav", stairs(12, 0.01))
	clip, err := DecodeClip(context.Background(), []string{a}, 10, 30, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := clip.Seconds() - clipGap; math.Abs(got-2) > 0.01 {
		t.Fatalf("clip of the last 2 s is %.3f s", got)
	}
}

func TestDecodeClipSkipsBrokenStems(t *testing.T) {
	dir := t.TempDir()
	good := writeWAV(t, dir, "song.wav", stairs(20, 0.01))
	bad := filepath.Join(dir, "guitar.ogg")
	if err := os.WriteFile(bad, []byte("not audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	clip, err := DecodeClip(context.Background(), []string{good, bad}, 0, 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if clip.Stems != 1 {
		t.Fatalf("stems %d, want 1", clip.Stems)
	}
	if _, err := DecodeClip(context.Background(), []string{bad}, 0, 5, ""); !errors.Is(err, ErrNeedFFmpeg) && err == nil {
		t.Fatal("a clip with no decodable stem should fail")
	}
}

func TestDecodeClipThroughFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	src := writeWAV(t, dir, "src.wav", stairs(60, 0.004))
	flac := filepath.Join(dir, "song.flac")
	opus := filepath.Join(dir, "guitar.opus")
	for _, out := range []string{flac, opus} {
		// stereo like real stems (ffmpeg upmixes mono 3 dB down)
		if b, err := exec.Command(ffmpeg, "-v", "error", "-i", src, "-af", "pan=stereo|c0=c0|c1=c0", out).CombinedOutput(); err != nil {
			t.Fatalf("encode %s: %v %s", out, err, b)
		}
	}
	// flac is lossless: the stairs come back exactly where they belong
	clip, err := DecodeClip(context.Background(), []string{flac}, 30, 8, ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sampleAt(clip.PCM, 2.5), 0.004*33; math.Abs(got-want) > 0.0005 {
		t.Fatalf("flac sample %.4f, want %.4f", got, want)
	}
	if got := clip.Seconds() - clipGap; math.Abs(got-8) > 0.05 {
		t.Fatalf("flac clip %.3f s, want 8", got)
	}
	// opus is lossy; just check it decodes the right amount of sound
	clip, err = DecodeClip(context.Background(), []string{opus, flac}, 30, 8, ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	if clip.Stems != 2 || math.Abs(clip.Seconds()-clipGap-8) > 0.05 {
		t.Fatalf("opus+flac: %d stems, %.3f s", clip.Stems, clip.Seconds())
	}
	// without ffmpeg these formats can't be previewed
	if _, err := DecodeClip(context.Background(), []string{opus}, 0, 5, ""); !errors.Is(err, ErrNeedFFmpeg) {
		t.Fatalf("err %v, want ErrNeedFFmpeg", err)
	}
	// cancelling stops the decode
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeClip(ctx, []string{opus}, 0, 5, ffmpeg); err == nil {
		t.Fatal("cancelled decode should fail")
	}
}

func TestMixClips(t *testing.T) {
	loud := PCM{0.8, -0.8, 0.1}.Bytes()
	short := PCM{0.8, -0.8}.Bytes()
	out := Mix([][]byte{loud, short})
	if len(out) != len(loud) {
		t.Fatalf("mix length %d, want %d", len(out), len(loud))
	}
	if v := sampleAt(out, 0); v < 0.999 {
		t.Fatalf("clipped sum %v, want full scale", v)
	}
	if v := sampleAt(out, 1.0/SampleRate); v > -0.999 {
		t.Fatalf("clipped negative sum %v, want full scale", v)
	}
	if v := sampleAt(out, 2.0/SampleRate); math.Abs(v-0.1) > 0.001 {
		t.Fatalf("tail %v, want 0.1 from the longer stem only", v)
	}
}

func TestPreviewStems(t *testing.T) {
	stems := map[string]string{"song": "s.ogg", "guitar": "g.ogg", "crowd": "c.ogg"}
	paths, dedicated := PreviewStems(stems)
	if dedicated || len(paths) != 2 {
		t.Fatalf("got %v %v, want song and guitar without the crowd", paths, dedicated)
	}
	stems["preview"] = "p.ogg"
	paths, dedicated = PreviewStems(stems)
	if !dedicated || len(paths) != 1 || paths[0] != "p.ogg" {
		t.Fatalf("got %v %v, want the preview file alone", paths, dedicated)
	}
}
