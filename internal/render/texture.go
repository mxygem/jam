package render

import (
	"image"
	"image/color"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	xdraw "golang.org/x/image/draw"

	// image decoders for custom highway images
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// Highway textures are images painted on the highway surface. They scroll
// with the notes, so the board itself looks like it moves. Built-in ones
// are drawn in code (tileable, no files); players can add their own
// images under a "highways" folder in the customs folder.

// texKind says how a texture's pixels combine with the theme.
type texKind uint8

const (
	texColor   texKind = iota // its own colors (wood)
	texTint                   // gray detail, colored by the theme's highway color
	texOverlay                // white marks on transparent, colored by the beat line color
)

type texDef struct {
	id, label string
	kind      texKind
	gen       func() *image.RGBA
}

// The built-in textures. The ids are what settings and theme files store.
var builtinTextures = []texDef{
	{"rosewood", "Rosewood", texColor, func() *image.RGBA { return genWood(woodRose) }},
	{"ebony", "Ebony", texColor, func() *image.RGBA { return genWood(woodEbony) }},
	{"maple", "Maple", texColor, func() *image.RGBA { return genWood(woodMaple) }},
	{"brushed", "Brushed metal", texTint, genBrushed},
	{"carbon", "Carbon fiber", texTint, genCarbon},
	{"diamond", "Diamond plate", texTint, genDiamond},
	{"concrete", "Concrete", texTint, genConcrete},
	{"stars", "Starfield", texOverlay, genStars},
	{"hex", "Hex mesh", texOverlay, genHex},
}

// TextureNone is the plain surface.
const TextureNone = ""

// customPrefix marks a custom image: "custom:highways/board.png", relative
// to HighwayImageDir.
const customPrefix = "custom:"

// HighwayImageDir is the customs folder (set by the app). Images in any
// folder under it with "highway" in its name can be highway textures.
var HighwayImageDir string

// IsHighwayImageDir reports whether rel (a folder relative to the customs
// folder) holds highway images.
func IsHighwayImageDir(rel string) bool {
	return strings.Contains(strings.ToLower(filepath.ToSlash(rel)), "highway")
}

var (
	customMu    sync.Mutex
	customFiles []string
	customAt    bool
)

// CustomHighwayImages lists the player's highway images (relative to
// HighwayImageDir, slash separated, sorted). rescan looks at the folder again.
func CustomHighwayImages(rescan bool) []string {
	customMu.Lock()
	defer customMu.Unlock()
	if customAt && !rescan {
		return customFiles
	}
	if rescan {
		// let changed images load again
		texMu.Lock()
		for id := range texSrcs {
			if strings.HasPrefix(id, customPrefix) {
				delete(texSrcs, id)
				delete(texCache, id)
			}
		}
		texMu.Unlock()
	}
	customAt = true
	customFiles = nil
	if HighwayImageDir == "" {
		return nil
	}
	_ = filepath.WalkDir(HighwayImageDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".png", ".jpg", ".jpeg", ".webp":
		default:
			return nil
		}
		rel, err := filepath.Rel(HighwayImageDir, p)
		if err != nil || !IsHighwayImageDir(filepath.Dir(rel)) {
			return nil
		}
		customFiles = append(customFiles, filepath.ToSlash(rel))
		return nil
	})
	sort.Slice(customFiles, func(i, j int) bool { return strings.ToLower(customFiles[i]) < strings.ToLower(customFiles[j]) })
	return customFiles
}

// HighwayTextures lists every texture id in menu order: none, the
// built-ins, then the player's images.
func HighwayTextures() []string {
	out := []string{TextureNone}
	for _, d := range builtinTextures {
		out = append(out, d.id)
	}
	for _, f := range CustomHighwayImages(false) {
		out = append(out, customPrefix+f)
	}
	return out
}

