// Package physics integrates a rolling golf ball over a green.Surface and
// decides hole capture. Model: Penner, "The physics of putting", Can. J.
// Phys. 80 (2002); Holmes, "Putting: How a golf ball and hole interact",
// Am. J. Phys. 59 (1991). See docs/physics.md.
package physics

import (
	"math"

	"github.com/jhoblitt/putttron/internal/green"
)

const (
	G          = 9.80665
	BallRadius = 0.021335 // m (1.68 in diameter)
	HoleRadius = 0.054    // m (4.25 in diameter)
	StimpSpeed = 1.83     // m/s, ball release speed off a Stimpmeter

	// rollFactor is 1/(1+I/mr²) for a uniform sphere: translational
	// acceleration from an in-plane force on a rolling ball.
	rollFactor = 5.0 / 7.0
)

// DecelFromStimp converts a Stimpmeter reading (feet) to the flat-green
// rolling deceleration a_d (m/s²): the stimp ball is released at StimpSpeed
// and rolls stimpFeet.
func DecelFromStimp(stimpFeet float64) float64 {
	s := stimpFeet * 0.3048
	return StimpSpeed * StimpSpeed / (2 * s)
}

type Vec2 struct{ X, Y float64 }

func (a Vec2) Add(b Vec2) Vec2      { return Vec2{a.X + b.X, a.Y + b.Y} }
func (a Vec2) Sub(b Vec2) Vec2      { return Vec2{a.X - b.X, a.Y - b.Y} }
func (a Vec2) Scale(k float64) Vec2 { return Vec2{k * a.X, k * a.Y} }
func (a Vec2) Dot(b Vec2) float64   { return a.X*b.X + a.Y*b.Y }
func (a Vec2) Cross(b Vec2) float64 { return a.X*b.Y - a.Y*b.X }
func (a Vec2) Norm() float64        { return math.Hypot(a.X, a.Y) }

// Capture decides whether a ball crossing the hole disk falls in.
// Penner (2002) Eq. 23: the critical speed at impact parameter b
// (perpendicular offset of the path from the hole center) falls
// quadratically to zero at the hole edge: v_c(b) = VC0·(1 − (b/R)²),
// with VC0 = 1.63 m/s for all capture mechanisms on a level green.
// Penner Eq. 30 adds a slope correction: v_c scales by 1/sqrt(1 − s)
// with s the sine of the green slope along the travel direction
// (positive uphill), making uphill entries slightly easier.
type Capture struct {
	VC0 float64 // max capture speed for a dead-center hit on a level green, m/s
}

// PennerVC0 is the level-green center-hit critical capture speed including
// lip-roll and bounce-in mechanisms (Penner 2002, from Holmes 1991).
const PennerVC0 = 1.63

func (c Capture) VC(b, slopeAlong float64) float64 {
	if b >= HoleRadius {
		return 0
	}
	q := b / HoleRadius
	vc := c.VC0 * (1 - q*q)
	if slopeAlong > -0.99 {
		vc /= math.Sqrt(1 - math.Min(slopeAlong, 0.99))
	}
	return vc
}

// Env is an integration environment. Hole is at HolePos; capture is only
// tested when the Roll call asks for it (a "filled" hole is used by the aim
// solver).
type Env struct {
	Surf    green.Surface
	Capture Capture
	HolePos Vec2
	Dt      float64 // s
	MaxTime float64 // s
}

func NewEnv(surf green.Surface, vc0 float64) *Env {
	return &Env{
		Surf:    surf,
		Capture: Capture{VC0: vc0},
		Dt:      1e-3,
		MaxTime: 60,
	}
}

type Outcome struct {
	Holed   bool
	Runaway bool // never came to rest (fell off the model or slope too steep)
	Rest    Vec2 // valid if !Holed && !Runaway

	// Diagnostics of the pass nearest the hole: closest center-to-center
	// distance, position and velocity there, and path length from launch to
	// that point and to rest.
	MinHoleDist   float64
	ClosestPos    Vec2
	ClosestVel    Vec2
	PathLen       float64
	PathLenAtMin  float64
	EnterSpeed    float64 // speed when the ball last entered the hole disk (0 if never)
	EnteredDisk   bool
	RestInsideCup bool // came to rest with center over the hole (counted as Holed)
}

