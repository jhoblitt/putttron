package main

import (
	"math"
	"testing"
)

// quadRows builds rows with E exactly on a parabola with vertex at roV, plus
// paired stats consistent with dE = E - E(best grid point).
func quadRows(rollouts []float64, roV, curve, pairSE float64) []rptRow {
	e := func(ro float64) float64 { return 2 + curve*(ro-roV)*(ro-roV) }
	best := 0
	for i, ro := range rollouts {
		if e(ro) < e(rollouts[best]) {
			best = i
		}
	}
	rows := make([]rptRow, len(rollouts))
	for i, ro := range rollouts {
		rows[i] = rptRow{
			rollout: ro, solveOK: true, eStrokes: e(ro), eSE: 0.006,
			dE: e(ro) - e(rollouts[best]), dSE: pairSE, hasPaired: true,
		}
	}
	return rows
}

func TestArgminSERefinesToVertex(t *testing.T) {
	grid := []float64{0, 0.1, 0.2, 0.3, 0.4, 0.5}
	g := quadRows(grid, 0.27, 0.5, 0.001)
	best, roStar, _, _ := argminSE(g)
	if g[best].rollout != 0.3 {
		t.Errorf("grid argmin = %g, want 0.3", g[best].rollout)
	}
	if math.Abs(roStar-0.27) > 1e-9 {
		t.Errorf("refined optimum = %g, want 0.27", roStar)
	}
}

// At a grid edge there is no neighbor pair; the refinement must fall back to
// the grid point rather than extrapolate.
func TestArgminSEEdgeFallback(t *testing.T) {
	grid := []float64{0, 0.1, 0.2}
	g := quadRows(grid, -0.1, 0.5, 0.001)
	_, roStar, _, _ := argminSE(g)
	if roStar != 0 {
		t.Errorf("edge optimum = %g, want 0 (grid fallback)", roStar)
	}
}

func TestArgminSEPairedPlateau(t *testing.T) {
	grid := []float64{0, 0.1, 0.2, 0.3, 0.4}
	// Vertex at 0.2; curve 0.5 → ΔE at ±0.1 is 0.005, at ±0.2 is 0.02.
	g := quadRows(grid, 0.2, 0.5, 0.006)
	_, _, lo, hi := argminSE(g)
	if lo != 0.1 || hi != 0.3 {
		t.Errorf("paired plateau [%g, %g], want [0.1, 0.3]", lo, hi)
	}

	// Tighter paired SE must shrink the plateau to the minimum alone, even
	// though the ±0.1 neighbors (ΔE = 0.005) are within one MARGINAL SE
	// (0.006) of the min and would qualify under the fallback rule.
	g = quadRows(grid, 0.2, 0.5, 0.001)
	_, _, lo, hi = argminSE(g)
	if lo != 0.2 || hi != 0.2 {
		t.Errorf("tight paired plateau [%g, %g], want [0.2, 0.2]", lo, hi)
	}
}

// Without paired columns the plateau falls back to the marginal-SE band.
func TestArgminSEMarginalFallback(t *testing.T) {
	grid := []float64{0, 0.1, 0.2, 0.3, 0.4}
	g := quadRows(grid, 0.2, 0.5, 0)
	for i := range g {
		g[i].hasPaired = false
	}
	_, _, lo, hi := argminSE(g)
	// eSE 0.006: ΔE 0.005 at ±0.1 qualifies, 0.02 at ±0.2 does not.
	if lo != 0.1 || hi != 0.3 {
		t.Errorf("marginal plateau [%g, %g], want [0.1, 0.3]", lo, hi)
	}
}