// TextureLabel is how menus show a texture id.
func TextureLabel(id string) string {
	if id == TextureNone {
		return "None"
	}
	if strings.HasPrefix(id, customPrefix) {
		name := filepath.Base(strings.TrimPrefix(id, customPrefix))
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	for _, d := range builtinTextures {
		if d.id == id {
			return d.label
		}
	}
	return id
}

// ParseTexture cleans up a texture id from a settings or theme file.
// Unknown built-in ids give none; custom ids are kept as written.
func ParseTexture(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, customPrefix) {
		return customPrefix + strings.ReplaceAll(strings.TrimPrefix(s, customPrefix), `\`, "/")
	}
	s = strings.ToLower(s)
	for _, d := range builtinTextures {
		if d.id == s {
			return s
		}
	}
	return TextureNone
}

// hwTexture is a texture ready to draw: mip levels, each half the size of
// the one before, so the far end of the highway doesn't shimmer.
type hwTexture struct {
	kind   texKind
	levels []*ebiten.Image
	w, h   float64 // level 0 size
}

var (
	texMu    sync.Mutex
	texCache = map[string]*hwTexture{}
	texSrcs  = map[string]*texPixels{}
)

// texPixels is a texture's mip levels in memory, made once (in the
// background when PrewarmTexture asks first).
type texPixels struct {
	once   sync.Once
	kind   texKind
	levels []*image.RGBA
}

func pixelsFor(id string) *texPixels {
	texMu.Lock()
	p, ok := texSrcs[id]
	if !ok {
		p = &texPixels{}
		texSrcs[id] = p
	}
	texMu.Unlock()
	p.once.Do(func() {
		var src *image.RGBA
		if strings.HasPrefix(id, customPrefix) {
			img, err := loadCustomTexture(strings.TrimPrefix(id, customPrefix))
			if err != nil {
				log.Printf("highway texture %s: %v", id, err)
				return
			}
			src = img
		} else {
			for _, d := range builtinTextures {
				if d.id == id {
					src, p.kind = d.gen(), d.kind
				}
			}
			if src == nil {
				return
			}
		}
		for lv := src; ; lv = halve(lv) {
			p.levels = append(p.levels, lv)
			if lv.Bounds().Dx() <= 16 || lv.Bounds().Dy() <= 16 || len(p.levels) >= 7 {
				break
			}
		}
	})
	return p
}

// PrewarmTexture starts drawing a texture in the background so it's ready
// when the highway needs it.
func PrewarmTexture(id string) {
	if id != TextureNone {
		go pixelsFor(id)
	}
}

// loadTexture returns the texture for id (nil for none or a file that
// can't be read). Built-ins are drawn the first time they're asked for.
func loadTexture(id string) *hwTexture {
	if id == TextureNone {
		return nil
	}
	texMu.Lock()
	t, ok := texCache[id]
	texMu.Unlock()
	if ok {
		return t
	}
	p := pixelsFor(id)
	if len(p.levels) > 0 {
		b := p.levels[0].Bounds()
		t = &hwTexture{kind: p.kind, w: float64(b.Dx()), h: float64(b.Dy())}
		for _, lv := range p.levels {
			t.levels = append(t.levels, ebiten.NewImageFromImage(lv))
		}
	}
	texMu.Lock()
	texCache[id] = t // nil for a file that can't be read, so it's tried once
	texMu.Unlock()
	return t
}

// loadCustomTexture reads a player's image, scaled to 512 wide with a
// height that halves cleanly a few times.
func loadCustomTexture(rel string) (*image.RGBA, error) {
	f, err := os.Open(filepath.Join(HighwayImageDir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	const w = 512
	h := int(math.Round(float64(b.Dy())*w/float64(b.Dx())/32)) * 32
	if h < 32 {
		h = 32
	}
	if h > 4096 {
		h = 4096
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst, nil
}

// halve box-filters an image to half size (wrapping, so tiles stay tileable).
func halve(src *image.RGBA) *image.RGBA {
	w, h := src.Bounds().Dx()/2, src.Bounds().Dy()/2
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var s [4]int
			for _, d := range [4][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				o := src.PixOffset(2*x+d[0], 2*y+d[1])
				for k := 0; k < 4; k++ {
					s[k] += int(src.Pix[o+k])
				}
			}
			o := dst.PixOffset(x, y)
			for k := 0; k < 4; k++ {
				dst.Pix[o+k] = uint8((s[k] + 2) / 4)
			}
		}
	}
	return dst
}

// HighwayLength is how long the visible highway is, in highway widths; it
// sets how a texture's height maps onto the board.
const HighwayLength = 3.2

var texOpts = &ebiten.DrawTrianglesOptions{Filter: ebiten.FilterLinear, Address: ebiten.AddressRepeat, DisableMipmaps: true}

// drawTexture paints the texture over the highway surface between zMin
// and the far end. surf is the surface color (already tinted for star
// power) and alpha the strength times the highway's opacity.
func (h *Highway) drawTexture(dst *ebiten.Image, tex *hwTexture, lanes int, now float64, surf, beat, sp color.RGBA, spOn bool, alpha float64) {
	if tex == nil || alpha <= 0 || len(tex.levels) == 0 {
		return
	}
	// Strips evenly spaced on screen (perspective makes texture
	// interpolation wrong inside each one; thin strips hide it), split
	// at the lane lines so each piece is a near-rectangle.
	const strips = 56
	yNear, yFar := h.Y(zMin), h.Y(1)
	hz := h.horizon()
	zOf := func(y float64) float64 {
		s := (y - hz) / (h.Cfg.StrikeY - hz)
		d := h.far()
		return d/s - d
	}
	// Texture rows per unit of depth: one tile is (h/w) highway widths long.
	rowsPerZ := HighwayLength * tex.w // a tile spans the highway's width; texels are square on the board
	scroll := math.Mod(now/h.Cfg.Lookahead*rowsPerZ, tex.h)
	v := func(z float64) float64 { return scroll + z*rowsPerZ }

	var tint color.RGBA
	switch tex.kind {
	case texColor:
		tint = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	case texTint:
		tint = Straight(Lighten(surf, 0.4))
		tint.A = 0xFF
	case texOverlay:
		tint = Straight(beat)
		tint.A = 0xFF
	}

	type piece struct {
		v []ebiten.Vertex
		i []uint16
	}
	per := make([]piece, len(tex.levels))
	cols := lanes
	if cols < 4 {
		cols = 4
	}
	for s := 0; s < strips; s++ {
		y0 := yNear + (yFar-yNear)*float64(s)/strips
		y1 := yNear + (yFar-yNear)*float64(s+1)/strips
		z0, z1 := zOf(y0), zOf(y1)
		if s == 0 {
			z0 = zMin
		}
		if s == strips-1 {
			z1 = 1
		}
		y0, y1 = h.Y(z0), h.Y(z1)
		a0 := alpha * fadeFar(z0) * fadePast(z0)
		a1 := alpha * fadeFar(z1) * fadePast(z1)
		if a0 <= 0 && a1 <= 0 {
			continue
		}
		// Pick the level from how many texels land on a screen pixel,
		// across and along the highway (along is usually the squashed one).
		zm := (z0 + z1) / 2
		across := tex.w / (h.X(float64(lanes), lanes, zm) - h.X(0, lanes, zm))
		along := (v(z1) - v(z0)) / math.Max(0.5, y0-y1)
		lod := math.Log2(math.Max(across, along*0.75))
		lv := int(math.Max(0, math.Floor(lod+0.15)))
		if lv >= len(tex.levels) {
			lv = len(tex.levels) - 1
		}
		k := 1 / math.Pow(2, float64(lv))
		c0, c1 := premulRGBA(tint, a0), premulRGBA(tint, a1)
		p := &per[lv]
		for c := 0; c < cols; c++ {
			p0, p1 := float64(c)*float64(lanes)/float64(cols), float64(c+1)*float64(lanes)/float64(cols)
			u0, u1 := p0/float64(lanes)*tex.w*k, p1/float64(lanes)*tex.w*k
			base := uint16(len(p.v))
			p.v = append(p.v,
				texVtx(h.X(p0, lanes, z0), y0, u0, v(z0)*k, c0),
				texVtx(h.X(p1, lanes, z0), y0, u1, v(z0)*k, c0),
				texVtx(h.X(p1, lanes, z1), y1, u1, v(z1)*k, c1),
				texVtx(h.X(p0, lanes, z1), y1, u0, v(z1)*k, c1))
			p.i = append(p.i, base, base+1, base+2, base, base+2, base+3)
		}
	}
	for lv, p := range per {
		if len(p.i) > 0 {
			dst.DrawTriangles(p.v, p.i, tex.levels[lv], texOpts)
		}
	}
	// Wood keeps its own colors, so star power tints it on top.
	if spOn && tex.kind == texColor {
		var b Batch
		steps := []float64{zMin, 0, 0.86, 1}
		for i := 0; i < len(steps)-1; i++ {
			z0, z1 := steps[i], steps[i+1]
			ca := WithAlpha(sp, 0.5*alpha*fadeFar(z0)*fadePast(z0))
			cb := WithAlpha(sp, 0.5*alpha*fadeFar(z1)*fadePast(z1))
			b.Quad(dst, h.X(0, lanes, z0), h.Y(z0), h.X(float64(lanes), lanes, z0), h.Y(z0),
				h.X(float64(lanes), lanes, z1), h.Y(z1), h.X(0, lanes, z1), h.Y(z1), ca, ca, cb, cb)
		}
		b.Flush(dst)
	}
}

// premulRGBA is a straight color at alpha a, premultiplied.
func premulRGBA(c color.RGBA, a float64) color.RGBA {
	a = math.Max(0, math.Min(1, a))
	return color.RGBA{uint8(float64(c.R) * a), uint8(float64(c.G) * a), uint8(float64(c.B) * a), uint8(255 * a)}
}

func texVtx(x, y, sx, sy float64, c color.RGBA) ebiten.Vertex {
	return ebiten.Vertex{
		DstX: float32(x), DstY: float32(y), SrcX: float32(sx), SrcY: float32(sy),
		ColorR: float32(c.R) / 255, ColorG: float32(c.G) / 255, ColorB: float32(c.B) / 255, ColorA: float32(c.A) / 255,
	}
}
