package geom

import (
	"math"
	"testing"
)

func TestConvexHullSquare(t *testing.T) {
	pts := []Pt{
		{0, 0}, {2, 0}, {2, 2}, {0, 2}, // corners
		{1, 1}, {0.5, 1.5}, {1.5, 0.4}, // interior
	}
	hull := ConvexHull(pts)
	if len(hull) != 4 {
		t.Fatalf("hull has %d vertices, want 4: %v", len(hull), hull)
	}
	for _, want := range []Pt{{0, 0}, {2, 0}, {2, 2}, {0, 2}} {
		found := false
		for _, h := range hull {
			if h == want {
				found = true
			}
		}
		if !found {
			t.Errorf("hull missing corner %v: %v", want, hull)
		}
	}
	if a := PolyArea(hull); math.Abs(a-4) > 1e-12 {
		t.Errorf("hull area = %g, want 4 (CCW positive)", a)
	}
}

func TestConvexHullDegenerate(t *testing.T) {
	for _, tc := range []struct {
		name string
		pts  []Pt
		want int
	}{
		{"empty", nil, 0},
		{"single", []Pt{{1, 1}}, 1},
		{"pair", []Pt{{1, 1}, {3, 2}}, 2},
		{"duplicates", []Pt{{1, 1}, {1, 1}, {1, 1}}, 1},
		{"collinear", []Pt{{0, 0}, {1, 1}, {2, 2}, {3, 3}}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hull := ConvexHull(tc.pts)
			if len(hull) != tc.want {
				t.Errorf("hull = %v, want %d vertices", hull, tc.want)
			}
			if a := PolyArea(hull); a != 0 {
				t.Errorf("degenerate hull area = %g, want 0", a)
			}
		})
	}
}

func TestPolyAreaSigned(t *testing.T) {
	ccw := []Pt{{0, 0}, {3, 0}, {3, 2}, {0, 2}}
	if a := PolyArea(ccw); math.Abs(a-6) > 1e-12 {
		t.Errorf("CCW rectangle area = %g, want +6", a)
	}
	cw := []Pt{{0, 2}, {3, 2}, {3, 0}, {0, 0}}
	if a := PolyArea(cw); math.Abs(a+6) > 1e-12 {
		t.Errorf("CW rectangle area = %g, want -6", a)
	}
	tri := []Pt{{0, 0}, {4, 0}, {0, 3}}
	if a := PolyArea(tri); math.Abs(a-6) > 1e-12 {
		t.Errorf("triangle area = %g, want 6", a)
	}
}

func TestDecimateRing(t *testing.T) {
	var ring []Pt
	for i := 0; i < 100; i++ {
		th := 2 * math.Pi * float64(i) / 100
		ring = append(ring, Pt{math.Cos(th), math.Sin(th)})
	}
	got := DecimateRing(ring, 16)
	if len(got) > 16 {
		t.Errorf("decimated to %d vertices, want <= 16", len(got))
	}
	if got[0] != ring[0] {
		t.Errorf("first vertex changed: %v vs %v", got[0], ring[0])
	}
	short := ring[:9]
	if out := DecimateRing(short, 16); len(out) != 9 {
		t.Errorf("short ring decimated to %d, want 9 unchanged", len(out))
	}
}

func TestRingsAreaHoles(t *testing.T) {
	outer := []Pt{{0, 0}, {4, 0}, {4, 4}, {0, 4}} // CCW, area 16
	hole := []Pt{{1, 1}, {1, 3}, {3, 3}, {3, 1}}  // CW, area -4
	if a := RingsArea([][]Pt{outer, hole}); math.Abs(a-12) > 1e-12 {
		t.Errorf("ring area with hole = %g, want 12", a)
	}
}

func TestDistanceTransform(t *testing.T) {
	// A 9x9 grid, all true except the border: the center is 4 cells (and one
	// cell of edge margin) from the nearest false cell.
	const n = 9
	mask := make([]bool, n*n)
	for iy := 1; iy < n-1; iy++ {
		for ix := 1; ix < n-1; ix++ {
			mask[iy*n+ix] = true
		}
	}
	d := DistanceTransform(mask, n, n, 0.5)
	if got := d[4*n+4]; math.Abs(got-4*0.5) > 1e-9 {
		t.Errorf("center distance = %g, want %g", got, 4*0.5)
	}
	if got := d[0]; got != 0 {
		t.Errorf("a false cell has distance %g, want 0", got)
	}
	if got := d[1*n+1]; math.Abs(got-0.5) > 1e-9 {
		t.Errorf("cell against the border = %g, want %g", got, 0.5)
	}
	// Diagonal distances must be euclidean, not chamfer: (2,2) from the
	// border is 2 cells straight, not sqrt(2)*2.
	if got := d[2*n+2]; math.Abs(got-2*0.5) > 1e-9 {
		t.Errorf("diagonal-interior cell = %g, want %g", got, 2*0.5)
	}
}

// Eroding a dilated shape recovers it: the property the greens loader relies
// on to recover a putting surface from a collar-buffered grid.
func TestDistanceTransformErodesDilation(t *testing.T) {
	const n, cell = 60, 0.25
	inR, buf := 3.0, 2.0
	mask := make([]bool, n*n)
	center := float64(n-1) / 2
	for iy := 0; iy < n; iy++ {
		for ix := 0; ix < n; ix++ {
			dx := (float64(ix) - center) * cell
			dy := (float64(iy) - center) * cell
			if math.Hypot(dx, dy) <= inR+buf { // the dilated disc
				mask[iy*n+ix] = true
			}
		}
	}
	d := DistanceTransform(mask, n, n, cell)
	area, dilated := 0, 0
	for i := range mask {
		if mask[i] {
			dilated++
		}
		if d[i] >= buf {
			area++
		}
	}
	got := float64(area) * cell * cell
	// Rasterizing at cell centers fattens a shape by up to half a cell, so
	// the recovered disc lands between the true one and a one-cell-wider one.
	lo := math.Pi * inR * inR
	hi := math.Pi * (inR + cell) * (inR + cell)
	if got < lo || got > hi {
		t.Errorf("eroding the dilated disc gives %.1f m², want between %.1f and %.1f (the original, allowing for grid quantization)",
			got, lo, hi)
	}
	if float64(dilated)*cell*cell < 2*got {
		t.Error("the dilated mask is not meaningfully bigger than the recovered shape")
	}
}
