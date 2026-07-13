package green

import (
	"math"
	"testing"
)

// planeGrid samples f at the north-up row-major node positions.
func sampleGrid(rows, cols int, x0, y0, dx float64, f func(x, y float64) float64) []float64 {
	z := make([]float64, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			z[i*cols+j] = f(x0+float64(j)*dx, y0-float64(i)*dx)
		}
	}
	return z
}

// Catmull-Rom reproduces linear functions exactly, so a sampled plane must
// interpolate back to the plane (interior — edge-clamped stencils differ).
func TestHeightmapPlane(t *testing.T) {
	const rows, cols, dx = 24, 30, 0.25
	x0, y0 := -3.0, 2.5
	f := func(x, y float64) float64 { return 0.15 - 0.03*x + 0.01*y }
	h, err := NewHeightmap(sampleGrid(rows, cols, x0, y0, dx, f), rows, cols, x0, y0, dx, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]float64{{-1.7, 0.3}, {0.42, -1.9}, {2.11, 1.07}, {-0.33, -2.6}} {
		x, y := p[0], p[1]
		if got := h.Elevation(x, y); math.Abs(got-f(x, y)) > 1e-9 {
			t.Errorf("Elevation(%g, %g) = %.12f, want %.12f", x, y, got, f(x, y))
		}
		gx, gy := h.Gradient(x, y)
		if math.Abs(gx-(-0.03)) > 1e-9 || math.Abs(gy-0.01) > 1e-9 {
			t.Errorf("Gradient(%g, %g) = (%.12f, %.12f), want (-0.03, 0.01)", x, y, gx, gy)
		}
	}
	if ad := h.DecelCoeff(0, 0, 1, 0); ad != 0.55 {
		t.Errorf("DecelCoeff = %g, want 0.55", ad)
	}
}

// Keys' a=-1/2 kernel has quadratic precision; the analytic gradient of a
// sampled quadratic must be exact in the interior.
func TestHeightmapQuadratic(t *testing.T) {
	const rows, cols, dx = 30, 30, 0.25
	x0, y0 := -3.5, 3.5
	f := func(x, y float64) float64 { return 0.02*x*x - 0.01*x*y + 0.03*y*y }
	h, err := NewHeightmap(sampleGrid(rows, cols, x0, y0, dx, f), rows, cols, x0, y0, dx, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]float64{{-1.3, 0.7}, {0.61, -1.11}, {1.9, 2.0}} {
		x, y := p[0], p[1]
		if got := h.Elevation(x, y); math.Abs(got-f(x, y)) > 1e-9 {
			t.Errorf("Elevation(%g, %g) = %.12f, want %.12f", x, y, got, f(x, y))
		}
		gx, gy := h.Gradient(x, y)
		wantX := 0.04*x - 0.01*y
		wantY := -0.01*x + 0.06*y
		if math.Abs(gx-wantX) > 1e-9 || math.Abs(gy-wantY) > 1e-9 {
			t.Errorf("Gradient(%g, %g) = (%.12f, %.12f), want (%.12f, %.12f)",
				x, y, gx, gy, wantX, wantY)
		}
	}
}

