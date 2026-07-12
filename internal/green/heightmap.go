package green

import (
	"errors"
	"fmt"
	"math"
)

// Heightmap is a Surface over a regular elevation grid in the local frame,
// north-up row-major: node (i, j) sits at (x0 + j·dx, y0 − i·dx), so row
// index grows southward and y0 is the northern edge. Cells that were NaN in
// the source grid are off-green: they are inpainted so the interpolant is
// finite everywhere (the integrator samples slightly past the boundary), and
// the original mask drives OnGreen.
type Heightmap struct {
	rows, cols int
	x0, y0, dx float64
	z          []float64 // inpainted, row-major [i*cols+j]
	valid      []bool    // original NaN mask
	decel      float64   // uniform a_d, m/s²
}

// NewHeightmap builds a Heightmap from a raw local-frame grid where NaN
// marks off-green cells. The grid is inpainted deterministically: repeated
// dilation passes fill each NaN cell with the mean of its finite 8-neighbors
// until none remain, then a few Jacobi smoothing passes relax the filled
// cells (original cells stay pinned).
func NewHeightmap(z []float64, rows, cols int, x0, y0, dx, decel float64) (*Heightmap, error) {
	if rows < 4 || cols < 4 {
		return nil, fmt.Errorf("grid %dx%d too small (need at least 4x4)", rows, cols)
	}
	if len(z) != rows*cols {
		return nil, fmt.Errorf("grid %dx%d needs %d values, got %d", rows, cols, rows*cols, len(z))
	}
	if dx <= 0 {
		return nil, fmt.Errorf("cell size %g must be positive", dx)
	}
	h := &Heightmap{
		rows: rows, cols: cols,
		x0: x0, y0: y0, dx: dx,
		z:     make([]float64, len(z)),
		valid: make([]bool, len(z)),
		decel: decel,
	}
	nValid := 0
	for k, v := range z {
		h.z[k] = v
		if !math.IsNaN(v) {
			if math.IsInf(v, 0) {
				return nil, fmt.Errorf("non-finite elevation at cell %d", k)
			}
			h.valid[k] = true
			nValid++
		}
	}
	if nValid == 0 {
		return nil, errors.New("grid has no valid cells")
	}
	h.inpaint()
	return h, nil
}

func (h *Heightmap) inpaint() {
	cur := h.z
	next := make([]float64, len(cur))
	for {
		remaining := 0
		copy(next, cur)
		for i := 0; i < h.rows; i++ {
			for j := 0; j < h.cols; j++ {
				k := i*h.cols + j
				if !math.IsNaN(cur[k]) {
					continue
				}
				var sum float64
				n := 0
				for di := -1; di <= 1; di++ {
					for dj := -1; dj <= 1; dj++ {
						if di == 0 && dj == 0 {
							continue
						}
						ni, nj := i+di, j+dj
						if ni < 0 || ni >= h.rows || nj < 0 || nj >= h.cols {
							continue
						}
						if v := cur[ni*h.cols+nj]; !math.IsNaN(v) {
							sum += v
							n++
						}
					}
				}
				if n > 0 {
					next[k] = sum / float64(n)
				} else {
					remaining++
				}
			}
		}
		cur, next = next, cur
		if remaining == 0 {
			break
		}
	}

	for pass := 0; pass < 8; pass++ {
		copy(next, cur)
		for i := 0; i < h.rows; i++ {
			for j := 0; j < h.cols; j++ {
				k := i*h.cols + j
				if h.valid[k] {
					continue
				}
				var sum float64
				n := 0
				for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					ni, nj := i+d[0], j+d[1]
					if ni < 0 || ni >= h.rows || nj < 0 || nj >= h.cols {
						continue
					}
					sum += cur[ni*h.cols+nj]
					n++
				}
				next[k] = sum / float64(n)
			}
		}
		cur, next = next, cur
	}
	copy(h.z, cur)
}