// accel returns the ball's horizontal-plane acceleration. Rolling model:
// a = (5/7)·g_par − a_d·cosθ·v̂, with g_par the in-plane gravity vector.
// At (numerically) zero speed the resistance opposes incipient downslope
// motion instead.
func (e *Env) accel(pos, vel Vec2) Vec2 {
	gx, gy := e.Surf.Gradient(pos.X, pos.Y)
	inv := 1 / math.Sqrt(1+gx*gx+gy*gy)
	gpar := Vec2{-G * gx * inv, -G * gy * inv}
	speed := vel.Norm()

	var dir Vec2
	if speed > 1e-9 {
		dir = vel.Scale(1 / speed)
	} else {
		gm := gpar.Norm()
		if gm < 1e-12 {
			return Vec2{}
		}
		dir = gpar.Scale(1 / gm)
	}
	ad := e.Surf.DecelCoeff(pos.X, pos.Y, dir.X, dir.Y) * inv
	drive := gpar.Scale(rollFactor)
	if speed <= 1e-9 {
		net := drive.Norm() - ad
		if net <= 0 {
			return Vec2{}
		}
		return dir.Scale(net)
	}
	return drive.Sub(dir.Scale(ad))
}

// atRest reports whether a stationary ball stays put: rolling resistance
// must hold the in-plane gravity component.
func (e *Env) atRest(pos Vec2) bool {
	gx, gy := e.Surf.Gradient(pos.X, pos.Y)
	inv := 1 / math.Sqrt(1+gx*gx+gy*gy)
	gpar := G * math.Hypot(gx, gy) * inv
	ad := e.Surf.DecelCoeff(pos.X, pos.Y, 1, 0) * inv
	return rollFactor*gpar <= ad
}

// Roll integrates from pos with initial velocity vel. If capture is true the
// hole can swallow the ball; otherwise it is treated as filled.
func (e *Env) Roll(pos, vel Vec2, capture bool) Outcome {
	out := Outcome{MinHoleDist: math.Inf(1)}
	dt := e.Dt
	inDisk := false
	lastMovingVel := vel

	for t := 0.0; t < e.MaxTime; t += dt {
		if vel.Norm() >= 0.05 {
			lastMovingVel = vel
		}
		d := pos.Sub(e.HolePos).Norm()
		if d < out.MinHoleDist {
			out.MinHoleDist = d
			out.ClosestPos = pos
			// As the ball dies its instantaneous direction degenerates;
			// report the arrival direction instead so callers get a stable
			// travel axis.
			out.ClosestVel = lastMovingVel
			out.PathLenAtMin = out.PathLen
		}
		speed := vel.Norm()

		if d < HoleRadius {
			if !inDisk {
				inDisk = true
				out.EnteredDisk = true
				out.EnterSpeed = speed
				if capture {
					vhat := vel.Scale(1 / math.Max(speed, 1e-12))
					b := math.Abs(pos.Sub(e.HolePos).Cross(vhat))
					gx, gy := e.Surf.Gradient(pos.X, pos.Y)
					// sine of slope along travel: dz/ds, positive uphill
					slopeAlong := (gx*vhat.X + gy*vhat.Y) / math.Sqrt(1+gx*gx+gy*gy)
					if speed <= e.Capture.VC(b, slopeAlong) {
						out.Holed = true
						return out
					}
				}
			}
		} else {
			inDisk = false
		}

		if speed < 1e-3 {
			if capture && d < HoleRadius {
				out.Holed = true
				out.RestInsideCup = true
				return out
			}
			if e.atRest(pos) {
				out.Rest = pos
				return out
			}
		}

		pos, vel = e.rk4(pos, vel, dt)
		out.PathLen += vel.Norm() * dt
	}
	out.Runaway = true
	return out
}

func (e *Env) rk4(pos, vel Vec2, dt float64) (Vec2, Vec2) {
	k1v := e.accel(pos, vel)
	k1x := vel

	k2v := e.accel(pos.Add(k1x.Scale(dt/2)), vel.Add(k1v.Scale(dt/2)))
	k2x := vel.Add(k1v.Scale(dt / 2))

	k3v := e.accel(pos.Add(k2x.Scale(dt/2)), vel.Add(k2v.Scale(dt/2)))
	k3x := vel.Add(k2v.Scale(dt / 2))

	k4v := e.accel(pos.Add(k3x.Scale(dt)), vel.Add(k3v.Scale(dt)))
	k4x := vel.Add(k3v.Scale(dt))

	pos = pos.Add(k1x.Add(k2x.Scale(2)).Add(k3x.Scale(2)).Add(k4x).Scale(dt / 6))
	vel = vel.Add(k1v.Add(k2v.Scale(2)).Add(k3v.Scale(2)).Add(k4v).Scale(dt / 6))
	return pos, vel
}
