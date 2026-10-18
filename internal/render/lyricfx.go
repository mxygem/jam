package render

import (
	"image"
	"image/color"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/mxygem/jam/internal/chart"
	"github.com/mxygem/jam/internal/rich"
)

// Lyric effects. A phrase without effects is drawn as before: one pre-rendered
// line image with the sung part clipped over it. A phrase with effects is drawn
// letter by letter, each letter a slice of that same line image (so fonts,
// kerning and color tags stay exactly as rendered), moved, scaled, tinted and
// faded on its own.

// LyricFxModes lists the values LyricOpts.Effects takes, in menu order.
var LyricFxModes = []string{"full", "calm", "off"}

// calmDrops are the effects Calm leaves out: the ones that throw letters
// around or never sit still. Color, glow, whisper, pulse, bounce, zoom, hold
// and typewriter stay.
var calmDrops = map[string]bool{"shake": true, "glitch": true, "explode": true, "wave": true, "flyin": true, "takeover": true}

// phraseFX is a phrase's effects, filtered by the player's setting.
type phraseFX struct {
	syl        [][]chart.FX // per syllable
	any        bool
	takeover   bool
	pulse      bool
	whisperAll bool
	tail       float64 // seconds the phrase stays after its end (explosions need longer)
}

// hasAny reports whether any syllable of the phrase has the effect.
func (pf *phraseFX) hasAny(name string) bool {
	for _, fx := range pf.syl {
		if chart.HasFX(fx, name) {
			return true
		}
	}
	return false
}

func buildPhraseFX(p *chart.LyricPhrase, mode string) phraseFX {
	pf := phraseFX{syl: make([][]chart.FX, len(p.Syllables)), tail: 0.3}
	if mode == "off" {
		return pf
	}
	whisper := 0
	for j, s := range p.Syllables {
		for _, f := range s.FX {
			if mode == "calm" && calmDrops[f.Name] {
				continue
			}
			pf.syl[j] = append(pf.syl[j], f)
			pf.any = true
			switch f.Name {
			case "takeover":
				pf.takeover = true
			case "pulse":
				pf.pulse = true
			case "whisper":
				whisper++
			case "explode":
				pf.tail = 1.4
			}
		}
	}
	pf.whisperAll = whisper > 0 && whisper == len(p.Syllables)
	return pf
}

// letterBox is one visible letter of a rendered line, in line pixels.
type letterBox struct {
	x0, x1 float64
	syl    int
	k      int // index of the letter within its syllable
	n      int // letters in its syllable
}

// lineTextFX is lineText with whispered syllables set in italics.
func lineTextFX(p *chart.LyricPhrase, pf *phraseFX) (string, []int, []int) {
	var b strings.Builder
	starts := make([]int, len(p.Syllables))
	ends := make([]int, len(p.Syllables))
	for i, s := range p.Syllables {
		starts[i] = b.Len()
		italic := pf != nil && chart.HasFX(pf.syl[i], "whisper") && !strings.Contains(s.Text, "<i>")
		if italic {
			b.WriteString("<i>")
		}
		b.WriteString(s.Text)
		if italic {
			b.WriteString("</i>")
		}
		ends[i] = b.Len()
		if !s.Join && i < len(p.Syllables)-1 {
			b.WriteByte(' ')
		}
	}
	return b.String(), starts, ends
}

// measureLetters finds each visible letter's span in the rendered line.
func measureLetters(s string, starts, ends []int, size float64) []letterBox {
	var out []letterBox
	pieces, isTag := rich.Split(s)
	off := 0
	for i, pc := range pieces {
		if isTag[i] {
			off += len(pc)
			continue
		}
		for j := 0; j < len(pc); {
			r, w := utf8.DecodeRuneInString(pc[j:])
			at := off + j
			j += w
			if r == ' ' || r == ' ' {
				continue
			}
			syl := -1
			for k := range starts {
				if at >= starts[k] && at < ends[k] {
					syl = k
					break
				}
			}
			if syl < 0 {
				continue
			}
			x0, _ := MeasureRich(s[:at], UIBold, size)
			x1, _ := MeasureRich(s[:at+w], UIBold, size)
			out = append(out, letterBox{x0: x0, x1: math.Max(x1, x0+1), syl: syl})
		}
		off += len(pc)
	}
	// number the letters inside each syllable
	count := map[int]int{}
	for i := range out {
		out[i].k = count[out[i].syl]
		count[out[i].syl]++
	}
	for i := range out {
		out[i].n = count[out[i].syl]
	}
	return out
}

