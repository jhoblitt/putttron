package sim

import (
	"math"
	"math/rand/v2"

	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
)

// CellResult is the Monte Carlo outcome of one first-putt parameter cell.
type CellResult struct {
	SolveOK    bool
	Make       float64 // P(hole first putt)
	MakeSE     float64
	ThreePlus  float64 // P(3 or more putts) ≈ Σ_miss (1 − P(make 2nd)) / N
	EStrokes   float64 // expected putts to hole out
	EStrokesSE float64

	// Miss geometry, measured along the error-free direction of travel at
	// the hole ("past" is beyond the hole on that axis).
	MeanPastMiss float64 // mean past-hole distance among misses that finished past, m
	PctMissShort float64 // fraction of misses that finished short of the hole
	MeanLeave    float64 // mean distance from hole among misses, m
}

// EvalCell solves the aim for one putt under a target-rollout pace policy and
// runs n error-perturbed trials. Follow-up putts are scored with the
// expected-strokes field.
func EvalCell(env *physics.Env, ball physics.Vec2, skill player.Skill,
	rollout float64, field *Field, n int, seed uint64) CellResult {

	aim, ok := player.Solve(env, ball, rollout)
	if !ok {
		return CellResult{}
	}

	// Error-free reference roll: direction of travel at the hole defines the
	// short/past axis for miss geometry.
	refVel := physics.Vec2{X: math.Cos(aim.Dir), Y: math.Sin(aim.Dir)}.Scale(aim.Speed)
	ref := env.Roll(ball, refVel, false)
	axis := ref.ClosestVel
	if axis.Norm() < 1e-9 {
		axis = refVel
	}
	axis = axis.Scale(1 / axis.Norm())

	dist := ball.Sub(env.HolePos).Norm() + rollout
	rng := rand.New(rand.NewPCG(seed, 0xce11))

	var (
		makes                  int
		sumStrokes, sumSq      float64
		sumMissNext            float64 // Σ (1 − P(make next)) over misses
		nPast, nShort          int
		sumPast, sumLeave      float64
	)
	for t := 0; t < n; t++ {
		dir, speed := skill.Perturb(aim, dist, rng)
		vel := physics.Vec2{X: math.Cos(dir), Y: math.Sin(dir)}.Scale(speed)
		out := env.Roll(ball, vel, true)

		var strokes float64
		switch {
		case out.Holed:
			makes++
			strokes = 1
		case out.Runaway:
			strokes = 4
		default:
			strokes = 1 + field.EStrokes(out.Rest, env.HolePos)
			sumMissNext += 1 - field.PMake(out.Rest, env.HolePos)
			dp := out.Rest.Sub(env.HolePos).Dot(axis)
			if dp > 0 {
				nPast++
				sumPast += dp
			} else {
				nShort++
			}
			sumLeave += out.Rest.Sub(env.HolePos).Norm()
		}
		sumStrokes += strokes
		sumSq += strokes * strokes
	}

	nf := float64(n)
	res := CellResult{
		SolveOK:   true,
		Make:      float64(makes) / nf,
		ThreePlus: sumMissNext / nf,
		EStrokes:  sumStrokes / nf,
	}
	res.MakeSE = math.Sqrt(res.Make * (1 - res.Make) / nf)
	varS := sumSq/nf - res.EStrokes*res.EStrokes
	res.EStrokesSE = math.Sqrt(math.Max(varS, 0) / nf)
	if miss := n - makes; miss > 0 {
		res.PctMissShort = float64(nShort) / float64(miss)
		res.MeanLeave = sumLeave / float64(miss)
	}
	if nPast > 0 {
		res.MeanPastMiss = sumPast / float64(nPast)
	}
	return res
}
