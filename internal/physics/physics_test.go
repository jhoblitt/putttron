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

// planeHeightmap samples the same tilted plane Planar defines (+X downhill)
// onto a grid big enough for multi-meter rolls.
func planeHeightmap(t *testing.T, slopePct, decel float64) *green.Heightmap {
	t.Helper()
	const rows, cols, dx = 88, 88, 0.25
	x0, y0 := -11.0, 11.0
	z := make([]float64, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			x := x0 + float64(j)*dx
			z[i*cols+j] = -slopePct / 100 * x
		}
	}
	h, err := green.NewHeightmap(z, rows, cols, x0, y0, dx, decel)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// A heightmap sampled from a plane must roll identically to Planar — the
// end-to-end check on interpolation + gradient + integration.
func TestHeightmapPlanarRollEquivalence(t *testing.T) {
	const slopePct, stimp = 3.0, 10.0
	ad := DecelFromStimp(stimp)
	planar := NewEnv(green.NewPlanar(slopePct, ad), 1.63)
	planar.HolePos = Vec2{X: 100}
	hm := NewEnv(planeHeightmap(t, slopePct, ad), 1.63)
	hm.HolePos = Vec2{X: 100}

	for _, tc := range []struct {
		name string
		vel  Vec2
	}{
		{"uphill", Vec2{X: -2.0}},
		{"downhill", Vec2{X: 1.5}},
		{"sidehill", Vec2{Y: 2.0}},
	} {
		a := planar.Roll(Vec2{}, tc.vel, false)
		b := hm.Roll(Vec2{}, tc.vel, false)
		if a.Runaway != b.Runaway {
			t.Fatalf("%s: runaway mismatch", tc.name)
		}
		if d := a.Rest.Sub(b.Rest).Norm(); d > 0.001 {
			t.Errorf("%s: rest differs by %.4f m (planar %+v, heightmap %+v)",
				tc.name, d, a.Rest, b.Rest)
		}
	}
}

func BenchmarkHeightmapRoll(b *testing.B) {
	const rows, cols, dx = 88, 88, 0.25
	x0, y0 := -11.0, 11.0
	z := make([]float64, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			z[i*cols+j] = -0.02 * (x0 + float64(j)*dx)
		}
	}
	h, err := green.NewHeightmap(z, rows, cols, x0, y0, dx, DecelFromStimp(10))
	if err != nil {
		b.Fatal(err)
	}
	env := NewEnv(h, 1.63)
	env.HolePos = Vec2{X: 100}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env.Roll(Vec2{}, Vec2{X: 2.0}, false)
	}
}

// boundedFlatHeightmap is a flat green valid only for x <= edgeX.
func boundedFlatHeightmap(t *testing.T, edgeX, decel float64) *green.Heightmap {
	t.Helper()
	const rows, cols, dx = 60, 60, 0.25
	x0, y0 := -7.0, 7.0
	z := make([]float64, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			if x0+float64(j)*dx > edgeX {
				z[i*cols+j] = math.NaN()
			}
		}
	}
	h, err := green.NewHeightmap(z, rows, cols, x0, y0, dx, decel)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// A ball rolling over the mask boundary ends the putt at the exit point.
func TestRollOffGreenExit(t *testing.T) {
	const edgeX = 1.0
	e := NewEnv(boundedFlatHeightmap(t, edgeX, DecelFromStimp(10)), 1.63)
	e.HolePos = Vec2{X: 100}

	out := e.Roll(Vec2{}, Vec2{X: 2.0}, false)
	if !out.OffGreen || out.Holed || out.Runaway {
		t.Fatalf("fast ball should exit the green: %+v", out)
	}
	// OnGreen's bilinear-mask boundary sits half a cell past the last valid
	// node column; the exit sample lands within a step of it.
	if out.Rest.X < edgeX || out.Rest.X > edgeX+0.13 {
		t.Errorf("exit point x = %.4f, want just past %.2f", out.Rest.X, edgeX)
	}
	if math.Abs(out.Rest.Y) > 1e-9 {
		t.Errorf("straight roll drifted: y = %g", out.Rest.Y)
	}

	slow := e.Roll(Vec2{}, Vec2{X: 0.5}, false)
	if slow.OffGreen || slow.Runaway {
		t.Errorf("gentle ball should stop on the green: %+v", slow)
	}

	start := Vec2{X: edgeX + 1}
	off := e.Roll(start, Vec2{X: 1}, false)
	if !off.OffGreen || off.Rest != start || off.PathLen != 0 {
		t.Errorf("ball starting off-green should exit immediately: %+v", off)
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
