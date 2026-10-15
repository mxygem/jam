package library

import (
	"math"
	"math/bits"
	"sort"

	"github.com/mxygem/jam/internal/chart"
)

// Difficulty estimates ported from Fretwork by Staycation
// (https://github.com/Staycation44/fretwork, MIT licence): note density and
// fret/pad movement in sliding one-second windows, combined into a scalar D,
// then mapped to a 0-6 "remap" calibrated against official releases and an
// open-ended log tier. Only the Expert chart of each part is rated.

const (
	fwWindowMs = 1000.0
	fwStepMs   = fwWindowMs / 4
	fwTRef     = 230.0 // seconds; a typical song length
	fwSStam    = 0.20
	fwRollCap  = 4.0 // drum roll hits per second
)

// Fretwork is one part's rating.
type Fretwork struct {
	D     float64 `json:"d"`
	Remap int     `json:"remap"` // 0-6
	Tier  int     `json:"tier"`  // 0 and up, log scaled
}

var (
	fwGuitarBins = []float64{0, 7.2, 8.6, 10.3, 12.0, 14.0, 17.1, math.Inf(1)}
	fwBassBins   = []float64{0, 5.1, 7.2, 8.6, 10.2, 11.9, 14.1, math.Inf(1)}
	fwKeysBins   = []float64{0, 3.5, 6.0, 7.8, 9.7, 11.8, 13.6, math.Inf(1)}
	fwDrumBins   = []float64{0, 9.8, 12.1, 13.6, 15.6, 18.5, 21.8, math.Inf(1)}
)

const (
	fwFretBaseD = 7.101
	fwFretLnInc = 0.1792
	fwDrumBaseD = 10.115
	fwDrumLnInc = 0.1516
)

func fwRemap(d float64, bins []float64) int {
	if d <= 0 {
		return 0
	}
	for i := 1; i < len(bins); i++ {
		if d <= bins[i] {
			return i - 1
		}
	}
	return 6
}

func fwTier(d, base, inc float64) int {
	if d < base {
		return 0
	}
	return int(math.Floor(math.Log(d/base)/inc)) + 1
}

// series is the per-window rate of one stream plus its summary stats.
type series struct {
	avg, peak, median, std float64
}

// windowSums sums values over [g, g+window) for every grid start g.
func windowSums(times []float64, values []float64, n int) []float64 {
	prefix := make([]float64, len(values)+1)
	for i, v := range values {
		prefix[i+1] = prefix[i] + v
	}
	out := make([]float64, n)
	for k := 0; k < n; k++ {
		g := float64(k) * fwStepMs
		l := sort.SearchFloat64s(times, g)
		r := sort.SearchFloat64s(times, g+fwWindowMs)
		out[k] = prefix[r] - prefix[l]
	}
	return out
}

// activeStats returns median and (population) std dev over windows where
// active is true, as Fretwork gates both by note activity.
func activeStats(vals []float64, active []bool) (median, std float64) {
	var a []float64
	for i, v := range vals {
		if active[i] {
			a = append(a, v)
		}
	}
	if len(a) == 0 {
		return 0, 0
	}
	sort.Float64s(a)
	if n := len(a); n%2 == 1 {
		median = a[n/2]
	} else {
		median = (a[n/2-1] + a[n/2]) / 2
	}
	mean := 0.0
	for _, v := range a {
		mean += v
	}
	mean /= float64(len(a))
	for _, v := range a {
		std += (v - mean) * (v - mean)
	}
	return median, math.Sqrt(std / float64(len(a)))
}

func peak(vals []float64) float64 {
	p := 0.0
	for _, v := range vals {
		p = math.Max(p, v)
	}
	return p
}

// combo is Fretwork's ((median + eps) * avg * peak)^(1/3) with eps = 5% of avg.
func combo(s series) float64 {
	return math.Cbrt((s.median + s.avg*0.05) * s.avg * s.peak)
}

