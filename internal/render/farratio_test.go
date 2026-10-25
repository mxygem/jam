package render

import (
	"math"
	"testing"
)

// A far ratio of 1 (the old top of the angle setting) made the horizon
// infinite and every Y/X NaN, which crashed the texture level pick.
func TestHighwayFarRatioExtremesStayFinite(t *testing.T) {
	for _, r := range []float64{0, MinFarRatio, 0.3, MaxFarRatio, 1, 1.5} {
		h := &Highway{Cfg: DefaultHighway()}
		h.Cfg.FarRatio = r
		for _, z := range []float64{zMin, 0, 0.5, 1} {
			y, x := h.Y(z), h.X(5, 5, z)
			if math.IsNaN(y) || math.IsInf(y, 0) || math.IsNaN(x) || math.IsInf(x, 0) {
				t.Fatalf("far ratio %v, z %v: Y=%v X=%v", r, z, y, x)
			}
		}
	}
}
