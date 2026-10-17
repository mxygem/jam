package app

import (
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	_ "golang.org/x/image/webp"

	"github.com/mxygem/jam/internal/clock"
	"github.com/mxygem/jam/internal/render"
)

// Album art is decoded off the main thread; the GPU images are made on it.

type artEntry struct {
	path    string
	decoded image.Image // set by the loader
	thumb   *image.RGBA // tiny version for the blurred backdrop
	err     error
	sharp   *ebiten.Image
	blur    *ebiten.Image
	ready   bool
	used    int
}

type artCache struct {
	mu      sync.Mutex
	entries map[string]*artEntry
	tick    int
}

func newArtCache() *artCache { return &artCache{entries: map[string]*artEntry{}} }

// get returns art for path, starting a load if needed. It returns nil until
// the images are ready (or if the file can't be read).
func (c *artCache) get(path string) *artEntry {
	if path == "" {
		return nil
	}
	c.mu.Lock()
	c.tick++
	e, ok := c.entries[path]
	if !ok {
		e = &artEntry{path: path}
		c.entries[path] = e
		c.evict()
		c.mu.Unlock()
		go c.load(e)
		return nil
	}
	e.used = c.tick
	decoded, thumb, err := e.decoded, e.thumb, e.err
	c.mu.Unlock()
	if err != nil {
		return nil
	}
	if !e.ready && decoded != nil {
		// GPU images are made on the main thread.
		e.sharp = ebiten.NewImageFromImage(decoded)
		e.blur = ebiten.NewImageFromImage(thumb)
		e.ready = true
		c.mu.Lock()
		e.decoded = nil // free the full size copy
		c.mu.Unlock()
	}
	if !e.ready {
		return nil
	}
	return e
}

// pending reports whether art for path is still loading.
func (c *artCache) pending(path string) bool {
	if path == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[path]
	return ok && e.err == nil && !e.ready
}

func (c *artCache) load(e *artEntry) {
	f, err := os.Open(e.path)
	if err != nil {
		c.mu.Lock()
		e.err = err
		c.mu.Unlock()
		return
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		log.Printf("album art %s: %v", e.path, err)
		c.mu.Lock()
		e.err = err
		c.mu.Unlock()
		return
	}
	img = fitWithin(img, 512) // the panel shows ~300 px; keep GPU memory small
	thumb := shrink(img, 24)
	c.mu.Lock()
	e.decoded, e.thumb = img, thumb
	c.mu.Unlock()
}

// evict keeps the cache to 32 entries (called with the lock held).
func (c *artCache) evict() {
	for len(c.entries) > 32 {
		var oldest *artEntry
		for _, e := range c.entries {
			if oldest == nil || e.used < oldest.used {
				oldest = e
			}
		}
		if oldest.sharp != nil {
			oldest.sharp.Deallocate()
			oldest.blur.Deallocate()
		}
		delete(c.entries, oldest.path)
	}
}

// fitWithin scales img down (box filter) so its larger side is at most n.
func fitWithin(img image.Image, n int) image.Image {
	b := img.Bounds()
	if b.Dx() <= n && b.Dy() <= n {
		return img
	}
	w, h := n, b.Dy()*n/b.Dx()
	if b.Dy() > b.Dx() {
		w, h = b.Dx()*n/b.Dy(), n
	}
	return boxScale(img, max(1, w), max(1, h))
}

// shrink makes a tiny square-ish thumbnail; stretched back up with linear
// filtering it reads as a soft blur.
func shrink(img image.Image, n int) *image.RGBA {
	return boxScale(img, n, n)
}

func boxScale(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+(y+1)*b.Dy()/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/w, b.Min.X+(x+1)*b.Dx()/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			// sample a grid of at most 8x8 pixels per output pixel
			sx := max(1, (x1-x0)/8)
			sy := max(1, (y1-y0)/8)
			var r, g, bl, a, n uint32
			for yy := y0; yy < y1; yy += sy {
				for xx := x0; xx < x1; xx += sx {
					cr, cg, cb, ca := img.At(xx, yy).RGBA()
					r, g, bl, a, n = r+cr, g+cg, bl+cb, a+ca, n+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), uint8(a / n >> 8)})
		}
	}
	return out
}

// backdrop draws the selected song's art blurred across the screen at about
// half strength, cross-fading when the selection changes.
type backdrop struct {
	cur, prev *artEntry
	since     float64
}

func (b *backdrop) set(e *artEntry) {
	if e == b.cur {
		return
	}
	b.prev, b.cur = b.cur, e
	b.since = clock.Now()
}

func (b *backdrop) draw(dst *ebiten.Image) {
	k := (clock.Now() - b.since) / 0.35
	if k > 1 {
		k = 1
		b.prev = nil
	}
	full, w, h, _, _ := render.Backdrop(dst)
	if b.prev != nil {
		drawCover(full, b.prev.blur, w, h, 0.5*(1-k))
	}
	if b.cur != nil {
		drawCover(full, b.cur.blur, w, h, 0.5*k)
	}
	// keep text readable over bright art
	if b.cur != nil || b.prev != nil {
		render.FillRect(full, 0, 0, w, h, color.RGBA{0x08, 0x08, 0x10, 0x60})
	}
}

// drawCover scales img to cover a w x h area at the origin (cropping the
// overflow).
func drawCover(dst, img *ebiten.Image, w, h, alpha float64) {
	if img == nil || alpha <= 0 {
		return
	}
	iw, ih := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
	s := max(w/iw, h/ih)
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(s, s)
	op.GeoM.Translate((w-iw*s)/2, (h-ih*s)/2)
	op.ColorScale.ScaleAlpha(float32(alpha))
	dst.DrawImage(img, op)
}

// drawFit draws img scaled to fit a w x h box at x, y.
func drawFit(dst, img *ebiten.Image, x, y, w, h float64) {
	iw, ih := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
	s := min(w/iw, h/ih)
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(s, s)
	op.GeoM.Translate(x+(w-iw*s)/2, y+(h-ih*s)/2)
	dst.DrawImage(img, op)
}
