package app

import (
	"image"
	"io/fs"
	"log"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	xdraw "golang.org/x/image/draw"

	"github.com/mxygem/jam/internal/config"
	"github.com/mxygem/jam/internal/render"
)

// Background images come from the customs folder next to the songs folder
// (%AppData%\jam\customs on Windows): any image under a folder with
// "background" in its name, or every image there if no such folder exists.

const (
	bgDefault = ""       // the built-in gradient
	bgRandom  = "random" // a different image each song
)

// CustomsDir is where custom images live.
func CustomsDir() string { return filepath.Join(config.Dir(), "customs") }

type backgrounds struct {
	dir string

	mu       sync.Mutex
	files    []string // relative to dir, sorted
	scanned  bool
	want     string      // file to show ("" = none)
	decoding string      // file being decoded
	decoded  image.Image // ready for upload to the GPU
	decodedF string
	shown    string // file currently on screen
	img      *ebiten.Image
	failed   map[string]bool
}

func newBackgrounds(dir string) *backgrounds {
	return &backgrounds{dir: dir, failed: map[string]bool{}}
}

func isImageFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}

// List returns the available images (scanning the folder the first time).
func (b *backgrounds) List() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.scanned {
		b.files = scanBackgrounds(b.dir)
		b.scanned = true
	}
	return b.files
}

// Rescan looks at the folder again.
func (b *backgrounds) Rescan() []string {
	b.mu.Lock()
	b.scanned = false
	b.mu.Unlock()
	return b.List()
}

func scanBackgrounds(dir string) []string {
	var tagged, all []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isImageFile(p) {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		all = append(all, rel)
		if strings.Contains(strings.ToLower(filepath.Dir(rel)), "background") {
			tagged = append(tagged, rel)
		}
		return nil
	})
	out := tagged
	if len(out) == 0 {
		out = all
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// Apply shows what the setting asks for. reroll picks a new random image
// when the setting is "random" (each song gets its own).
func (b *backgrounds) Apply(setting string, reroll bool) {
	files := b.List()
	b.mu.Lock()
	defer b.mu.Unlock()
	switch setting {
	case bgDefault:
		b.want = ""
	case bgRandom:
		if len(files) == 0 {
			b.want = ""
		} else if reroll || b.want == "" {
			pick := files[rand.Intn(len(files))]
			if len(files) > 1 {
				for pick == b.want {
					pick = files[rand.Intn(len(files))]
				}
			}
			b.want = pick
		}
	default:
		b.want = setting
	}
}

// Showing is the image currently wanted ("" for the gradient).
func (b *backgrounds) Showing() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.want
}

// update runs on the main thread each frame: starts decodes, uploads
// decoded images and hands the current one to the renderer.
func (b *backgrounds) update(a *App) {
	if b == nil {
		return
	}
	set := a.Settings
	b.mu.Lock()
	want := b.want
	if want != b.shown && b.decoded != nil && b.decodedF == want {
		if b.img != nil {
			b.img.Deallocate()
		}
		b.img = ebiten.NewImageFromImage(b.decoded)
		b.shown = want
		b.decoded = nil
	}
	if want == "" && b.shown != "" {
		if b.img != nil {
			b.img.Deallocate()
		}
		b.img, b.shown = nil, ""
	}
	start := want != "" && want != b.shown && b.decoding == "" && b.decodedF != want && !b.failed[want]
	if start {
		b.decoding = want
	}
	img := b.img
	b.mu.Unlock()
	if start {
		tw, th := screenPixels()
		go b.decode(want, tw, th)
	}
	if img != nil {
		render.SetBackgroundImage(img, set.BackgroundDim, set.BackgroundMenus)
	} else {
		render.SetBackgroundImage(nil, 0, false)
	}
}

// busy reports whether the wanted image is still loading.
func (b *backgrounds) busy() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.want != "" && b.want != b.shown && !b.failed[b.want]
}

// image is the background image on screen now (nil for the gradient).
func (b *backgrounds) image() *ebiten.Image {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shown != b.want {
		return nil
	}
	return b.img
}

// screenPixels is the monitor size in pixels, for scaling big images down.
func screenPixels() (int, int) {
	m := ebiten.Monitor()
	if m == nil {
		return 3840, 2160
	}
	w, h := m.Size()
	s := m.DeviceScaleFactor()
	if w <= 0 || h <= 0 {
		return 3840, 2160
	}
	return int(float64(w) * s), int(float64(h) * s)
}

func (b *backgrounds) decode(rel string, tw, th int) {
	img, err := loadBackground(filepath.Join(b.dir, rel), tw, th)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.decoding = ""
	if err != nil {
		log.Printf("background %s: %v", rel, err)
		b.failed[rel] = true
		return
	}
	b.decoded, b.decodedF = img, rel
}

// loadBackground decodes an image and scales it down, if it's bigger, to
// just cover a tw x th screen.
func loadBackground(path string, tw, th int) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	bd := img.Bounds()
	iw, ih := bd.Dx(), bd.Dy()
	s := math.Max(float64(tw)/float64(iw), float64(th)/float64(ih))
	if s >= 1 || tw <= 0 || th <= 0 {
		return img, nil
	}
	w, h := max(1, int(math.Round(float64(iw)*s))), max(1, int(math.Round(float64(ih)*s)))
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), img, bd, xdraw.Src, nil)
	return out, nil
}

// bgLabel is how a background setting is shown in the menu.
func bgLabel(setting string) string {
	switch setting {
	case bgDefault:
		return "Default"
	case bgRandom:
		return "Random each song"
	}
	name := strings.TrimSuffix(filepath.Base(setting), filepath.Ext(setting))
	return name
}

// cycleBackground steps the setting through Default, Random and each image.
func cycleBackground(cur string, files []string, d int) string {
	opts := append([]string{bgDefault, bgRandom}, files...)
	i := 0
	for k, o := range opts {
		if o == cur {
			i = k
		}
	}
	i = ((i+d)%len(opts) + len(opts)) % len(opts)
	return opts[i]
}

// openFolder shows a folder in the system file browser, creating it first.
func openFolder(a *App, dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.Toast("Couldn't create " + dir + ": " + err.Error())
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		a.Toast("Couldn't open " + dir + ": " + err.Error())
		return
	}
	go func() { _ = cmd.Wait() }()
}
