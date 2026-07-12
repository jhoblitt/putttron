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
