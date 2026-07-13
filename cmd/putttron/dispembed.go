package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"

	"github.com/jhoblitt/putttron/internal/geom"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/sim"
)

// Embedding budget: the page is a single self-contained file served over
// GitHub Pages, so the scatter is thinned for display (the hull and contour
// geometry are computed from the full stored sample first) and coordinates
// are quantized to centimeters — far below a dot's own size on screen.
const (
	embedMaxPts  = 600
	minKDEMisses = 20 // below this a density estimate is not worth drawing
	kdeGridN     = 64
	maxRingVerts = 48
)

var hdrMasses = []float64{0.50, 0.80, 0.95}

// dispData is one cell as read back from a dispersion run.
type dispData struct {
	rollout                                       float64
	nTrials, nHoled, nRunaway, nOff, nMiss, nKept int
	ball                                          physics.Vec2
	axis                                          physics.Vec2
	pts                                           []geom.Pt
}

type hdrLevel struct {
	P     int       `json:"p"`
	A     float64   `json:"a"` // m²
	Rings [][]int16 `json:"rings"`
}

// dispEmbed is the per-cell payload injected into the page: centimeter ints
// for geometry, meters for areas. Bx/By is the unit direction from the hole
// back to the ball (the line the putt was struck along); Ax/Ay is the unit
// direction the ball is actually travelling when it reaches the hole. On a
// breaking putt those are not the same, which is the whole reason the map has
// to say which is which.
type dispEmbed struct {
	Ro      float64    `json:"ro"`
	N       int        `json:"n"`
	Holed   int        `json:"holed"`
	Runaway int        `json:"runaway"`
	Miss    int        `json:"miss"`
	Kept    int        `json:"kept"`
	Bx      float64    `json:"bx"`
	By      float64    `json:"by"`
	Ax      float64    `json:"ax"`
	Ay      float64    `json:"ay"`
	Pts     []int16    `json:"pts"`
	Hull    []int16    `json:"hull"`
	HullA   float64    `json:"hullA"`
	HDR     []hdrLevel `json:"hdr"`
}

// readDispersion loads a dispersion run written by cmdDispersion: <base>.cells.csv
// (one row per cell) joined with <base>.points.csv (the kept miss positions).
func readDispersion(base string) (map[groupKey]*dispData, error) {
	cells, err := readCSVRows(base + ".cells.csv")
	if err != nil {
		return nil, err
	}
	out := map[groupKey]*dispData{}
	for i, r := range cells {
		if len(r) < 16 {
			return nil, fmt.Errorf("%s.cells.csv:%d: want 16 columns, got %d", base, i+2, len(r))
		}
		k, err := dispKey(base+".cells.csv", i, r)
		if err != nil {
			return nil, err
		}
		num := func(j int) float64 { v, _ := strconv.ParseFloat(r[j], 64); return v }
		ints := func(j int) int { v, _ := strconv.Atoi(r[j]); return v }
		d := &dispData{
			rollout: num(5),
			nTrials: ints(6), nHoled: ints(7), nRunaway: ints(8),
			nOff: ints(9), nMiss: ints(10), nKept: ints(11),
			ball: physics.Vec2{X: num(12), Y: num(13)},
			axis: physics.Vec2{X: num(14), Y: num(15)},
		}
		// The CSV rounds these to a few decimals; renormalize, since they
		// drive a view rotation that has to be an exact unit basis.
		if n := d.axis.Norm(); n > 1e-9 {
			d.axis = d.axis.Scale(1 / n)
		} else {
			return nil, fmt.Errorf("%s.cells.csv:%d: degenerate travel axis", base, i+2)
		}
		if n := d.ball.Norm(); n > 1e-9 {
			d.ball = d.ball.Scale(1 / n)
		} else {
			return nil, fmt.Errorf("%s.cells.csv:%d: ball is on top of the hole", base, i+2)
		}
		if d.nHoled+d.nRunaway+d.nOff+d.nMiss != d.nTrials {
			return nil, fmt.Errorf("%s.cells.csv:%d: outcome counts (%d+%d+%d+%d) do not sum to %d trials",
				base, i+2, d.nHoled, d.nRunaway, d.nOff, d.nMiss, d.nTrials)
		}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("%s.cells.csv:%d: duplicate cell %+v", base, i+2, k)
		}
		out[k] = d
	}

	pts, err := readCSVRows(base + ".points.csv")
	if err != nil {
		return nil, err
	}
	for i, r := range pts {
		if len(r) < 9 {
			return nil, fmt.Errorf("%s.points.csv:%d: want 9 columns, got %d", base, i+2, len(r))
		}
		k, err := dispKey(base+".points.csv", i, r)
		if err != nil {
			return nil, err
		}
		d, ok := out[k]
		if !ok {
			return nil, fmt.Errorf("%s.points.csv:%d: point for a cell absent from the cells CSV", base, i+2)
		}
		x, errX := strconv.ParseFloat(r[7], 64)
		y, errY := strconv.ParseFloat(r[8], 64)
		if errX != nil || errY != nil {
			return nil, fmt.Errorf("%s.points.csv:%d: bad coordinates %q, %q", base, i+2, r[7], r[8])
		}
		d.pts = append(d.pts, geom.Pt{X: x, Y: y})
	}
	for k, d := range out {
		if len(d.pts) != d.nKept {
			return nil, fmt.Errorf("%s: cell %+v declares %d kept points but the points CSV has %d",
				base, k, d.nKept, len(d.pts))
		}
	}
	return out, nil
}