func cv(s series) float64 {
	if s.median+s.avg == 0 {
		return 0
	}
	return s.std / (s.median + s.avg)
}

func stamina(durS float64) float64 {
	if durS <= 0 {
		return 0
	}
	return math.Pow(durS/fwTRef, fwSStam)
}

// rateFret rates a five-fret part (guitar, bass, rhythm, coop, keys).
// It also returns the average and peak notes per second.
func rateFret(t *chart.Track, inst chart.Instrument) (Fretwork, float64, float64) {
	notes := t.Notes
	if len(notes) == 0 {
		return Fretwork{}, 0, 0
	}
	times := make([]float64, 0, len(notes))
	ones := make([]float64, 0, len(notes))
	changes := make([]float64, 0, len(notes))
	var prev uint8
	for _, n := range notes {
		if n.Time < 0 {
			continue
		}
		m := n.Lanes // frets plus the open bit: any simultaneous frets are one note
		times = append(times, n.Time*1000)
		ones = append(ones, 1)
		if len(times) == 1 {
			changes = append(changes, float64(bits.OnesCount8(m)))
		} else {
			removed := bits.OnesCount8(prev &^ m)
			added := bits.OnesCount8(m &^ prev)
			changes = append(changes, float64(max(removed, added)))
		}
		prev = m
	}
	if len(times) == 0 {
		return Fretwork{}, 0, 0
	}
	last := times[len(times)-1]
	n := int(last/fwStepMs) + 1
	durS := last / 1000
	nps := windowSums(times, ones, n)
	vps := windowSums(times, changes, n)
	active := make([]bool, n)
	for i := range nps {
		active[i] = nps[i] > 0
	}
	total := 0.0
	for _, c := range changes {
		total += c
	}
	N := series{peak: peak(nps)}
	V := series{peak: peak(vps)}
	if durS > 0 {
		N.avg = float64(len(times)) / durS
		V.avg = total / durS
	}
	N.median, N.std = activeStats(nps, active)
	V.median, V.std = activeStats(vps, active)

	cov := 1 + math.Sqrt(cv(N)*cv(V))
	d := (combo(N) + combo(V)) * cov * stamina(math.Floor(durS)) // Fretwork truncates to whole seconds
	if math.IsNaN(d) {
		d = 0
	}
	bins := fwGuitarBins
	switch inst {
	case chart.Bass:
		bins = fwBassBins
	case chart.Keys:
		bins = fwKeysBins
	}
	return Fretwork{D: d, Remap: fwRemap(d, bins), Tier: fwTier(d, fwFretBaseD, fwFretLnInc)}, N.avg, N.peak
}

