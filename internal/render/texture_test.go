package render

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// colDiff is the mean difference between two columns (or rows) of pixels.
func lineDiff(img *image.RGBA, a, b int, cols bool) float64 {
	n := img.Bounds().Dy()
	if !cols {
		n = img.Bounds().Dx()
	}
	sum := 0.0
	for k := 0; k < n; k++ {
		var p, q int
		if cols {
			p, q = img.PixOffset(a, k), img.PixOffset(b, k)
		} else {
			p, q = img.PixOffset(k, a), img.PixOffset(k, b)
		}
		for c := 0; c < 4; c++ {
			sum += math.Abs(float64(img.Pix[p+c]) - float64(img.Pix[q+c]))
		}
	}
	return sum / float64(n)
}

// Every built-in texture tiles: the seam between the last and first
// column (and row) is no bigger than the steps inside the image.
func TestBuiltinTexturesTile(t *testing.T) {
	for _, d := range builtinTextures {
		start := time.Now()
		img := d.gen()
		t.Logf("%s: drawn in %v", d.id, time.Since(start).Round(time.Millisecond))
		if img.Bounds().Dx() != texW || img.Bounds().Dy() != texH {
			t.Fatalf("%s: size %v", d.id, img.Bounds())
		}
		for _, cols := range []bool{true, false} {
			last := texH - 1
			if cols {
				last = texW - 1
			}
			seam := lineDiff(img, last, 0, cols)
			inside := 0.0
			for _, k := range []int{1, last / 3, last / 2, (last + 1) / 2, 2 * last / 3, last - 1} { // (last+1)/2 is on a cell edge
				inside = math.Max(inside, lineDiff(img, k-1, k, cols))
			}
			if seam > inside*1.6+2 {
				t.Errorf("%s: seam across %v is %.1f, steps inside up to %.1f", d.id, map[bool]string{true: "u", false: "v"}[cols], seam, inside)
			}
		}
		if os.Getenv("JAM_TEX_OUT") != "" {
			f, err := os.Create(filepath.Join(os.Getenv("JAM_TEX_OUT"), d.id+".png"))
			if err == nil {
				_ = png.Encode(f, img)
				f.Close()
			}
		}
	}
}

func TestParseTexture(t *testing.T) {
	for in, want := range map[string]string{
		"":                          TextureNone,
		"Rosewood":                  "rosewood",
		" hex ":                     "hex",
		"plaid":                     TextureNone,
		`custom:highways\board.png`: "custom:highways/board.png",
		"custom:highways/x/y.jpg":   "custom:highways/x/y.jpg",
	} {
		if got := ParseTexture(in); got != want {
			t.Errorf("ParseTexture(%q) = %q, want %q", in, got, want)
		}
	}
	for _, id := range HighwayTextures() {
		if TextureLabel(id) == "" || TextureLabel(id) == id && id != TextureNone {
			t.Errorf("%q has no label", id)
		}
	}
}

func TestTextureInThemeSpec(t *testing.T) {
	l := DefaultLook()
	l.Texture, l.TextureStrength = "carbon", 0.45
	spec := CaptureSpec("x", &jamTheme, l)
	_, got := spec.Resolve(1)
	if got.Texture != "carbon" || got.TextureStrength != 0.45 {
		t.Errorf("round trip gave %q %.2f", got.Texture, got.TextureStrength)
	}
	// themes from before textures resolve to none at the default strength
	_, old := (&ThemeSpec{Name: "old", Material: "metal"}).Resolve(1)
	if old.Texture != TextureNone || old.TextureStrength != DefaultLook().TextureStrength {
		t.Errorf("old theme gave %q %.2f", old.Texture, old.TextureStrength)
	}
	for _, s := range BuiltinThemes {
		if s.Texture != "" && ParseTexture(s.Texture) != s.Texture {
			t.Errorf("%s: unknown texture %q", s.Name, s.Texture)
		}
	}
}

func TestCustomHighwayImages(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string, w, h int) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for i := range img.Pix {
			img.Pix[i] = uint8(i)
		}
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		_ = png.Encode(f, img)
		f.Close()
	}
	write("highways/Board.png", 300, 700)
	write("My Highways/sub/b.png", 64, 64)
	write("backgrounds/sky.png", 64, 64)
	write("loose.png", 64, 64)
	_ = os.WriteFile(filepath.Join(dir, "highways", "notes.txt"), []byte("hi"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "highways", "broken.png"), []byte("not a png"), 0o644)

	saved := HighwayImageDir
	HighwayImageDir = dir
	defer func() { HighwayImageDir = saved; CustomHighwayImages(true) }()

	got := CustomHighwayImages(true)
	want := []string{"highways/Board.png", "highways/broken.png", "My Highways/sub/b.png"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if TextureLabel("custom:highways/Board.png") != "Board" {
		t.Errorf("label %q", TextureLabel("custom:highways/Board.png"))
	}
	tex := loadTexture("custom:highways/Board.png")
	if tex == nil || tex.w != 512 || int(tex.h)%32 != 0 || len(tex.levels) < 4 {
		t.Fatalf("custom texture: %+v", tex)
	}
	if loadTexture("custom:highways/broken.png") != nil || loadTexture("custom:highways/missing.png") != nil {
		t.Error("unreadable images should give no texture")
	}
	if loadTexture(TextureNone) != nil {
		t.Error("none should give no texture")
	}
}

func TestHalveKeepsAverage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for i := 0; i < 8; i++ {
		img.Set(i%4, i/4, color.RGBA{uint8(i * 30), 0, 0, 255})
	}
	h := halve(img)
	if h.Bounds().Dx() != 2 || h.Bounds().Dy() != 1 {
		t.Fatalf("size %v", h.Bounds())
	}
	// (0 + 30 + 120 + 150) / 4 = 75
	if r := h.RGBAAt(0, 0).R; r != 75 {
		t.Errorf("got %d, want 75", r)
	}
}