func ease(t float64) float64 { t = clamp01(t); return 1 - math.Pow(1-t, 3) }

// noise01 is a steady pseudo-random number in [0,1) for a seed.
func noise01(seed float64) float64 {
	x := math.Sin(seed*12.9898+78.233) * 43758.5453
	return x - math.Floor(x)
}

func sylDur(p *chart.LyricPhrase, j int) float64 {
	d := 0.6
	if j+1 < len(p.Syllables) {
		d = p.Syllables[j+1].Time - p.Syllables[j].Time
	} else if p.End > p.Syllables[j].Time {
		d = p.End - p.Syllables[j].Time
	}
	return math.Max(0.15, math.Min(3, d))
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	l := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.RGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 0xFF}
}

// drawFX draws phrase i of the track with its effects. hx is how far the
// sweep has reached, in line pixels.
func (t *lyricTrack) drawFX(dst *ebiten.Image, i int, cur *lyricLine, now, cx, y, fade, hx float64, unsung color.RGBA, beat func(float64) float64, next *lyricLine) {
	p := &t.Phrases[i]
	pf := &t.fx[i]
	size := cur.size
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}

	// Whole-line transform: whisper shrinks, pulse breathes, takeover fills the screen.
	lineScale, dy, bandA, nextA := 1.0, 0.0, 0.55*fade, 0.4*fade
	if pf.whisperAll {
		lineScale *= 0.82
	}
	if pf.pulse && now >= p.Start {
		ph := math.Mod((now-p.Start)/0.5, 1)
		if beat != nil {
			ph = beat(now)
		}
		lineScale *= 1 + 0.09*math.Exp(-ph*6)
	}
	lcx, lcy := cx, y+cur.h/2
	if pf.takeover {
		k := ease((now-(p.Start-0.2))/0.35) * (1 - ease((now-(p.End+0.2))/0.5))
		lineScale *= 1 + 1.6*k
		dy = (ScreenH*0.42 - lcy) * k
		bandA *= 1 - k
		nextA *= 1 - k
	}

	// Hold: a held syllable's letters spread apart over its note.
	shift := make([]float64, len(cur.letters))
	total := 0.0
	for k, L := range cur.letters {
		shift[k] = total
		if L.k < L.n-1 && chart.HasFX(pf.syl[L.syl], "hold") {
			s := p.Syllables[L.syl]
			total += size * 0.22 * ease((now-s.Time)/sylDur(p, L.syl))
		}
	}
	bw := cur.w*lineScale + total
	if next != nil {
		bw = math.Max(bw, next.w)
	}
	nh := 0.0
	if next != nil {
		nh = next.h + 6
	}
	drawBand(dst, cx, y, bw, cur.h, nh, bandA)
	left := cx - (cur.w+total)/2

	ih := float64(cur.img.Bounds().Dy())
	iw := float64(cur.img.Bounds().Dx())
	frame := math.Floor(now * 30)
	lastSung := -1.0
	for k, L := range cur.letters {
		fx := pf.syl[L.syl]
		s := p.Syllables[L.syl]
		dur := sylDur(p, L.syl)
		// The slice of the line image for this letter; the ends reach the image edges for overhangs.
		ix0 := lyricPad + L.x0
		ix1 := lyricPad + L.x1
		if k == 0 {
			ix0 = 0
		}
		if k == len(cur.letters)-1 {
			ix1 = iw
		}
		lw := ix1 - ix0
		sx := left + shift[k] - lyricPad + ix0 // where the slice's left edge sits without effects
		sy := y - lyricPad
		dx, ddy, scale, rot, alpha := 0.0, 0.0, 1.0, 0.0, fade
		sungCol, unsungCol := t.color, unsung
		hidden := false
		var sparks []func()

		sungFrac := clamp01((hx - L.x0) / (L.x1 - L.x0))
		if hx >= L.x1 || (chart.HasFX(fx, "zoom") && now >= s.Time) {
			sungFrac = 1 // a zoomed syllable lights all at once on its hit
		}
		for _, f := range fx {
			arg := f.Arg
			switch f.Name {
			case "color":
				sungCol = f.Color
				unsungCol = mix(f.Color, white, 0.55)
			case "rainbow":
				if sungFrac > 0 {
					sungCol = FromHSV(float64(k)*28+now*160, 0.75, 1)
				}
			case "whisper":
				alpha *= 0.75
			case "bounce":
				if q := (now - s.Time) / 0.28; q > 0 && q < 1 {
					ddy -= math.Sin(math.Pi*q) * size * 0.32
				}
			case "wave":
				if arg == 0 {
					arg = 1
				}
				ddy += math.Sin(now*3.2+float64(k)*0.55) * size * 0.14 * arg
			case "shake":
				if arg == 0 {
					arg = 1
				}
				if now >= s.Time && now <= s.Time+dur+0.15 {
					amp := 3.2 * arg * size / 46
					dx += (noise01(float64(k)*7+frame) - 0.5) * 2 * amp
					ddy += (noise01(float64(k)*13+frame+0.5) - 0.5) * 2 * amp
				}
			case "zoom":
				if arg == 0 {
					arg = 2.4
				}
				if q := (now - s.Time) / 0.45; q >= 0 && q < 1 {
					// The syllable grows as a word around its middle, so its letters don't overlap.
					z := 1 + (arg-1)*(1-ease(q))
					scale *= z
					dx += ((L.x0+L.x1)/2 - (cur.start[L.syl]+cur.end[L.syl])/2) * (z - 1)
				}
			case "type":
				if sungFrac <= 0 {
					hidden = true
				}
			case "flyin":
				land := p.Start - 0.05 + float64(k)*0.035
				q := ease((now - (land - 0.45)) / 0.45)
				ddy -= (1 - q) * size * 1.4
				alpha *= q
				rot += (1 - q) * 0.5
			case "explode":
				t0 := s.Time + dur + 0.05
				if q := (now - t0) / 0.9; q > 0 {
					ang := noise01(float64(k)*3.1+s.Time) * 2 * math.Pi
					dist := (30 + noise01(float64(k)*5.7+s.Time)*90) * size / 46
					e := ease(q)
					dx += math.Cos(ang) * dist * e
					ddy += math.Sin(ang)*dist*e - 20*q
					rot += (noise01(float64(k)+s.Time) - 0.5) * 4 * q
					fadeOut := 1 - clamp01(q)
					alpha *= fadeOut
					cxs, cys := sx+lw/2, sy+ih/2
					for n := 0; n < 5; n++ {
						a2 := ang + float64(n-2)*0.35
						d2 := dist * (1.2 + float64(n)*0.15) * e
						px, py := cxs+math.Cos(a2)*d2, cys+math.Sin(a2)*d2
						c := sungCol
						if n%2 == 1 {
							c = color.RGBA{0xFF, 0xD1, 0x66, 0xFF}
						}
						sa := fadeOut * 0.9 * fade
						sparks = append(sparks, func() {
							x, yy := lineXY(px, py, lcx, lcy, lineScale, dy)
							RoundRect(dst, x-2, yy-2, 4, 4, 1, color.RGBA{c.R, c.G, c.B, uint8(255 * sa)})
						})
					}
				}
			}
		}
		if hidden || alpha <= 0.002 {
			continue
		}
		geo := func() ebiten.GeoM {
			var g ebiten.GeoM
			g.Translate(-lw/2, -ih/2)
			g.Scale(scale, scale)
			g.Rotate(rot)
			g.Translate(sx+lw/2+dx, sy+ih/2+ddy)
			g.Translate(-lcx, -lcy)
			g.Scale(lineScale, lineScale)
			g.Translate(lcx, lcy+dy)
			return g
		}
		slice := func(w float64) *ebiten.Image {
			b := cur.img.Bounds()
			return cur.img.SubImage(image.Rect(b.Min.X+int(ix0), b.Min.Y, b.Min.X+int(math.Ceil(ix0+w)), b.Max.Y)).(*ebiten.Image)
		}
		draw := func(img *ebiten.Image, c color.RGBA, a float64, offX float64, blend ebiten.Blend) {
			op := &ebiten.DrawImageOptions{GeoM: geo()}
			if offX != 0 {
				op.GeoM.Translate(offX*lineScale, 0)
			}
			op.ColorScale.Scale(float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, 1)
			op.ColorScale.ScaleAlpha(float32(clamp01(a)))
			op.Blend = blend
			dst.DrawImage(img, op)
		}
		full := slice(lw)
		if !chart.HasFX(fx, "type") {
			draw(full, unsungCol, alpha*0.5, 0, ebiten.BlendSourceOver)
		}
		if sungFrac > 0 {
			part := full
			if sungFrac < 1 {
				part = slice(lyricPad + L.x0 + (L.x1-L.x0)*sungFrac - ix0)
			}
			if g, ok := chart.FindFX(fx, "glow"); ok {
				r := size * 0.06
				if g.Arg > 0 {
					r *= g.Arg
				}
				for n := 0; n < 8; n++ {
					a := float64(n) * math.Pi / 4
					op := &ebiten.DrawImageOptions{GeoM: geo()}
					op.GeoM.Translate(math.Cos(a)*r*lineScale, math.Sin(a)*r*lineScale)
					op.ColorScale.Scale(float32(sungCol.R)/255, float32(sungCol.G)/255, float32(sungCol.B)/255, 1)
					op.ColorScale.ScaleAlpha(float32(0.22 * alpha * sungFrac))
					op.Blend = ebiten.BlendLighter
					dst.DrawImage(part, op)
				}
			}
			if chart.HasFX(fx, "glitch") && now >= s.Time && now <= s.Time+dur+0.1 {
				j := math.Floor(now * 24)
				off := (noise01(j+float64(k)) - 0.5) * 8
				draw(part, color.RGBA{0xFF, 0x2E, 0x63, 0xFF}, alpha*0.8, -3+off, ebiten.BlendLighter)
				draw(part, color.RGBA{0x08, 0xD9, 0xD6, 0xFF}, alpha*0.8, 3-off, ebiten.BlendLighter)
				if noise01(j*3+float64(k)) > 0.7 {
					dx += (noise01(j+float64(k)*2) - 0.5) * 14
				}
			}
			draw(part, sungCol, alpha, 0, ebiten.BlendSourceOver)
			lastSung = float64(k)
		}
		for _, sp := range sparks {
			sp()
		}
	}
	// Typewriter caret after the last letter that's been sung.
	if typing := len(cur.letters) > 0 && chart.HasFX(pf.syl[cur.letters[0].syl], "type"); typing && now >= p.Start && now <= p.End+0.6 {
		if math.Mod(now*3, 2) < 1 {
			cxr := left - lyricPad
			if lastSung >= 0 {
				L := cur.letters[int(lastSung)]
				cxr = left + shift[int(lastSung)] + L.x0 + (L.x1-L.x0)*clamp01((hx-L.x0)/(L.x1-L.x0)) + 3
			}
			x, yy := lineXY(cxr, y+cur.h*0.12, lcx, lcy, lineScale, dy)
			RoundRect(dst, x, yy, 3*lineScale, cur.h*0.8*lineScale, 1, t.color)
		}
	}
	if next != nil && nextA > 0 {
		drawLine(dst, next, cx-next.w/2, y+cur.h+6, white, nextA, -1)
	}
}

// lineXY maps a point through the whole-line transform.
func lineXY(x, y, lcx, lcy, scale, dy float64) (float64, float64) {
	return lcx + (x-lcx)*scale, lcy + (y-lcy)*scale + dy
}