// rateDrums rates a drum part on its single-pedal (1x) reading. Notes per
// second count every gem, kicks included.
func rateDrums(t *chart.Track) (Fretwork, float64, float64) {
	const kick = 1 << chart.DrumKick
	var hTimes, hits, travel, kTimes, allTimes, gems []float64
	var rollHit []bool
	var prev uint8
	first := true
	inRoll := func(tm float64) bool {
		for _, r := range t.Rolls {
			if tm >= r.Start && tm < r.End {
				return true
			}
		}
		return false
	}
	for _, n := range t.Notes {
		if n.Time < 0 {
			continue
		}
		ms := n.Time * 1000
		allTimes = append(allTimes, ms)
		gems = append(gems, float64(bits.OnesCount8(n.Lanes)))
		if n.Lanes&kick != 0 && !n.DoubleKick {
			kTimes = append(kTimes, ms)
		}
		hand := n.Lanes &^ kick
		if hand == 0 {
			continue
		}
		hTimes = append(hTimes, ms)
		h := float64(bits.OnesCount8(hand))
		tr := float64(bits.OnesCount8(hand &^ prev))
		if first {
			tr, first = h, false
		}
		roll := inRoll(n.Time)
		if roll {
			tr = 0
		}
		hits = append(hits, h)
		travel = append(travel, tr)
		rollHit = append(rollHit, roll)
		prev = hand
	}
	if len(hTimes) == 0 {
		return Fretwork{}, 0, 0
	}
	lastMs := hTimes[len(hTimes)-1]
	if len(kTimes) > 0 {
		lastMs = math.Max(lastMs, kTimes[len(kTimes)-1])
	}
	durS := lastMs / 1000

	n := int(hTimes[len(hTimes)-1]/fwStepMs) + 1
	normal := make([]float64, len(hits))
	rolled := make([]float64, len(hits))
	rollTotal := 0.0
	for i, h := range hits {
		if rollHit[i] {
			rolled[i] = h
			rollTotal += h
		} else {
			normal[i] = h
		}
	}
	hps := windowSums(hTimes, normal, n)
	hr := windowSums(hTimes, rolled, n)
	for i := range hps {
		hps[i] += math.Min(hr[i], fwRollCap)
	}
	tps := windowSums(hTimes, travel, n)
	active := make([]bool, n)
	for i := range hps {
		active[i] = hps[i] > 0
	}
	rollSpan := 0.0
	for _, r := range t.Rolls {
		rollSpan += r.End - r.Start
	}
	totalHits := 0.0
	for _, v := range normal {
		totalHits += v
	}
	totalHits += math.Min(rollTotal, fwRollCap*rollSpan)
	totalTravel := 0.0
	for _, v := range travel {
		totalTravel += v
	}
	H := series{peak: peak(hps), avg: totalHits / durS}
	T := series{peak: peak(tps), avg: totalTravel / durS}
	H.median, H.std = activeStats(hps, active)
	T.median, T.std = activeStats(tps, active)

	K := series{}
	cvK := 0.0
	if len(kTimes) > 0 {
		kn := int(kTimes[len(kTimes)-1]/fwStepMs) + 1
		ones := make([]float64, len(kTimes))
		for i := range ones {
			ones[i] = 1
		}
		kps := windowSums(kTimes, ones, kn)
		kact := make([]bool, kn)
		for i := range kps {
			kact[i] = kps[i] > 0
		}
		K = series{peak: peak(kps), avg: float64(len(kTimes)) / durS}
		K.median, K.std = activeStats(kps, kact)
		cvK = cv(K)
	}
	kc := 0.0
	if len(kTimes) > 0 {
		kc = combo(K)
	}
	cov := 1 + 2*math.Sqrt(cv(H)*cvK)
	d := (combo(H) + combo(T) + kc) * cov * stamina(durS)
	if math.IsNaN(d) {
		d = 0
	}
	// notes per second over all gems
	ln := int(allTimes[len(allTimes)-1]/fwStepMs) + 1
	g := windowSums(allTimes, gems, ln)
	totalGems := 0.0
	for _, v := range gems {
		totalGems += v
	}
	avgNPS := 0.0
	if s := allTimes[len(allTimes)-1] / 1000; s > 0 {
		avgNPS = totalGems / s
	}
	return Fretwork{D: d, Remap: fwRemap(d, fwDrumBins), Tier: fwTier(d, fwDrumBaseD, fwDrumLnInc)}, avgNPS, peak(g)
}

// RemapFor maps a Fretwork D to the 0-6 scale calibrated for a part.
func RemapFor(d float64, inst chart.Instrument) int {
	switch inst {
	case chart.Drums:
		return fwRemap(d, fwDrumBins)
	case chart.Bass:
		return fwRemap(d, fwBassBins)
	case chart.Keys:
		return fwRemap(d, fwKeysBins)
	}
	return fwRemap(d, fwGuitarBins)
}

// TierFor maps a Fretwork D to the open-ended log tier for a part.
func TierFor(d float64, inst chart.Instrument) int {
	if inst == chart.Drums {
		return fwTier(d, fwDrumBaseD, fwDrumLnInc)
	}
	return fwTier(d, fwFretBaseD, fwFretLnInc)
}
