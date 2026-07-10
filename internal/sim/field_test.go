package sim

import (
	"math"
	"testing"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
)

// handField builds a 3-radius × 4-angle field with E = base + r + ψ/(2π) so
// every interpolation check has a closed-form expectation.
func handField() *Field {
	f := &Field{
		Rs:   []float64{0.5, 1.0, 2.0},
		Psis: []float64{0, math.Pi / 2, math.Pi, 3 * math.Pi / 2},
	}
	f.E = make([][]float64, len(f.Rs))
	f.P = make([][]float64, len(f.Rs))
	for ri, r := range f.Rs {
		f.E[ri] = make([]float64, len(f.Psis))
		f.P[ri] = make([]float64, len(f.Psis))
		for pi := range f.Psis {
			f.E[ri][pi] = 1 + r + float64(pi)*0.1
			f.P[ri][pi] = 0.9 - 0.2*r
		}
	}
	return f
}

func TestFieldLookupAtNodes(t *testing.T) {
	f := handField()
	hole := physics.Vec2{}
	for ri, r := range f.Rs {
		for pi, psi := range f.Psis {
			rest := physics.Vec2{X: r * math.Cos(psi), Y: r * math.Sin(psi)}
			got := f.lookup(rest, hole, f.E, 0)
			if math.Abs(got-f.E[ri][pi]) > 1e-9 {
				t.Errorf("node (r=%g ψ=%g): lookup %.6f, want %.6f", r, psi, got, f.E[ri][pi])
			}
		}
	}
}

func TestFieldLookupInterpolates(t *testing.T) {
	f := handField()
	hole := physics.Vec2{}

	// Radial midpoint between r=0.5 and r=1.0 at ψ=0.
	got := f.lookup(physics.Vec2{X: 0.75}, hole, f.E, 0)
	want := (f.E[0][0] + f.E[1][0]) / 2
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("radial midpoint: %.6f, want %.6f", got, want)
	}

	// Angular midpoint between ψ=0 and ψ=π/2 at r=1.
	got = f.lookup(physics.Vec2{X: math.Cos(math.Pi / 4), Y: math.Sin(math.Pi / 4)}, hole, f.E, 0)
	want = (f.E[1][0] + f.E[1][1]) / 2
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("angular midpoint: %.6f, want %.6f", got, want)
	}
}

// The angle axis wraps: a point between the last grid angle (3π/2) and 0 must
// interpolate between the last and first columns, not clamp or read garbage.
func TestFieldLookupAngleWraparound(t *testing.T) {
	f := handField()
	hole := physics.Vec2{}
	psi := 7 * math.Pi / 4 // midway between 3π/2 and 2π
	got := f.lookup(physics.Vec2{X: math.Cos(psi), Y: math.Sin(psi)}, hole, f.E, 0)
	want := (f.E[1][3] + f.E[1][0]) / 2
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("wraparound at ψ=7π/4: %.6f, want %.6f", got, want)
	}
}

func TestFieldLookupExtrapolatesBeyondGrid(t *testing.T) {
	f := handField()
	hole := physics.Vec2{}
	got := f.lookup(physics.Vec2{X: 3.0}, hole, f.E, 0.15)
	want := f.E[2][0] + 0.15*(3.0-2.0)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("extrapolation at r=3: %.6f, want %.6f", got, want)
	}
}

func TestFieldTapInAndPMakeClamp(t *testing.T) {
	f := handField()
	hole := physics.Vec2{X: 5, Y: -2}
	if e := f.EStrokes(physics.Vec2{X: 5.05, Y: -2}, hole); e != 1 {
		t.Errorf("tap-in EStrokes = %g, want 1", e)
	}
	if p := f.PMake(physics.Vec2{X: 5.05, Y: -2}, hole); p != 1 {
		t.Errorf("tap-in PMake = %g, want 1", p)
	}
	// Force an out-of-range grid value and confirm the clamp.
	for pi := range f.P[2] {
		f.P[2][pi] = 1.7
	}
	if p := f.PMake(physics.Vec2{X: 6.5, Y: -2}, hole); p != 1 {
		t.Errorf("PMake not clamped to 1: %g", p)
	}
}

// A built field must be physically sensible: E ≥ 1 everywhere, near-certain
// one-putt at the innermost radius, and expected strokes non-decreasing with
// distance along every angle (up to Monte Carlo slack).
func TestBuildFieldSanity(t *testing.T) {
	env := physics.NewEnv(green.NewPlanar(2, physics.DecelFromStimp(10)), physics.PennerVC0)
	sk := testSkill(t, "tour")
	f := BuildField(env, sk, FieldOpts{LagRollout: 0.25, Trials: 300, Sweeps: 3, Seed: 7})

	for ri := range f.Rs {
		for pi := range f.Psis {
			if f.E[ri][pi] < 1 {
				t.Errorf("E[%d][%d] = %.3f < 1", ri, pi, f.E[ri][pi])
			}
			if f.P[ri][pi] < 0 || f.P[ri][pi] > 1 {
				t.Errorf("P[%d][%d] = %.3f outside [0,1]", ri, pi, f.P[ri][pi])
			}
		}
	}
	for pi := range f.Psis {
		if f.P[0][pi] < 0.95 {
			t.Errorf("P at r=%.2g ψ#%d = %.3f, want ≥ 0.95 (tour from 10 cm)", f.Rs[0], pi, f.P[0][pi])
		}
		for ri := 1; ri < len(f.Rs); ri++ {
			if f.E[ri][pi] < f.E[ri-1][pi]-0.08 {
				t.Errorf("E not increasing with r at ψ#%d: E(%.2g)=%.3f > E(%.2g)=%.3f",
					pi, f.Rs[ri-1], f.E[ri-1][pi], f.Rs[ri], f.E[ri][pi])
			}
		}
	}
}

func testSkill(t *testing.T, name string) player.Skill {
	t.Helper()
	s, ok := player.ProfileByName(name)
	if !ok {
		t.Fatalf("no profile %q", name)
	}
	return s
}
