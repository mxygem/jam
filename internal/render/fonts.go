package render

import (
	"bytes"
	_ "embed"
	"image/color"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/gomono"
)

// Barlow by The Barlow Project Authors, SIL Open Font License 1.1 (fonts/OFL.txt).
var (
	//go:embed fonts/Barlow-SemiBold.ttf
	barlowSemiBold []byte
	//go:embed fonts/Barlow-Bold.ttf
	barlowBold []byte
	//go:embed fonts/BarlowCondensed-Medium.ttf
	condMedium []byte
	//go:embed fonts/BarlowCondensed-Bold.ttf
	condBold []byte
	//go:embed fonts/BarlowCondensed-ExtraBold.ttf
	condExtraBold []byte
)

// Font selects a typeface.
type Font uint8

const (
	UI       Font = iota // Barlow SemiBold
	UIBold               // Barlow Bold
	Cond                 // Barlow Condensed Medium
	CondBold             // Barlow Condensed Bold
	Display              // Barlow Condensed ExtraBold (score, big numbers)
	Mono                 // Go Mono for tables
)

var (
	fontOnce    sync.Once
	fontSources [6]*text.GoTextFaceSource
	faceCache   = map[[2]int]*text.GoTextFace{}
)

func loadFonts() {
	for i, b := range [][]byte{barlowSemiBold, barlowBold, condMedium, condBold, condExtraBold, gomono.TTF} {
		s, err := text.NewGoTextFaceSource(bytes.NewReader(b))
		if err != nil {
			panic(err)
		}
		fontSources[i] = s
	}
}

// Face returns a cached face.
func Face(f Font, size float64) *text.GoTextFace {
	fontOnce.Do(loadFonts)
	k := [2]int{int(f), int(size * 4)}
	if fc, ok := faceCache[k]; ok {
		return fc
	}
	fc := &text.GoTextFace{Source: fontSources[f], Size: size}
	faceCache[k] = fc
	return fc
}

// Align is horizontal text alignment.
type Align uint8

const (
	Left Align = iota
	Center
	Right
)

// Text draws s with its top-left (or top-center/top-right) at x, y.
func Text(dst *ebiten.Image, s string, f Font, size, x, y float64, c color.Color, al Align) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(x, y)
	op.ColorScale.ScaleWithColor(c)
	switch al {
	case Center:
		op.PrimaryAlign = text.AlignCenter
	case Right:
		op.PrimaryAlign = text.AlignEnd
	}
	text.Draw(dst, s, Face(f, size), op)
}

// TextShadow draws text with a soft drop shadow for legibility over art.
func TextShadow(dst *ebiten.Image, s string, f Font, size, x, y float64, c color.Color, al Align) {
	Text(dst, s, f, size, x+2, y+3, color.RGBA{0, 0, 0, 150}, al)
	Text(dst, s, f, size, x, y, c, al)
}

// Measure returns the advance width and line height of s.
func Measure(s string, f Font, size float64) (float64, float64) {
	return text.Measure(s, Face(f, size), 0)
}
