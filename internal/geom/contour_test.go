package geom

import (
	"math"
	"math/rand/v2"
	"testing"
)

// A unit isotropic Gaussian cut at half its peak must contour to a circle of
// radius sqrt(2 ln 2) enclosing pi*2*ln2.
func TestContoursGaussianCircle(t *testing.T) {
	g := GridFromFunc(-4, -4, 4, 4, 96, 96, func(x, y float64) float64 {
		return math.Exp(-(x*x + y*y) / 2)
	})
	rings := Contours(g, 0.5)
	if len(rings) != 1 {
		t.Fatalf("got %d rings, want 1", len(rings))
	}
	wantR := math.Sqrt(2 * math.Ln2)
	for _, p := range rings[0] {
		r := math.Hypot(p.X, p.Y)
		if math.Abs(r-wantR)/wantR > 0.01 {
			t.Fatalf("contour vertex at radius %.4f, want %.4f ±1%%", r, wantR)
		}
	}
	wantA := math.Pi * 2 * math.Ln2
	if a := RingsArea(rings); math.Abs(a-wantA)/wantA > 0.01 {
		t.Errorf("contour area = %.4f, want %.4f ±1%%", a, wantA)
	}
}

// A radial ridge produces a region with a hole: an outer CCW ring and an
// inner CW one, whose signed sum is the annulus area.
func TestContoursAnnulus(t *testing.T) {
	const bigR, w = 2.0, 0.5
	g := GridFromFunc(-4, -4, 4, 4, 160, 160, func(x, y float64) float64 {
		r := math.Hypot(x, y)
		d := (r - bigR) / w
		return math.Exp(-d * d)
	})
	rings := Contours(g, 0.5)
	if len(rings) != 2 {
		t.Fatalf("got %d rings, want 2 (outer + hole)", len(rings))
	}
	a0, a1 := PolyArea(rings[0]), PolyArea(rings[1])
	if a0*a1 >= 0 {
		t.Fatalf("rings have the same orientation: areas %.3f, %.3f", a0, a1)
	}
	halfW := w * math.Sqrt(math.Ln2)
	rOut, rIn := bigR+halfW, bigR-halfW
	wantA := math.Pi * (rOut*rOut - rIn*rIn)
	if a := RingsArea(rings); math.Abs(a-wantA)/wantA > 0.02 {
		t.Errorf("annulus area = %.4f, want %.4f ±2%%", a, wantA)
	}
}

// A hot 2x2 block in a 4x4 grid contours to an octagon whose vertices sit at
// exact half-cell crossings.
func TestContoursHandGrid(t *testing.T) {
	g := &Grid{X0: 0, Y0: 0, Dx: 1, Dy: 1, Nx: 4, Ny: 4, Z: make([]float64, 16)}
	for _, n := range [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}} {
		g.Z[n[1]*4+n[0]] = 2
	}
	rings := Contours(g, 1)
	if len(rings) != 1 {
		t.Fatalf("got %d rings, want 1", len(rings))
	}
	ring := rings[0]
	if len(ring) != 8 {
		t.Fatalf("ring has %d vertices, want 8: %v", len(ring), ring)
	}
	want := []Pt{{1, 0.5}, {2, 0.5}, {2.5, 1}, {2.5, 2}, {2, 2.5}, {1, 2.5}, {0.5, 2}, {0.5, 1}}
	for _, w := range want {
		found := false
		for _, p := range ring {
			if math.Abs(p.X-w.X) < 1e-12 && math.Abs(p.Y-w.Y) < 1e-12 {
				found = true
			}
		}
		if !found {
			t.Errorf("ring missing crossing %v: %v", w, ring)
		}
	}
	// 2x2 square with its four corners cut by half-cell triangles.
	if a := PolyArea(ring); math.Abs(a-3.5) > 1e-12 {
		t.Errorf("octagon area = %g, want 3.5 (CCW)", a)
	}

	again := Contours(g, 1)
	for i := range ring {
		if again[0][i] != ring[i] {
			t.Fatalf("contours not deterministic at vertex %d", i)
		}
	}
}

func TestContoursConstantGrid(t *testing.T) {
	g := GridFromFunc(0, 0, 1, 1, 8, 8, func(x, y float64) float64 { return 3 })
	if rings := Contours(g, 5); rings != nil {
		t.Errorf("constant grid below level yields %d rings, want none", len(rings))
	}
	// Entirely above the level: the region is the whole grid, and the
	// below-threshold padding closes it into one ring just outside the data.
	rings := Contours(g, 1)
	if len(rings) != 1 {
		t.Fatalf("constant grid above level yields %d rings, want 1 around the domain", len(rings))
	}
	if a := PolyArea(rings[0]); a <= 1 {
		t.Errorf("domain-spanning ring area = %g, want > 1 (the unit grid)", a)
	}
}

// Two well-separated clusters must contour to two disjoint rings.
func TestContoursBimodal(t *testing.T) {
	rng := rand.New(rand.NewPCG(4, 5))
	var pts []Pt
	for i := 0; i < 1500; i++ {
		c := 0.0
		if i%2 == 0 {
			c = 3.0
		}
		pts = append(pts, Pt{rng.NormFloat64()*0.25 + c, rng.NormFloat64() * 0.25})
	}
	hx, hy := BandwidthScott(pts)
	g := KDEGrid(pts, hx, hy, 96, 96)
	rings := Contours(g, HDRLevel(g, pts, 0.5))
	if len(rings) != 2 {
		t.Fatalf("bimodal 50%% HDR gave %d rings, want 2", len(rings))
	}
	for _, r := range rings {
		if PolyArea(r) <= 0 {
			t.Errorf("cluster ring is not CCW: area %g", PolyArea(r))
		}
	}
}
