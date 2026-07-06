// Package sim runs Monte Carlo putting trials: an expected-strokes field
// around the hole (value iteration over follow-up putts) and first-putt
// parameter sweeps.
package sim

import (
	"math"
	"math/rand/v2"
	"runtime"
	"sync"

	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
)

// Field holds expected strokes to hole out E(r,ψ) and one-putt probability
// P(r,ψ) on a polar grid centered on the hole (ψ measured from +X, the
// downhill direction on planar greens). Inside minR the putt is a tap-in
// (E=1); beyond maxR a linear penalty extrapolates.
type Field struct {
	Rs   []float64
	Psis []float64
	E    [][]float64 // [ri][pi]
	P    [][]float64
}

var defaultRs = []float64{0.10, 0.20, 0.30, 0.45, 0.60, 0.80, 1.00, 1.30, 1.70, 2.20, 2.80, 3.50, 4.50, 6.00}

const nPsi = 12

// tap-in radius: below this a putt is always holed.
const tapIn = 0.10

// lookup bilinearly interpolates grid at rest's polar position; beyond the
// outermost radius it clamps and adds extrapolPerMeter per meter. Callers
// handle the tap-in region.
func (f *Field) lookup(rest physics.Vec2, hole physics.Vec2, grid [][]float64, extrapolPerMeter float64) float64 {
	d := rest.Sub(hole)
	r := d.Norm()
	psi := math.Atan2(d.Y, d.X)
	if psi < 0 {
		psi += 2 * math.Pi
	}

	// psi cell (uniform, wrapping)
	dpsi := 2 * math.Pi / float64(len(f.Psis))
	pi0 := int(psi / dpsi)
	if pi0 >= len(f.Psis) {
		pi0 = len(f.Psis) - 1
	}
	pi1 := (pi0 + 1) % len(f.Psis)
	tp := (psi - float64(pi0)*dpsi) / dpsi

	// r cell (non-uniform)
	rs := f.Rs
	if r >= rs[len(rs)-1] {
		v := (1-tp)*grid[len(rs)-1][pi0] + tp*grid[len(rs)-1][pi1]
		return v + extrapolPerMeter*(r-rs[len(rs)-1])
	}
	ri0 := 0
	for ri0 < len(rs)-2 && rs[ri0+1] < r {
		ri0++
	}
	tr := (r - rs[ri0]) / (rs[ri0+1] - rs[ri0])
	if tr < 0 {
		tr = 0
	}
	v0 := (1-tp)*grid[ri0][pi0] + tp*grid[ri0][pi1]
	v1 := (1-tp)*grid[ri0+1][pi0] + tp*grid[ri0+1][pi1]
	return (1-tr)*v0 + tr*v1
}

// EStrokes returns expected strokes to hole out from rest.
func (f *Field) EStrokes(rest, hole physics.Vec2) float64 {
	if rest.Sub(hole).Norm() <= tapIn {
		return 1
	}
	return f.lookup(rest, hole, f.E, 0.15)
}

// PMake returns the probability of holing the next putt from rest.
func (f *Field) PMake(rest, hole physics.Vec2) float64 {
	if rest.Sub(hole).Norm() <= tapIn {
		return 1
	}
	p := f.lookup(rest, hole, f.P, 0)
	return math.Max(0, math.Min(1, p))
}

type FieldOpts struct {
	LagRollout float64 // pace policy for follow-up putts, m past the hole
	Trials     int     // per node per sweep
	Sweeps     int
	Seed       uint64
}

func DefaultFieldOpts() FieldOpts {
	return FieldOpts{LagRollout: 0.25, Trials: 1200, Sweeps: 5, Seed: 1}
}

// BuildField runs value iteration: E(node) = mean over trials of
// 1 + (holed ? 0 : E(rest)), with every putt played under the same skill and
// lag pace policy. Converges in a few sweeps since P(make) > 0 everywhere.
func BuildField(env *physics.Env, skill player.Skill, o FieldOpts) *Field {
	f := &Field{Rs: defaultRs, Psis: make([]float64, nPsi)}
	for i := range f.Psis {
		f.Psis[i] = 2 * math.Pi / float64(nPsi) * float64(i)
	}
	f.E = make([][]float64, len(f.Rs))
	f.P = make([][]float64, len(f.Rs))
	for i := range f.E {
		f.E[i] = make([]float64, nPsi)
		f.P[i] = make([]float64, nPsi)
		for j := range f.E[i] {
			f.E[i][j] = 1 + 0.4*f.Rs[i] // rough seed; iteration overwrites
			f.P[i][j] = 0.5
		}
	}

	type node struct{ ri, pi int }
	nodes := make([]node, 0, len(f.Rs)*nPsi)
	for ri := range f.Rs {
		for pi := 0; pi < nPsi; pi++ {
			nodes = append(nodes, node{ri, pi})
		}
	}

	aims := make([]player.Aim, len(nodes))
	aimOK := make([]bool, len(nodes))
	ballPos := func(n node) physics.Vec2 {
		return env.HolePos.Add(physics.Vec2{
			X: f.Rs[n.ri] * math.Cos(f.Psis[n.pi]),
			Y: f.Rs[n.ri] * math.Sin(f.Psis[n.pi]),
		})
	}

	ParallelDo(len(nodes), func(i int) {
		aims[i], aimOK[i] = player.Solve(env, ballPos(nodes[i]), o.LagRollout)
	})

	for sweep := 0; sweep < o.Sweeps; sweep++ {
		newE := make([]float64, len(nodes))
		newP := make([]float64, len(nodes))
		ParallelDo(len(nodes), func(i int) {
			n := nodes[i]
			if !aimOK[i] {
				newE[i] = 2 + f.Rs[n.ri]/2 // unsolvable node: pessimistic filler
				newP[i] = 0
				return
			}
			rng := rand.New(rand.NewPCG(o.Seed, uint64(sweep)<<32|uint64(i)))
			ball := ballPos(n)
			dist := f.Rs[n.ri] + o.LagRollout
			var sumE float64
			makes := 0
			for t := 0; t < o.Trials; t++ {
				dir, speed := skill.Perturb(aims[i], dist, rng)
				vel := physics.Vec2{X: math.Cos(dir), Y: math.Sin(dir)}.Scale(speed)
				out := env.Roll(ball, vel, true)
				switch {
				case out.Holed:
					sumE += 1
					makes++
				case out.Runaway:
					sumE += 1 + 3 // shouldn't happen on Phase-1 configs; pessimistic
				default:
					sumE += 1 + f.EStrokes(out.Rest, env.HolePos)
				}
			}
			newE[i] = sumE / float64(o.Trials)
			newP[i] = float64(makes) / float64(o.Trials)
		})
		for i, n := range nodes {
			f.E[n.ri][n.pi] = newE[i]
			f.P[n.ri][n.pi] = newP[i]
		}
	}
	return f
}

func ParallelDo(n int, fn func(i int)) {
	workers := runtime.GOMAXPROCS(0)
	if workers > n {
		workers = n
	}
	var wg sync.WaitGroup
	ch := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
}
