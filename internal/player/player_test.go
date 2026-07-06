package player

import (
	"math"
	"testing"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
)

// The solved error-free putt must pass through the hole and stop the
// requested rollout past it, on flat and on breaking configurations.
func TestSolveGeometry(t *testing.T) {
	for _, tc := range []struct {
		name     string
		slopePct float64
		ball     physics.Vec2
	}{
		{"flat", 0, physics.Vec2{X: -3}},
		{"uphill", 3.5, physics.Vec2{X: 4.5}},
		{"downhill", 3.5, physics.Vec2{X: -4.5}},
		{"sidehill", 5, physics.Vec2{Y: 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := physics.NewEnv(green.NewPlanar(tc.slopePct, physics.DecelFromStimp(10)), physics.PennerVC0)
			const rollout = 0.4
			aim, ok := Solve(env, tc.ball, rollout)
			if !ok {
				t.Fatal("solve failed")
			}
			vel := physics.Vec2{X: math.Cos(aim.Dir), Y: math.Sin(aim.Dir)}.Scale(aim.Speed)
			out := env.Roll(tc.ball, vel, false)
			if out.MinHoleDist > 0.005 {
				t.Errorf("trajectory misses hole center by %.4f m", out.MinHoleDist)
			}
			past := out.Rest.Sub(env.HolePos).Norm()
			if math.Abs(past-rollout) > 0.02 {
				t.Errorf("rest %.3f m past hole, want %.3f", past, rollout)
			}
		})
	}
}

// On a sidehill putt the solved aim must borrow to the uphill (−X) side.
func TestSolveBorrowsUphill(t *testing.T) {
	env := physics.NewEnv(green.NewPlanar(3.5, physics.DecelFromStimp(10)), physics.PennerVC0)
	ball := physics.Vec2{Y: 3}
	aim, ok := Solve(env, ball, 0.3)
	if !ok {
		t.Fatal("solve failed")
	}
	// Direction from ball to hole is -Y (angle -π/2); uphill of that is
	// toward -X, i.e. angle more negative than -π/2.
	if aim.Dir > -math.Pi/2 {
		t.Errorf("aim %.4f rad does not borrow uphill (want < %.4f)", aim.Dir, -math.Pi/2)
	}
}
