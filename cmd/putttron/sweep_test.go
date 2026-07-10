package main

import (
	"math"
	"testing"

	"github.com/jhoblitt/putttron/internal/sim"
)

func rowWithStrokes(rollout float64, strokes ...float64) sweepRow {
	var sum float64
	for _, s := range strokes {
		sum += s
	}
	return sweepRow{
		rolloutM: rollout,
		res: sim.CellResult{
			SolveOK:  true,
			EStrokes: sum / float64(len(strokes)),
			Strokes:  append([]float64(nil), strokes...),
		},
	}
}

func TestPairGroup(t *testing.T) {
	a := rowWithStrokes(0.1, 2, 2, 3, 2) // E = 2.25
	b := rowWithStrokes(0.2, 2, 2, 2, 2) // E = 2.00, best
	c := rowWithStrokes(0.3, 3, 2, 2, 3) // E = 2.50
	g := []*sweepRow{&a, &b, &c}
	pairGroup(g)

	if b.dEBest != 0 || b.dEPairSE != 0 {
		t.Errorf("best row ΔE = %g ± %g, want 0 ± 0", b.dEBest, b.dEPairSE)
	}
	if math.Abs(a.dEBest-0.25) > 1e-12 {
		t.Errorf("row a ΔE = %g, want 0.25", a.dEBest)
	}
	// a's per-trial deltas vs b: {0,0,1,0} → mean 0.25, var 0.1875, se = √(0.1875/4).
	if want := math.Sqrt(0.1875 / 4); math.Abs(a.dEPairSE-want) > 1e-12 {
		t.Errorf("row a paired SE = %g, want %g", a.dEPairSE, want)
	}
	if math.Abs(c.dEBest-0.5) > 1e-12 {
		t.Errorf("row c ΔE = %g, want 0.5", c.dEBest)
	}
	for _, r := range g {
		if r.res.Strokes != nil {
			t.Error("per-trial strokes not released after pairing")
		}
	}
}

func TestPairGroupSkipsUnsolved(t *testing.T) {
	bad := sweepRow{rolloutM: 0.1}
	ok := rowWithStrokes(0.2, 2, 2)
	g := []*sweepRow{&bad, &ok}
	pairGroup(g)
	if bad.dEBest != 0 || bad.dEPairSE != 0 {
		t.Errorf("unsolved row got paired stats: %g ± %g", bad.dEBest, bad.dEPairSE)
	}
	if ok.dEBest != 0 {
		t.Errorf("solved best row ΔE = %g, want 0", ok.dEBest)
	}

	allBad := []*sweepRow{{rolloutM: 0.1}, {rolloutM: 0.2}}
	pairGroup(allBad) // must not panic with no solvable rows
}
