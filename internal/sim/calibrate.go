package sim

import (
	"math"
	"math/rand/v2"

	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
)

// MakeRate estimates first-putt make probability from a distance (no
// follow-up scoring, so no field needed). Used by the calibration gate.
func MakeRate(env *physics.Env, ball physics.Vec2, skill player.Skill,
	rollout float64, n int, seed uint64) (make, se float64, ok bool) {

	aim, ok := player.Solve(env, ball, rollout)
	if !ok {
		return 0, 0, false
	}
	dist := ball.Sub(env.HolePos).Norm() + rollout
	rng := rand.New(rand.NewPCG(seed, 0xca11b))
	makes := 0
	for t := 0; t < n; t++ {
		dir, speed := skill.Perturb(aim, dist, rng)
		vel := physics.Vec2{X: math.Cos(dir), Y: math.Sin(dir)}.Scale(speed)
		if env.Roll(ball, vel, true).Holed {
			makes++
		}
	}
	make = float64(makes) / float64(n)
	return make, math.Sqrt(make * (1 - make) / float64(n)), true
}