// The interpolant is C1: gradients on either side of a node line agree.
func TestHeightmapC1AcrossNodes(t *testing.T) {
	const rows, cols, dx = 20, 20, 0.25
	x0, y0 := -2.0, 2.0
	f := func(x, y float64) float64 { return 0.05 * math.Sin(1.3*x) * math.Cos(0.9*y) }
	h, err := NewHeightmap(sampleGrid(rows, cols, x0, y0, dx, f), rows, cols, x0, y0, dx, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	nodeX := x0 + 8*dx
	const eps = 1e-9
	gxL, gyL := h.Gradient(nodeX-eps, 0.4)
	gxR, gyR := h.Gradient(nodeX+eps, 0.4)
	if math.Abs(gxL-gxR) > 1e-6 || math.Abs(gyL-gyR) > 1e-6 {
		t.Errorf("gradient jumps across node line: (%g,%g) vs (%g,%g)", gxL, gyL, gxR, gyR)
	}
}

func TestHeightmapOnGreen(t *testing.T) {
	const rows, cols, dx = 12, 12, 0.25
	x0, y0 := 0.0, 0.0
	z := make([]float64, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			if j >= 3 && j <= 8 && i >= 2 && i <= 9 {
				z[i*cols+j] = 0.01 * float64(i+j)
			} else {
				z[i*cols+j] = math.NaN()
			}
		}
	}
	h, err := NewHeightmap(z, rows, cols, x0, y0, dx, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	if !h.OnGreen(x0+5*dx, y0-5*dx) {
		t.Error("center of valid region reported off-green")
	}
	if h.OnGreen(x0-1, y0) || h.OnGreen(x0, y0+1) {
		t.Error("point outside grid rect reported on-green")
	}
	// Mask boundary between NaN col 2 and valid col 3 sits at fc = 2.5.
	yMid := y0 - 5*dx
	if !h.OnGreen(x0+2.51*dx, yMid) {
		t.Error("just inside mask boundary reported off-green")
	}
	if h.OnGreen(x0+2.49*dx, yMid) {
		t.Error("just outside mask boundary reported on-green")
	}
	// Elevation stays finite even deep in the inpainted region.
	if v := h.Elevation(x0, y0); math.IsNaN(v) || math.IsInf(v, 0) {
		t.Errorf("inpainted corner elevation not finite: %g", v)
	}
}

func TestHeightmapInpaintDeterministic(t *testing.T) {
	const rows, cols, dx = 16, 14, 0.25
	x0, y0 := -1.0, 1.0
	f := func(x, y float64) float64 { return -0.02*x + 0.015*y }
	z := sampleGrid(rows, cols, x0, y0, dx, f)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			if i < 3 || j < 3 || i >= rows-3 || j >= cols-3 {
				z[i*cols+j] = math.NaN()
			}
		}
	}
	z2 := append([]float64(nil), z...)
	a, err := NewHeightmap(z, rows, cols, x0, y0, dx, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewHeightmap(z2, rows, cols, x0, y0, dx, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			va, vb := a.ZAt(i, j), b.ZAt(i, j)
			if va != vb {
				t.Fatalf("inpaint not deterministic at (%d,%d): %g vs %g", i, j, va, vb)
			}
			if math.IsNaN(va) || math.IsInf(va, 0) {
				t.Fatalf("non-finite cell after inpaint at (%d,%d): %g", i, j, va)
			}
		}
	}
}

func TestHeightmapWithDecel(t *testing.T) {
	const rows, cols, dx = 8, 8, 0.25
	z := sampleGrid(rows, cols, 0, 0, dx, func(x, y float64) float64 { return 0 })
	h, err := NewHeightmap(z, rows, cols, 0, 0, dx, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	fast := h.WithDecel(0.44)
	if fast.DecelCoeff(0.5, -0.5, 1, 0) != 0.44 || h.DecelCoeff(0.5, -0.5, 1, 0) != 0.55 {
		t.Error("WithDecel did not isolate the deceleration")
	}
	if fast.Elevation(0.7, -0.7) != h.Elevation(0.7, -0.7) {
		t.Error("WithDecel changed the surface")
	}
}

// A grid whose support runs past the putting surface (a collar buffer) must
// report OnGreen for the surface only, while elevation stays defined out to
// the edge of the support — the integrator samples past wherever the ball is.
func TestHeightmapInsetGreen(t *testing.T) {
	const rows, cols, dx = 80, 80, 0.25
	x0, y0 := -10.0, 10.0
	const support = 6.0 // radius of the modeled disc, m
	z := make([]float64, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			x := x0 + float64(j)*dx
			y := y0 - float64(i)*dx
			if math.Hypot(x, y) > support {
				z[i*cols+j] = math.NaN()
				continue
			}
			z[i*cols+j] = -0.02 * x
		}
	}
	h, err := NewHeightmap(z, rows, cols, x0, y0, dx, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	if !h.OnGreen(5.5, 0) {
		t.Fatal("before insetting, the whole support should be on-green")
	}

	const collar = 2.0
	area := h.InsetGreen(collar)
	wantR := support - collar
	if !h.OnGreen(wantR-0.5, 0) {
		t.Errorf("a point %.1f m from center is off-green after a %.0f m inset", wantR-0.5, collar)
	}
	if h.OnGreen(wantR+0.5, 0) {
		t.Errorf("a point out in the collar (%.1f m) is still on-green", wantR+0.5)
	}
	// Grid quantization fattens the rasterized disc by up to a cell.
	lo, hi := math.Pi*wantR*wantR, math.Pi*(wantR+dx)*(wantR+dx)
	if area < lo || area > hi {
		t.Errorf("inset area = %.1f m², want between %.1f and %.1f", area, lo, hi)
	}
	// Elevation must still be defined out in the collar.
	if v := h.Elevation(5.5, 0); math.IsNaN(v) || math.Abs(v-(-0.02*5.5)) > 1e-6 {
		t.Errorf("elevation in the collar = %v, want the modeled surface", v)
	}
	if h.WithDecel(0.4).OnGreen(wantR+0.5, 0) {
		t.Error("WithDecel did not carry the putting-surface mask")
	}
}
