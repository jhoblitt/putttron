package player

import (
	"math"
	"testing"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
)

// The solver must converge over the whole Phase-1 sweep envelope:
// stimp 8–12, slopes 0–5% grade, all clock positions, 10–20 ft, rollouts 0–1.2 m.
func TestSolveEnvelope(t *testing.T) {
	if testing.Short() {
		t.Skip("envelope sweep")
	}
	fails := 0
	for _, stimp := range []float64{8, 10, 12} {
		for _, slope := range []float64{0, 1, 2, 3, 4, 5} {
			env := physics.NewEnv(green.NewPlanar(slope, physics.DecelFromStimp(stimp)), physics.PennerVC0)
			for _, ball := range []physics.Vec2{
				{X: -6.096}, {X: 6.096}, {Y: 6.096}, // 20 ft at 12, 6, 3 o'clock
				{X: -3.048}, {X: 3.048}, {Y: 3.048}, // 10 ft
			} {
				for _, rollout := range []float64{0, 0.4, 0.8, 1.2} {
					aim, ok := Solve(env, ball, rollout)
					if !ok {
						t.Errorf("solve failed: stimp=%g slope=%g ball=%+v rollout=%g", stimp, slope, ball, rollout)
						fails++
						continue
					}
					vel := physics.Vec2{X: math.Cos(aim.Dir), Y: math.Sin(aim.Dir)}.Scale(aim.Speed)
					out := env.Roll(ball, vel, false)
					// Rollout is defined as path length past the hole (the
					// post-hole path curves on breaking putts, so it is not
					// the straight-line leave).
					pathPast := out.PathLen - out.PathLenAtMin
					minDTol := 0.005
					if rollout < 0.05 {
						// A dying putt's closest approach absorbs the
						// solver's 8 mm length tolerance.
						minDTol = 0.012
					}
					if out.MinHoleDist > minDTol || math.Abs(pathPast-rollout) > 0.02 {
						t.Errorf("bad geometry: stimp=%g slope=%g ball=%+v rollout=%g minD=%.4f pathPast=%.3f",
							stimp, slope, ball, rollout, out.MinHoleDist, pathPast)
						fails++
					}
				}
			}
			if fails > 10 {
				t.Fatal("too many failures, aborting")
			}
		}
	}
}
