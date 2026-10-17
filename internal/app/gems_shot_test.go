package app

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/render"
)

// TestGemSheetShot draws every gem style in every material (look at it
// with JAM_SHOTS set).
func TestGemSheetShot(t *testing.T) {
	h := newHarness(t)
	h.screen.Fill(render.RGBA(0x0C, 0x0F, 0x1A, 0xFF))
	for m := render.MatClassic; m <= render.MatFlat; m++ {
		l := render.DefaultLook()
		l.Material = m
		render.SetLook(l)
		for st := render.GemStrum; st <= render.GemTom; st++ {
			for li, c := range []int{0, 3} {
				img := render.GemSprite(st, render.DefaultTheme.Lanes[c])
				op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
				op.GeoM.Translate(float64(int(st)*2+li)*190+20, float64(m)*150+20)
				h.screen.DrawImage(img, op)
			}
		}
	}
	h.shot("gems")
}