// crWeights are the Catmull-Rom (Keys a=−1/2) basis values and derivatives
// at parameter t for the 4-point stencil p0..p3 around the interval [p1, p2].
func crWeights(t float64) (w, d [4]float64) {
	t2, t3 := t*t, t*t*t
	w[0] = -0.5*t3 + t2 - 0.5*t
	w[1] = 1.5*t3 - 2.5*t2 + 1
	w[2] = -1.5*t3 + 2*t2 + 0.5*t
	w[3] = 0.5*t3 - 0.5*t2
	d[0] = -1.5*t2 + 2*t - 0.5
	d[1] = 4.5*t2 - 5*t
	d[2] = -4.5*t2 + 4*t + 0.5
	d[3] = 1.5*t2 - t
	return w, d
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// frac maps (x, y) to fractional grid coordinates (fr, fc), clamped to the
// grid so outside queries see a flat extension of the edge.
func (h *Heightmap) frac(x, y float64) (fr, fc float64) {
	fc = (x - h.x0) / h.dx
	fr = (h.y0 - y) / h.dx
	fc = math.Max(0, math.Min(float64(h.cols-1), fc))
	fr = math.Max(0, math.Min(float64(h.rows-1), fr))
	return fr, fc
}

// stencil returns the 4x4 Catmull-Rom stencil (edge-clamped indices) and the
// in-cell parameters for a fractional coordinate pair.
func (h *Heightmap) stencil(fr, fc float64) (is, js [4]int, tr, tc float64) {
	i0 := clampInt(int(fr), 0, h.rows-2)
	j0 := clampInt(int(fc), 0, h.cols-2)
	tr = fr - float64(i0)
	tc = fc - float64(j0)
	for a := 0; a < 4; a++ {
		is[a] = clampInt(i0-1+a, 0, h.rows-1)
		js[a] = clampInt(j0-1+a, 0, h.cols-1)
	}
	return is, js, tr, tc
}

func (h *Heightmap) Elevation(x, y float64) float64 {
	fr, fc := h.frac(x, y)
	is, js, tr, tc := h.stencil(fr, fc)
	wr, _ := crWeights(tr)
	wc, _ := crWeights(tc)
	var v float64
	for a := 0; a < 4; a++ {
		row := is[a] * h.cols
		var rowv float64
		for b := 0; b < 4; b++ {
			rowv += wc[b] * h.z[row+js[b]]
		}
		v += wr[a] * rowv
	}
	return v
}

// Gradient is the analytic derivative of the bicubic interpolant. The row
// axis runs north→south, so ∂/∂y carries a −1/dx factor.
func (h *Heightmap) Gradient(x, y float64) (gx, gy float64) {
	fr, fc := h.frac(x, y)
	is, js, tr, tc := h.stencil(fr, fc)
	wr, dr := crWeights(tr)
	wc, dc := crWeights(tc)
	for a := 0; a < 4; a++ {
		row := is[a] * h.cols
		var rowW, rowD float64
		for b := 0; b < 4; b++ {
			z := h.z[row+js[b]]
			rowW += wc[b] * z
			rowD += dc[b] * z
		}
		gx += wr[a] * rowD
		gy += dr[a] * rowW
	}
	return gx / h.dx, gy * (-1 / h.dx)
}

func (h *Heightmap) DecelCoeff(x, y, dirX, dirY float64) float64 { return h.decel }

// OnGreen reports whether the point lies in the valid (non-NaN) region:
// inside the grid rectangle and with the bilinearly interpolated 0/1 mask at
// least 0.5 — the boundary sits halfway between a valid node and a NaN node.
func (h *Heightmap) OnGreen(x, y float64) bool {
	fc := (x - h.x0) / h.dx
	fr := (h.y0 - y) / h.dx
	if fc < 0 || fc > float64(h.cols-1) || fr < 0 || fr > float64(h.rows-1) {
		return false
	}
	i0 := clampInt(int(fr), 0, h.rows-2)
	j0 := clampInt(int(fc), 0, h.cols-2)
	tr := fr - float64(i0)
	tc := fc - float64(j0)
	m := func(i, j int) float64 {
		if h.valid[i*h.cols+j] {
			return 1
		}
		return 0
	}
	v := (1-tr)*((1-tc)*m(i0, j0)+tc*m(i0, j0+1)) +
		tr*((1-tc)*m(i0+1, j0)+tc*m(i0+1, j0+1))
	return v >= 0.5
}

// WithDecel returns a copy sharing the elevation grids but with a different
// rolling deceleration (per-run green speed).
func (h *Heightmap) WithDecel(decel float64) *Heightmap {
	c := *h
	c.decel = decel
	return &c
}

func (h *Heightmap) GridSize() (rows, cols int) { return h.rows, h.cols }
func (h *Heightmap) Origin() (x0, y0 float64)   { return h.x0, h.y0 }
func (h *Heightmap) CellSize() float64          { return h.dx }

// NodePos returns the world position of grid node (i, j).
func (h *Heightmap) NodePos(i, j int) (x, y float64) {
	return h.x0 + float64(j)*h.dx, h.y0 - float64(i)*h.dx
}

// ValidAt reports the original (pre-inpaint) validity of node (i, j).
func (h *Heightmap) ValidAt(i, j int) bool { return h.valid[i*h.cols+j] }

// ZAt returns the (inpainted) elevation at node (i, j).
func (h *Heightmap) ZAt(i, j int) float64 { return h.z[i*h.cols+j] }
