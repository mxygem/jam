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

// hwTexture is a texture ready to draw: one image holding the full size
// picture with its mip levels (each half the size of the one before) in a
// column to its right, so the shader can blend between levels per pixel and
// the far end of the highway doesn't shimmer.
type hwTexture struct {
	kind   texKind
	atlas  *ebiten.Image
	levels int
	w, h   float64 // level 0 size
	trueH  float64 // the picture's real height at width w, before h was rounded
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
	trueH  float64
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
			img, trueH, err := loadCustomTexture(strings.TrimPrefix(id, customPrefix))
			if err != nil {
				log.Printf("highway texture %s: %v", id, err)
				return
			}
			src, p.trueH = img, trueH
		} else {
			for _, d := range builtinTextures {
				if d.id == id {
					src, p.kind = d.gen(), d.kind
				}
			}
			if src == nil {
				return
			}
			p.trueH = float64(src.Bounds().Dy())
		}
		// The shader works out each level's size by halving, so stop before
		// a side would need rounding.
		for lv := src; ; lv = halve(lv) {
			p.levels = append(p.levels, lv)
			w, h := lv.Bounds().Dx(), lv.Bounds().Dy()
			if w <= 16 || h <= 16 || w%2 != 0 || h%2 != 0 || len(p.levels) >= 7 {
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
		t = &hwTexture{kind: p.kind, w: float64(b.Dx()), h: float64(b.Dy()), trueH: p.trueH, levels: len(p.levels)}
		t.atlas = ebiten.NewImageFromImage(mipAtlas(p.levels))
	}
	texMu.Lock()
	texCache[id] = t // nil for a file that can't be read, so it's tried once
	texMu.Unlock()
	return t
}

// customTexWidth is how wide a player's image is kept. The highway is 640
// wide on the 1920x1080 canvas and more on bigger screens.
const customTexWidth = 1024

// loadCustomTexture reads a player's image, scaled to customTexWidth with a
// height that halves cleanly a few times. It also returns the height that
// keeps the picture's real shape: the highway draws with that, so rounding
// the rows doesn't stretch the image.
func loadCustomTexture(rel string) (*image.RGBA, float64, error) {
	f, err := os.Open(filepath.Join(HighwayImageDir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, 0, err
	}
	b := img.Bounds()
	const w = customTexWidth
	trueH := float64(b.Dy()) * w / float64(b.Dx())
	h := int(math.Round(trueH/32)) * 32
	if h < 32 {
		h = 32
	}
	if h > 4096 {
		h = 4096
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst, trueH, nil
}

// mipAtlas lays the levels out for the shader: level 0 at the left and the
// rest stacked in a column beside it (see texShaderSrc).
func mipAtlas(levels []*image.RGBA) *image.RGBA {
	b := levels[0].Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w+w/2, h))
	xdraw.Copy(out, image.Point{}, levels[0], b, xdraw.Src, nil)
	y := 0
	for _, lv := range levels[1:] {
		xdraw.Copy(out, image.Pt(w, y), lv, lv.Bounds(), xdraw.Src, nil)
		y += lv.Bounds().Dy()
	}
	return out
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

// texLength is how long the board is in highway widths, worked out from the
// highway's shape: at the strike line a texel covers as many pixels along
// the board as across it, so a tile keeps its proportions there whatever the
// width, angle and length settings are.
func (h *Highway) texLength() float64 {
	l := (h.Cfg.StrikeY - h.horizon()) / (h.Cfg.NearWidth * h.far())
	return math.Max(1, math.Min(20, l))
}

// texShaderSrc paints the texture with true perspective: every pixel works
// out its own place on the board, which plain triangles can't do (they bend
// the picture along their edges). It blends mip levels the same way. custom
// holds the pixel's position on the highway's own target.
const texShaderSrc = `//kage:unit pixels
package main

var Horizon, Strike, Far, CenterX, NearWidth float
var Rows, Scroll, TexW, TexH, MaxLevel, ZMin float

func texel(x, y, ox, oy, lw, lh float) vec4 {
	return imageSrc0UnsafeAt(imageSrc0Origin() + vec2(ox+mod(x, lw)+0.5, oy+mod(y, lh)+0.5))
}

// level samples mip level lv at u (0..1 across) and v (level 0 rows).
func level(u, v, lv float) vec4 {
	k := pow(2, lv)
	lw := TexW / k
	lh := TexH / k
	ox := 0.0
	oy := 0.0
	if lv > 0.5 {
		ox = TexW
		oy = TexH * (1 - 2/k)
	}
	px := u*lw - 0.5
	py := v/k - 0.5
	fx := floor(px)
	fy := floor(py)
	a := px - fx
	b := py - fy
	top := mix(texel(fx, fy, ox, oy, lw, lh), texel(fx+1, fy, ox, oy, lw, lh), a)
	bot := mix(texel(fx, fy+1, ox, oy, lw, lh), texel(fx+1, fy+1, ox, oy, lw, lh), a)
	return mix(top, bot, b)
}

func Fragment(dst vec4, src vec2, color vec4, custom vec4) vec4 {
	s := (custom.y - Horizon) / (Strike - Horizon)
	z := Far/s - Far
	u := (custom.x-CenterX)/(NearWidth*s) + 0.5
	v := Scroll + z*Rows
	across := TexW / (NearWidth * s)
	along := Rows * Far / (s * s * (Strike - Horizon))
	lod := clamp(log2(max(across, along*0.75)), 0, MaxLevel)
	l0 := floor(lod)
	c := level(u, v, l0)
	if lod > l0 {
		c = mix(c, level(u, v, l0+1), lod-l0)
	}
	fade := 1.0
	if z > 0.86 {
		fade = max(0, (1-z)/0.14)
	}
	if z < 0 {
		fade = max(0, 1-z/ZMin)
	}
	return c * color * fade
}
`

var (
	texShaderOnce sync.Once
	texShader     *ebiten.Shader
)

func textureShader() *ebiten.Shader {
	texShaderOnce.Do(func() {
		s, err := ebiten.NewShader([]byte(texShaderSrc))
		if err != nil {
			log.Printf("highway texture shader: %v", err)
			return
		}
		texShader = s
	})
	return texShader
}

// drawTexture paints the texture over the highway surface between zMin
// and the far end. surf is the surface color (already tinted for star
// power), alpha the strength times the highway's opacity and length
// texLength from before any highway bounce.
func (h *Highway) drawTexture(dst *ebiten.Image, tex *hwTexture, lanes int, now, length float64, surf, beat, sp color.RGBA, spOn bool, alpha float64) {
	if tex == nil || alpha <= 0 || tex.atlas == nil {
		return
	}
	shader := textureShader()
	if shader == nil {
		return
	}
	// A tile spans the highway's width; the rows are spread to the picture's
	// real height so it isn't stretched by the rounding when it was loaded.
	rowsPerZ := length * tex.w * tex.h / tex.trueH
	scroll := math.Mod(now/h.Cfg.Lookahead*rowsPerZ, tex.h)

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

	c := premulRGBA(tint, alpha)
	x0n, x1n, yn := h.X(0, lanes, zMin), h.X(float64(lanes), lanes, zMin), h.Y(zMin)
	x0f, x1f, yf := h.X(0, lanes, 1), h.X(float64(lanes), lanes, 1), h.Y(1)
	vs := []ebiten.Vertex{texVtx(x0n, yn, c), texVtx(x1n, yn, c), texVtx(x1f, yf, c), texVtx(x0f, yf, c)}
	op := &ebiten.DrawTrianglesShaderOptions{}
	op.Images[0] = tex.atlas
	op.Uniforms = map[string]any{
		"Horizon": float32(h.horizon()), "Strike": float32(h.Cfg.StrikeY), "Far": float32(h.far()),
		"CenterX": float32(h.Cfg.CenterX), "NearWidth": float32(h.Cfg.NearWidth),
		"Rows": float32(rowsPerZ), "Scroll": float32(scroll), "TexW": float32(tex.w), "TexH": float32(tex.h),
		"MaxLevel": float32(tex.levels - 1), "ZMin": float32(zMin),
	}
	dst.DrawTrianglesShader(vs, []uint16{0, 1, 2, 0, 2, 3}, shader, op)
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

func texVtx(x, y float64, c color.RGBA) ebiten.Vertex {
	return ebiten.Vertex{
		DstX: float32(x), DstY: float32(y), Custom0: float32(x), Custom1: float32(y),
		ColorR: float32(c.R) / 255, ColorG: float32(c.G) / 255, ColorB: float32(c.B) / 255, ColorA: float32(c.A) / 255,
	}
}
