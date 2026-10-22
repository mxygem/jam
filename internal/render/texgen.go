package render

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"
)

// Built-in highway textures, drawn in code. Every one tiles both ways:
// noise lattices repeat a whole number of times across the image and
// shapes wrap around the edges. u runs across the highway, v along it.

const texW, texH = 512, 1024

func texHash(x, y int, seed uint32) float64 {
	h := uint32(x)*374761393 + uint32(y)*668265263 + seed*2246822519
	h = (h ^ (h >> 13)) * 1274126177
	h ^= h >> 16
	return float64(h) / 4294967295
}

func wrapi(i, n int) int { return ((i % n) + n) % n }

func quintic(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }

// vnoise is value noise at lattice point (x, y), repeating every px by py.
func vnoise(x, y float64, px, py int, seed uint32) float64 {
	x0, y0 := math.Floor(x), math.Floor(y)
	fx, fy := quintic(x-x0), quintic(y-y0)
	ix, iy := int(x0), int(y0)
	a := texHash(wrapi(ix, px), wrapi(iy, py), seed)
	b := texHash(wrapi(ix+1, px), wrapi(iy, py), seed)
	c := texHash(wrapi(ix, px), wrapi(iy+1, py), seed)
	d := texHash(wrapi(ix+1, px), wrapi(iy+1, py), seed)
	return (a*(1-fx)+b*fx)*(1-fy) + (c*(1-fx)+d*fx)*fy
}

// fbm sums octaves of noise at (u, v) in 0..1, starting with px by py
// cells; 0..1 out.
func fbm(u, v float64, px, py, octaves int, seed uint32) float64 {
	sum, amp, norm := 0.0, 1.0, 0.0
	for o := 0; o < octaves; o++ {
		sum += amp * vnoise(u*float64(px), v*float64(py), px, py, seed+uint32(o)*101)
		norm += amp
		amp *= 0.5
		px, py = px*2, py*2
	}
	return sum / norm
}

func smoothstep(e0, e1, x float64) float64 {
	t := clamp01((x - e0) / (e1 - e0))
	return t * t * (3 - 2*t)
}

func lerpRGB(a, b color.RGBA, t float64) [3]float64 {
	return [3]float64{
		float64(a.R) + (float64(b.R)-float64(a.R))*t,
		float64(a.G) + (float64(b.G)-float64(a.G))*t,
		float64(a.B) + (float64(b.B)-float64(a.B))*t,
	}
}

// fillTex runs f for every pixel, rows split across the CPUs; f returns
// straight RGB 0..255 and alpha 0..1.
func fillTex(f func(x, y int, u, v float64) ([3]float64, float64)) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, texW, texH))
	workers := runtime.NumCPU()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for y := w; y < texH; y += workers {
				v := float64(y) / texH
				for x := 0; x < texW; x++ {
					c, a := f(x, y, float64(x)/texW, v)
					a = clamp01(a)
					o := img.PixOffset(x, y)
					for k := 0; k < 3; k++ {
						img.Pix[o+k] = uint8(math.Max(0, math.Min(255, c[k])) * a)
					}
					img.Pix[o+3] = uint8(255 * a)
				}
			}
		}(w)
	}
	wg.Wait()
	return img
}

func gray(g float64) [3]float64 { g = 255 * clamp01(g); return [3]float64{g, g, g} }

type woodPalette struct {
	dark, mid, light color.RGBA
	bands            float64 // weight of the wide growth bands
	pores            float64 // how dark the pores are
	seed             uint32
}

var (
	woodRose  = woodPalette{color.RGBA{0x24, 0x10, 0x09, 0xFF}, color.RGBA{0x4E, 0x26, 0x16, 0xFF}, color.RGBA{0x74, 0x3E, 0x25, 0xFF}, 0.35, 0.55, 1}
	woodEbony = woodPalette{color.RGBA{0x0A, 0x08, 0x08, 0xFF}, color.RGBA{0x18, 0x14, 0x12, 0xFF}, color.RGBA{0x2E, 0x26, 0x21, 0xFF}, 0.25, 0.5, 2}
	woodMaple = woodPalette{color.RGBA{0xA8, 0x74, 0x3E, 0xFF}, color.RGBA{0xCF, 0xA0, 0x62, 0xFF}, color.RGBA{0xE6, 0xC3, 0x8C, 0xFF}, 0.3, 0.3, 3}
)

