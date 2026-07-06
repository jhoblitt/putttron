// Package player models human execution error and solves the aim (launch
// direction and speed) for a putt under a pace policy.
package player

import (
	"math"
	"math/rand/v2"

	"github.com/jhoblitt/putttron/internal/physics"
)

// Skill is an execution-error model. Numbers and their provenance live in
// docs/literature.md; sigmas here are TOTAL outcome dispersion (they include
// green-reading error, since they are calibrated against observed make/leave
// data, not stroke mechanics alone).
type Skill struct {
	Name        string
	DirSigmaDeg float64 // std dev of launch-line error, degrees
	// Distance-control error: std dev of realized roll distance as a
	// fraction of intended distance, with an absolute floor (even tap-ins
	// carry some absolute error).
	DistSigmaPct   float64 // e.g. 0.06 = 6% of putt length
	DistSigmaFloor float64 // meters
}

// Perturb applies sampled execution error to a solved aim. Following Bansal
// & Broadie (2008) / Broadie & Shin (2014), the distance-control error is a
// Gaussian fractional error on v² (roll distance ∝ v² on a uniform green,
// so a % error on v² is a % error on rolled length); direction error is a
// Gaussian launch-angle error.
func (s Skill) Perturb(aim Aim, intendedDist float64, rng *rand.Rand) (dir, speed float64) {
	dir = aim.Dir + rng.NormFloat64()*s.DirSigmaDeg*math.Pi/180
	frac := math.Max(s.DistSigmaPct, s.DistSigmaFloor/math.Max(intendedDist, 1e-6))
	v2 := aim.Speed * aim.Speed * (1 + rng.NormFloat64()*frac)
	return dir, math.Sqrt(math.Max(v2, 1e-4))
}

type Aim struct {
	Dir   float64 // launch direction, radians
	Speed float64 // launch speed, m/s
}

// Solve finds the aim from ball position (hole at env.HolePos) such that the
// error-free ball rolls through the hole center and stops rollout meters
// past it (hole treated as filled). Damped 2-D Newton on the residual
// (lateral miss at closest approach, path-length error) — direction and
// speed are strongly coupled on breaking putts, so per-axis fixed-point
// updates limit-cycle. Returns ok=false when no such putt exists (e.g.
// downhill runaway) or the iteration fails to converge.
func Solve(env *physics.Env, ball physics.Vec2, rollout float64) (Aim, bool) {
	chord := env.HolePos.Sub(ball)
	dist := chord.Norm()
	if dist < 1e-6 {
		return Aim{}, false
	}
	aim := Aim{
		Dir:   math.Atan2(chord.Y, chord.X),
		Speed: initialSpeed(env, ball, dist+rollout),
	}

	const (
		latTol  = 0.002 // m
		lenTol  = 0.008 // m
		maxIter = 60
	)

	// residual: (lateral miss, wanted-minus-actual path length). ok=false
	// on runaway.
	residual := func(a Aim) (lat, lenErr float64, ok bool) {
		vel := physics.Vec2{X: math.Cos(a.Dir), Y: math.Sin(a.Dir)}.Scale(a.Speed)
		out := env.Roll(ball, vel, false)
		if out.Runaway {
			return 0, 0, false
		}
		tdir := out.ClosestVel
		if tdir.Norm() < 1e-9 {
			tdir = vel
		}
		tn := tdir.Scale(1 / tdir.Norm())
		lat = tn.Cross(env.HolePos.Sub(out.ClosestPos)) // + means hole is left of travel

		// "Stopped short" = the closest approach to the hole is the END of
		// the trajectory, so the remaining distance still has to be covered.
		// Otherwise the ball passed its closest approach and the target
		// length is path-to-closest-approach plus rollout.
		var wantLen float64
		if out.PathLen-out.PathLenAtMin < 0.02 {
			wantLen = out.PathLen + out.Rest.Sub(env.HolePos).Norm() + rollout
		} else {
			wantLen = out.PathLenAtMin + rollout
		}
		return lat, wantLen - out.PathLen, true
	}

	f1, f2, ok := residual(aim)
	for i := 0; i < maxIter; i++ {
		if !ok {
			aim.Speed *= 0.85 // runaway: back off and re-evaluate
			f1, f2, ok = residual(aim)
			continue
		}
		if math.Abs(f1) < latTol && math.Abs(f2) < lenTol {
			return aim, true
		}

		const dDir, relSpeed = 2e-3, 5e-3
		g1, g2, gok := residual(Aim{Dir: aim.Dir + dDir, Speed: aim.Speed})
		dSpeed := relSpeed * aim.Speed
		h1, h2, hok := residual(Aim{Dir: aim.Dir, Speed: aim.Speed + dSpeed})
		if !gok || !hok {
			aim.Speed *= 0.9
			f1, f2, ok = residual(aim)
			continue
		}
		j11, j12 := (g1-f1)/dDir, (h1-f1)/dSpeed
		j21, j22 := (g2-f2)/dDir, (h2-f2)/dSpeed
		det := j11*j22 - j12*j21
		var stepDir, stepSpeed float64
		if math.Abs(det) < 1e-12 {
			// Singular Jacobian: fall back to decoupled damped updates.
			stepDir = 0.5 * math.Atan2(f1, dist)
			stepSpeed = 0.25 * f2 / math.Max(dist+rollout, 1e-6) * aim.Speed
		} else {
			stepDir = -(j22*f1 - j12*f2) / det
			stepSpeed = -(-j21*f1 + j11*f2) / det
		}
		stepDir = math.Max(-0.3, math.Min(0.3, stepDir))
		maxDs := 0.3 * aim.Speed
		stepSpeed = math.Max(-maxDs, math.Min(maxDs, stepSpeed))

		// Backtracking: accept the first step that reduces the residual norm.
		norm0 := math.Hypot(f1, f2)
		scale := 1.0
		for k := 0; k < 5; k++ {
			cand := Aim{Dir: aim.Dir + scale*stepDir, Speed: math.Max(aim.Speed+scale*stepSpeed, 0.05)}
			c1, c2, cok := residual(cand)
			if cok && math.Hypot(c1, c2) < norm0 {
				aim, f1, f2, ok = cand, c1, c2, true
				break
			}
			scale /= 2
			if k == 4 {
				aim = Aim{Dir: aim.Dir + scale*stepDir, Speed: math.Max(aim.Speed+scale*stepSpeed, 0.05)}
				f1, f2, ok = residual(aim)
			}
		}
	}
	return aim, false
}

// initialSpeed is a first guess from the work-energy balance along the
// chord: v² = 2·(a_d + (5/7)·g·s̄)·L with s̄ the mean uphill grade.
func initialSpeed(env *physics.Env, ball physics.Vec2, length float64) float64 {
	target := env.HolePos
	dz := env.Surf.Elevation(target.X, target.Y) - env.Surf.Elevation(ball.X, ball.Y)
	ad := env.Surf.DecelCoeff(ball.X, ball.Y, 1, 0)
	v2 := 2 * (ad*length + (5.0/7.0)*physics.G*dz)
	if v2 < 0.01 {
		v2 = 0.01
	}
	return math.Sqrt(v2)
}
