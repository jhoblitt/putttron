package geom

import (
	"math"
	"math/rand/v2"
	"testing"
)

func normalSample(n int, seed uint64) []Pt {
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b9))
	pts := make([]Pt, n)
	for i := range pts {
		pts[i] = Pt{rng.NormFloat64(), rng.NormFloat64()}
	}
	return pts
}

func TestKDEMassIntegrates(t *testing.T) {
	pts := normalSample(2000, 11)
	hx, hy := BandwidthScott(pts)
	g := KDEGrid(pts, hx, hy, 96, 96)
	var sum float64
	for _, v := range g.Z {
		sum += v
	}
	if mass := sum * g.Dx * g.Dy; math.Abs(mass-1) > 0.02 {
		t.Errorf("KDE integrates to %.4f, want 1 ±2%%", mass)
	}
}

func TestBandwidthScott(t *testing.T) {
	pts := normalSample(4096, 3)
	hx, hy := BandwidthScott(pts)
	// sigma ~ 1, n^(-1/6) for 4096 = 1/4.
	if math.Abs(hx-0.25) > 0.02 || math.Abs(hy-0.25) > 0.02 {
		t.Errorf("bandwidths (%.4f, %.4f), want ~0.25 for unit-sigma n=4096", hx, hy)
	}
	// A sample with no spread across one axis must not collapse the kernel.
	line := []Pt{{0, 0}, {1, 0}, {2, 0}, {3, 0}}
	if _, hy := BandwidthScott(line); hy != bwFloor {
		t.Errorf("collinear sample gave hy = %g, want the floor %g", hy, bwFloor)
	}
}

// The HDR level is defined so a stated fraction of the sample lies inside it,
// and higher mass must mean a lower threshold enclosing more area.
func TestHDRLevelGaussianSample(t *testing.T) {
	pts := normalSample(4000, 7)
	hx, hy := BandwidthScott(pts)
	g := KDEGrid(pts, hx, hy, 96, 96)

	var lastLevel, lastArea float64
	for i, mass := range []float64{0.5, 0.8, 0.95} {
		lvl := HDRLevel(g, pts, mass)
		inside := 0
		for _, p := range pts {
			if g.Bilinear(p.X, p.Y) >= lvl {
				inside++
			}
		}
		if got := float64(inside) / float64(len(pts)); math.Abs(got-mass) > 0.03 {
			t.Errorf("%.0f%% HDR encloses %.3f of the sample, want %.2f ±0.03", 100*mass, got, mass)
		}
		area := RingsArea(Contours(g, lvl))
		if i > 0 {
			if lvl >= lastLevel {
				t.Errorf("level for mass %.2f (%.5f) not below the previous (%.5f)", mass, lvl, lastLevel)
			}
			if area <= lastArea {
				t.Errorf("area for mass %.2f (%.4f) not above the previous (%.4f)", mass, area, lastArea)
			}
		}
		lastLevel, lastArea = lvl, area
	}
}

func TestKDEEmpty(t *testing.T) {
	g := KDEGrid(nil, 0.1, 0.1, 32, 32)
	if g.Bilinear(0, 0) != 0 {
		t.Error("empty KDE is not zero")
	}
	if lvl := HDRLevel(g, nil, 0.5); !math.IsInf(lvl, 1) {
		t.Errorf("HDR level of an empty sample = %v, want +Inf", lvl)
	}
}