// genWood draws a fingerboard: grain running along the highway, wavy
// growth bands, fine fibers and pores.
func genWood(p woodPalette) *image.RGBA {
	s := p.seed * 1000
	return fillTex(func(x, y int, u, v float64) ([3]float64, float64) {
		warp := fbm(u, v, 3, 2, 3, s+11)
		g := u*13 + (warp-0.5)*2.4
		band := 0.5 + 0.5*math.Sin(2*math.Pi*g)
		band = math.Pow(band, 3)
		fiber := fbm(u, v, 96, 3, 3, s+21)
		fine := vnoise(u*256, v*6, 256, 6, s+31)
		t := 0.5*fiber + 0.2*fine + p.bands*band
		t = clamp01((t - 0.25) / 0.75)
		var c [3]float64
		if t < 0.5 {
			c = lerpRGB(p.dark, p.mid, t*2)
		} else {
			c = lerpRGB(p.mid, p.light, (t-0.5)*2)
		}
		// pores: short dark dashes along the grain
		if n := vnoise(u*170, v*22, 170, 22, s+41); n > 0.84 {
			k := 1 - p.pores*smoothstep(0.84, 0.95, n)
			c = [3]float64{c[0] * k, c[1] * k, c[2] * k}
		}
		return c, 1
	})
}

// genBrushed: fine streaks along the highway.
func genBrushed() *image.RGBA {
	return fillTex(func(x, y int, u, v float64) ([3]float64, float64) {
		g := 0.62 +
			0.34*(fbm(u, v, 128, 2, 2, 51)-0.5) +
			0.22*(vnoise(u*512, v*8, 512, 8, 52)-0.5) +
			0.16*(fbm(u, v, 2, 2, 2, 53)-0.5)
		return gray(g), 1
	})
}

// genCarbon: a 2/2 twill weave of carbon tows; each tow runs over two
// cells, then dips under its neighbor.
func genCarbon() *image.RGBA {
	const cell = 16
	return fillTex(func(x, y int, u, v float64) ([3]float64, float64) {
		i, j := x/cell, y/cell
		lx, ly := float64(x%cell)/cell, float64(y%cell)/cell
		m := wrapi(i-j, 4)
		var across, along, peak float64
		if m < 2 { // runs across the highway, over cells m = 0, 1
			across, along, peak = ly, (lx+float64(m))/2, 0.92
		} else { // runs along it, over cells m = 3, 2 going down
			across, along, peak = lx, (ly+float64(3-m))/2, 0.66
		}
		g := 0.18 + (peak-0.18)*math.Pow(math.Sin(math.Pi*across), 0.6)
		// sheen along the tow, darker where it dips under
		g *= 0.6 + 0.4*math.Pow(math.Sin(math.Pi*along), 0.5)
		fx, fy := float64(x), float64(y)
		if m < 2 {
			g += 0.05 * (vnoise(fx/32, fy, texW/32, texH, 71) - 0.5)
		} else {
			g += 0.05 * (vnoise(fx, fy/32, texW, texH/32, 72) - 0.5)
		}
		return gray(g), 1
	})
}

// genDiamond: tread plate, raised lozenges in alternating directions,
// lit from the top left.
func genDiamond() *image.RGBA {
	const cell = 32
	hgt := make([]float64, texW*texH)
	for y := 0; y < texH; y++ {
		for x := 0; x < texW; x++ {
			i, j := x/cell, y/cell
			cx, cy := float64(x%cell)/cell-0.5, float64(y%cell)/cell-0.5
			dir := 1.0
			if (i+j)%2 == 1 {
				dir = -1
			}
			a := (cx + dir*cy) / math.Sqrt2 // along the lozenge
			b := (cx - dir*cy) / math.Sqrt2 // across it
			d := math.Abs(b)/0.11 + (a/0.38)*(a/0.38)
			if d < 1 {
				hgt[y*texW+x] = math.Sqrt(1 - d)
			}
		}
	}
	at := func(x, y int) float64 { return hgt[wrapi(y, texH)*texW+wrapi(x, texW)] }
	return fillTex(func(x, y int, u, v float64) ([3]float64, float64) {
		h := at(x, y)
		slope := at(x+1, y+1) - at(x-1, y-1) // lit from the top left
		g := 0.52 + 0.16*(fbm(u, v, 64, 2, 2, 81)-0.5) + 0.10*(fbm(u, v, 4, 8, 3, 82)-0.5)
		if h > 0 {
			g += 0.12*h + 0.9*slope
		}
		return gray(g), 1
	})
}

