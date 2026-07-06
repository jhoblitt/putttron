package physics

import (
	"math"
	"testing"

	"github.com/jhoblitt/putttron/internal/green"
)

func flatEnv(stimp float64) *Env {
	return NewEnv(green.NewPlanar(0, DecelFromStimp(stimp)), 1.63)
}

// A ball released at Stimpmeter speed on a flat Stimp-10 green must roll
// 10 feet.
func TestStimpCalibration(t *testing.T) {
	e := flatEnv(10)
	e.HolePos = Vec2{X: 100} // out of the way
	out := e.Roll(Vec2{}, Vec2{X: StimpSpeed}, false)
	want := 10 * 0.3048
	if math.Abs(out.Rest.X-want) > 0.01 {
		t.Errorf("stimp-10 roll = %.4f m, want %.4f ± 0.01", out.Rest.X, want)
	}
	if math.Abs(out.Rest.Y) > 1e-9 {
		t.Errorf("flat straight roll drifted: y = %g", out.Rest.Y)
	}
}

// Stopping distance on a slope must match d = v0² / (2·(a_d·cosθ ± (5/7)·g·sinθ)).
func TestSlopeStoppingDistance(t *testing.T) {
	const slopePct, stimp, v0 = 3.0, 10.0, 2.0
	ad := DecelFromStimp(stimp)
	th := math.Atan(slopePct / 100)

	for _, tc := range []struct {
		name string
		dir  float64 // +1 rolls downhill (+X), -1 uphill
	}{
		{"uphill", -1},
		{"downhill", +1},
	} {
		e := NewEnv(green.NewPlanar(slopePct, ad), 1.63)
		e.HolePos = Vec2{X: 100}
		out := e.Roll(Vec2{}, Vec2{X: tc.dir * v0}, false)
		decel := ad*math.Cos(th) - tc.dir*(5.0/7.0)*G*math.Sin(th)
		want := v0 * v0 / (2 * decel)
		got := math.Abs(out.Rest.X)
		if math.Abs(got-want) > 0.02 {
			t.Errorf("%s: rolled %.4f m, want %.4f", tc.name, got, want)
		}
	}
}

func TestCaptureThreshold(t *testing.T) {
	e := flatEnv(10)
	e.HolePos = Vec2{X: 2}
	ad := DecelFromStimp(10)

	// Launch speed that arrives at the hole with speed vAtHole.
	launch := func(vAtHole float64) float64 {
		return math.Sqrt(vAtHole*vAtHole + 2*ad*2)
	}
	below := e.Roll(Vec2{}, Vec2{X: launch(e.Capture.VC0 * 0.95)}, true)
	if !below.Holed {
		t.Errorf("center hit below capture speed not holed (enter speed %.3f)", below.EnterSpeed)
	}
	above := e.Roll(Vec2{}, Vec2{X: launch(e.Capture.VC0 * 1.05)}, true)
	if above.Holed {
		t.Errorf("center hit above capture speed was holed (enter speed %.3f)", above.EnterSpeed)
	}
}

// A firm putt offset from the center line must have a smaller capture window
// than a dead-center one.
func TestOffsetCapture(t *testing.T) {
	e := flatEnv(10)
	e.HolePos = Vec2{X: 2}
	ad := DecelFromStimp(10)
	v := math.Sqrt(math.Pow(0.8*e.Capture.VC0, 2) + 2*ad*2)

	center := e.Roll(Vec2{}, Vec2{X: v}, true)
	if !center.Holed {
		t.Fatal("center hit at 0.8·VC0 not holed")
	}
	edge := e.Roll(Vec2{Y: 0.045}, Vec2{X: v}, true)
	if edge.Holed {
		t.Errorf("edge hit (b=45mm) at 0.8·VC0 was holed")
	}
}

// A ball trickling in dead weight falls even at the edge; at rest over the
// cup it always falls.
func TestDyingBallCapture(t *testing.T) {
	e := flatEnv(10)
	e.HolePos = Vec2{X: 2}
	ad := DecelFromStimp(10)
	v := math.Sqrt(2*ad*2 + 0.01) // stops ~1 cm past hole center
	out := e.Roll(Vec2{}, Vec2{X: v}, true)
	if !out.Holed {
		t.Errorf("dying center ball not holed (min dist %.4f)", out.MinHoleDist)
	}
}

func TestRestOnSlope(t *testing.T) {
	// Stimp 10: μ_r = 7·a_d/(5g) ≈ 0.078 → holds up to ~7.8% grade;
	// 5% holds, 10% rolls away.
	ad := DecelFromStimp(10)
	hold := NewEnv(green.NewPlanar(5, ad), 1.63)
	hold.HolePos = Vec2{X: 100}
	if out := hold.Roll(Vec2{}, Vec2{X: -0.01}, false); out.Runaway || out.Rest.Norm() > 0.1 {
		t.Errorf("ball on 5%%/stimp-10 should stop near start: %+v", out)
	}
	steep := NewEnv(green.NewPlanar(10, ad), 1.63)
	steep.HolePos = Vec2{X: 100}
	if out := steep.Roll(Vec2{}, Vec2{X: 0.01}, false); !out.Runaway {
		t.Errorf("ball on 10%%/stimp-10 should run away, rested at %+v", out.Rest)
	}
}

// A cross-slope putt must break toward the downhill (+X) side.
func TestSidehillBreak(t *testing.T) {
	e := NewEnv(green.NewPlanar(3.5, DecelFromStimp(10)), 1.63)
	e.HolePos = Vec2{X: 100, Y: 100}
	out := e.Roll(Vec2{}, Vec2{Y: 2.0}, false)
	if out.Rest.X <= 0.05 {
		t.Errorf("cross-slope putt did not break downhill: rest %+v", out.Rest)
	}
	if out.Rest.Y <= 1.0 {
		t.Errorf("cross-slope putt barely advanced: rest %+v", out.Rest)
	}
}
