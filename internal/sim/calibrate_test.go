package sim

import (
	"math"
	"testing"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
)

// The calibration gate, enforced: the fitted profiles must keep reproducing
// the published flat-green make percentages they were calibrated against
// (docs/literature.md §5; full table in results/calibration.md, regenerated
// by `putttron calibrate`). Green speeds match the sources: pros on Stimp 11,
// amateurs on Stimp 9 (Bansal & Broadie 2008). A physics or solver change
// that silently breaks calibration fails here instead of in the next sweep.
func TestCalibrationAnchors(t *testing.T) {
	if testing.Short() {
		t.Skip("Monte Carlo calibration anchors")
	}
	anchors := []struct {
		skill     string
		stimp     float64
		ft        float64
		published float64
	}{
		{"tour", 11, 10, 0.40},
		{"tour", 11, 20, 0.15},
		{"high", 9, 10, 0.20},
		{"high", 9, 20, 0.06},
		{"hcp30", 9, 10, 0.18},
	}
	// Fit residuals run up to ~2 points for the weak tiers (hcp30 short
	// putts are a documented exception, not anchored here); 6000 trials
	// puts the Monte Carlo SE near 0.6 points.
	const tol = 0.035

	skills := make([]player.Skill, len(anchors))
	for i, a := range anchors {
		skills[i] = testSkill(t, a.skill)
	}
	type result struct {
		make float64
		ok   bool
	}
	results := make([]result, len(anchors))
	ParallelDo(len(anchors), func(i int) {
		a := anchors[i]
		env := physics.NewEnv(green.NewPlanar(0, physics.DecelFromStimp(a.stimp)), physics.PennerVC0)
		ball := physics.Vec2{X: -a.ft * 0.3048}
		m, _, ok := MakeRate(env, ball, skills[i], 0.25, 6000, 1)
		results[i] = result{m, ok}
	})
	for i, a := range anchors {
		r := results[i]
		if !r.ok {
			t.Errorf("%s %g ft: solve failed", a.skill, a.ft)
			continue
		}
		if math.Abs(r.make-a.published) > tol {
			t.Errorf("%s %g ft (stimp %g): sim make %.3f, published %.3f (tol %.3f)",
				a.skill, a.ft, a.stimp, r.make, a.published, tol)
		}
	}
}