// genConcrete: blotchy, with specks and pits.
func genConcrete() *image.RGBA {
	return fillTex(func(x, y int, u, v float64) ([3]float64, float64) {
		g := 0.60 + 0.40*(fbm(u, v, 6, 12, 5, 61)-0.5)
		g += 0.18 * (vnoise(u*256, v*512, 256, 512, 62) - 0.5)
		if r := texHash(x, y, 63); r > 0.992 {
			g += 0.2
		} else if r < 0.006 {
			g -= 0.25
		}
		if p := vnoise(u*96, v*192, 96, 192, 64); p > 0.9 {
			g -= 0.35 * smoothstep(0.9, 0.97, p)
		}
		return gray(g), 1
	})
}

// genStars: a starfield with faint clouds (white on transparent).
func genStars() *image.RGBA {
	acc := make([]float64, texW*texH)
	const n = 700
	for s := 0; s < n; s++ {
		sx := texHash(s, 1, 91) * texW
		sy := texHash(s, 2, 91) * texH
		b := math.Pow(texHash(s, 3, 91), 2.6)
		r := 0.7 + 1.8*b
		for dy := -6; dy <= 6; dy++ {
			for dx := -6; dx <= 6; dx++ {
				px, py := int(math.Floor(sx))+dx, int(math.Floor(sy))+dy
				ddx, ddy := float64(px)+0.5-sx, float64(py)+0.5-sy
				d2 := (ddx*ddx + ddy*ddy) / (r * r)
				if d2 > 9 {
					continue
				}
				acc[wrapi(py, texH)*texW+wrapi(px, texW)] += (0.55 + 0.9*b) * math.Exp(-d2*1.6)
			}
		}
	}
	return fillTex(func(x, y int, u, v float64) ([3]float64, float64) {
		cloud := smoothstep(0.45, 0.85, fbm(u, v, 3, 6, 5, 92)) * 0.2
		return [3]float64{255, 255, 255}, acc[y*texW+x] + cloud
	})
}

// genHex: a honeycomb of thin lines (white on transparent).
func genHex() *image.RGBA {
	const cols, rows = 6, 14
	w := float64(texW) / cols // column spacing = twice the apothem
	rv := float64(texH) / rows
	apo := w / 2
	ys := 1.5 * (apo * 2 / math.Sqrt(3)) / rv // squash rows to a regular hexagon
	hexd := func(dx, dy float64) float64 {
		dx, dy = math.Abs(dx), math.Abs(dy)
		return math.Max(dx, (dx+math.Sqrt(3)*dy)/2)
	}
	return fillTex(func(x, y int, u, v float64) ([3]float64, float64) {
		fx, fy := float64(x)+0.5, float64(y)+0.5
		best := math.Inf(1)
		j0 := int(math.Floor(fy / rv))
		for j := j0 - 1; j <= j0+1; j++ {
			off := 0.0
			if wrapi(j, rows)%2 == 1 {
				off = w / 2
			}
			cy := (float64(j) + 0.5) * rv
			i0 := int(math.Floor((fx - off) / w))
			for i := i0 - 1; i <= i0+1; i++ {
				cx := (float64(i)+0.5)*w + off
				if d := hexd(fx-cx, (fy-cy)*ys); d < best {
					best = d
				}
			}
		}
		e := apo - best // distance to the nearest edge, in pixels
		line := 1 - smoothstep(0.6, 2.2, e)
		glow := 0.12 * math.Exp(-e/8)
		return [3]float64{255, 255, 255}, 0.5*line + glow + 0.02
	})
}