func readCSVRows(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rec, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rec) < 1 {
		return nil, fmt.Errorf("%s: empty", path)
	}
	return rec[1:], nil
}

// dispKey parses the shared leading key columns of both dispersion CSVs.
func dispKey(path string, row int, r []string) (groupKey, error) {
	stimp, err1 := strconv.ParseFloat(r[0], 64)
	slope, err2 := strconv.ParseFloat(r[2], 64)
	clock, err3 := strconv.Atoi(r[3])
	ft, err4 := strconv.ParseFloat(r[4], 64)
	for _, err := range []error{err1, err2, err3, err4} {
		if err != nil {
			return groupKey{}, fmt.Errorf("%s:%d: %v", path, row+2, err)
		}
	}
	return groupKey{stimp: stimp, slope: slope, clock: clock, lengthFt: ft, skill: r[1]}, nil
}

// validateDispersion rejects dispersion data that does not correspond to the
// sweep being reported: the page must never show a scatter that disagrees
// with the make% printed beside it.
func validateDispersion(disp map[groupKey]*dispData, groups map[groupKey][]rptRow, stimp float64) error {
	for k, d := range disp {
		if k.stimp != stimp {
			return fmt.Errorf("dispersion cell %+v is at stimp %g, but the page renders stimp %g",
				k, k.stimp, stimp)
		}
		g, ok := groups[k]
		if !ok {
			return fmt.Errorf("dispersion cell %+v has no matching sweep group", k)
		}
		sort.Slice(g, func(i, j int) bool { return g[i].rollout < g[j].rollout })
		best, _, _, _ := argminSE(g)
		if math.Abs(d.rollout-g[best].rollout) > 1e-9 {
			return fmt.Errorf("dispersion cell %+v was simulated at rollout %.2f m but the sweep's optimum is %.2f m; re-run putttron dispersion",
				k, d.rollout, g[best].rollout)
		}
		if got := float64(d.nHoled) / float64(d.nTrials); math.Abs(got-g[best].make_) > 1e-5 {
			return fmt.Errorf("dispersion cell %+v holed %.5f of its trials but the sweep says %.5f; the two runs disagree",
				k, got, g[best].make_)
		}
	}
	return nil
}

// buildDispEmbeds turns each cell's stored misses into the drawable payload:
// the convex hull and its area, highest-density-region contours at 50/80/95%,
// and a thinned scatter that still shows every hull vertex.
func buildDispEmbeds(disp map[groupKey]*dispData) map[string]dispEmbed {
	keys := make([]groupKey, 0, len(disp))
	for k := range disp {
		keys = append(keys, k)
	}
	embeds := make([]dispEmbed, len(keys))
	sim.ParallelDo(len(keys), func(i int) {
		embeds[i] = embedCell(disp[keys[i]])
	})
	out := make(map[string]dispEmbed, len(keys))
	for i, k := range keys {
		out[cellKeyString(k)] = embeds[i]
	}
	return out
}

func cellKeyString(k groupKey) string {
	return fmt.Sprintf("%s|%.0f|%.0f|%d", k.skill, k.lengthFt, k.slope, k.clock)
}

func embedCell(d *dispData) dispEmbed {
	e := dispEmbed{
		Ro: d.rollout, N: d.nTrials, Holed: d.nHoled, Runaway: d.nRunaway,
		Miss: d.nMiss, Kept: d.nKept,
		Bx: round4(d.ball.X), By: round4(d.ball.Y),
		Ax: round4(d.axis.X), Ay: round4(d.axis.Y),
	}
	hullIdx := geom.ConvexHullIndices(d.pts)
	hull := make([]geom.Pt, len(hullIdx))
	for i, j := range hullIdx {
		hull[i] = d.pts[j]
	}
	e.HullA = math.Abs(geom.PolyArea(hull))
	e.Hull = quantize(hull)

	if len(d.pts) >= minKDEMisses {
		hx, hy := geom.BandwidthScott(d.pts)
		g := geom.KDEGrid(d.pts, hx, hy, kdeGridN, kdeGridN)
		for _, mass := range hdrMasses {
			rings := geom.Contours(g, geom.HDRLevel(g, d.pts, mass))
			lvl := hdrLevel{P: int(math.Round(100 * mass)), A: geom.RingsArea(rings)}
			for _, r := range rings {
				lvl.Rings = append(lvl.Rings, quantize(geom.DecimateRing(r, maxRingVerts)))
			}
			e.HDR = append(e.HDR, lvl)
		}
	}

	// Thin for display, but keep the hull vertices so its corners always have
	// a dot under them.
	shown := map[int]bool{}
	for _, j := range hullIdx {
		shown[j] = true
	}
	n := len(d.pts)
	step := max(1, (n+embedMaxPts-1)/embedMaxPts)
	for j := 0; j < n; j += step {
		shown[j] = true
	}
	idx := make([]int, 0, len(shown))
	for j := range shown {
		idx = append(idx, j)
	}
	sort.Ints(idx)
	pts := make([]geom.Pt, len(idx))
	for i, j := range idx {
		pts[i] = d.pts[j]
	}
	e.Pts = quantize(pts)
	return e
}

// quantize flattens points to centimeter integers (x, y interleaved).
func quantize(pts []geom.Pt) []int16 {
	out := make([]int16, 0, 2*len(pts))
	for _, p := range pts {
		out = append(out, cm(p.X), cm(p.Y))
	}
	return out
}

func cm(v float64) int16 {
	q := math.Round(v * 100)
	return int16(math.Max(math.MinInt16, math.Min(math.MaxInt16, q)))
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }
