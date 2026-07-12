package sim

import (
	"math"
	"sync/atomic"
	"testing"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
)

// A skill with zero error must reproduce the error-free putt every trial:
// certain make, exactly one stroke, zero variance.
func TestEvalCellZeroError(t *testing.T) {
	env := physics.NewEnv(green.NewPlanar(0, physics.DecelFromStimp(10)), physics.PennerVC0)
	perfect := player.Skill{Name: "perfect"}
	f := handField()

	res := EvalCell(env, physics.Vec2{X: -3}, perfect, 0.3, f, 200, 42)
	if !res.SolveOK {
		t.Fatal("solve failed")
	}
	if res.Make != 1 || res.EStrokes != 1 || res.MakeSE != 0 || res.EStrokesSE != 0 {
		t.Errorf("zero-error putt: make=%g E=%g makeSE=%g eSE=%g, want 1/1/0/0",
			res.Make, res.EStrokes, res.MakeSE, res.EStrokesSE)
	}
	if res.ThreePlus != 0 {
		t.Errorf("zero-error 3-putt probability = %g, want 0", res.ThreePlus)
	}
}

func TestEvalCellStrokesAccounting(t *testing.T) {
	env := physics.NewEnv(green.NewPlanar(2, physics.DecelFromStimp(10)), physics.PennerVC0)
	sk := testSkill(t, "high")
	f := handField()
	const n = 400

	res := EvalCell(env, physics.Vec2{Y: 4.5}, sk, 0.3, f, n, 7)
	if !res.SolveOK {
		t.Fatal("solve failed")
	}
	if len(res.Strokes) != n {
		t.Fatalf("len(Strokes) = %d, want %d", len(res.Strokes), n)
	}
	var sum float64
	ones := 0
	for _, s := range res.Strokes {
		sum += s
		if s == 1 {
			ones++
		}
	}
	if got := sum / n; math.Abs(got-res.EStrokes) > 1e-12 {
		t.Errorf("mean(Strokes) = %.6f, EStrokes = %.6f", got, res.EStrokes)
	}
	if got := float64(ones) / n; math.Abs(got-res.Make) > 1e-12 {
		t.Errorf("share of 1-stroke trials = %.4f, Make = %.4f", got, res.Make)
	}
}

// Same seed must reproduce the cell exactly; a different seed must not.
func TestEvalCellDeterminism(t *testing.T) {
	env := physics.NewEnv(green.NewPlanar(3, physics.DecelFromStimp(10)), physics.PennerVC0)
	sk := testSkill(t, "mid")
	f := handField()
	ball := physics.Vec2{X: -4.5}

	a := EvalCell(env, ball, sk, 0.4, f, 300, 11)
	b := EvalCell(env, ball, sk, 0.4, f, 300, 11)
	if a.Make != b.Make || a.EStrokes != b.EStrokes || a.MeanLeave != b.MeanLeave {
		t.Errorf("same seed differs: %+v vs %+v", a, b)
	}
	c := EvalCell(env, ball, sk, 0.4, f, 300, 12)
	if a.Make == c.Make && a.EStrokes == c.EStrokes && a.MeanLeave == c.MeanLeave {
		t.Error("different seed produced identical statistics")
	}
}

// EvalCellRecord with a field must reproduce EvalCell exactly and return one
// terminal state per trial, consistent with the aggregates.
func TestEvalCellRecordMatchesEvalCell(t *testing.T) {
	env := physics.NewEnv(green.NewPlanar(3, physics.DecelFromStimp(10)), physics.PennerVC0)
	sk := testSkill(t, "high")
	f := handField()
	ball := physics.Vec2{Y: 4.5}
	const n = 300

	plain := EvalCell(env, ball, sk, 0.3, f, n, 9)
	rec, outs := EvalCellRecord(env, ball, sk, 0.3, f, n, 9)
	if plain.Make != rec.Make || plain.EStrokes != rec.EStrokes ||
		plain.ThreePlus != rec.ThreePlus || plain.MeanLeave != rec.MeanLeave {
		t.Errorf("recording changed the statistics: %+v vs %+v", plain, rec)
	}
	if len(outs) != n {
		t.Fatalf("len(outcomes) = %d, want %d", len(outs), n)
	}
	holed := 0
	for _, o := range outs {
		if o.Holed {
			holed++
			continue
		}
		if o.Runaway {
			continue
		}
		if math.IsNaN(o.Rest.X) || math.IsNaN(o.Rest.Y) {
			t.Fatal("miss rest position is NaN")
		}
	}
	if got := float64(holed) / n; got != rec.Make {
		t.Errorf("holed flags give make %.4f, aggregate says %.4f", got, rec.Make)
	}
}

// A nil field must not change the trial sequence — all randomness lives in
// Perturb — only the stroke scoring.
func TestEvalCellRecordNilField(t *testing.T) {
	env := physics.NewEnv(green.NewPlanar(2, physics.DecelFromStimp(10)), physics.PennerVC0)
	sk := testSkill(t, "mid")
	ball := physics.Vec2{X: -4.5}
	const n = 300

	withField, _ := EvalCellRecord(env, ball, sk, 0.4, handField(), n, 13)
	noField, outs := EvalCellRecord(env, ball, sk, 0.4, nil, n, 13)
	if withField.Make != noField.Make || withField.MeanLeave != noField.MeanLeave ||
		withField.MeanPastMiss != noField.MeanPastMiss {
		t.Errorf("field changed the trials: make %.4f vs %.4f", withField.Make, noField.Make)
	}
	if noField.EStrokes != 0 || noField.ThreePlus != 0 || noField.Strokes != nil {
		t.Errorf("nil-field run scored strokes: E=%g 3p=%g strokes=%v",
			noField.EStrokes, noField.ThreePlus, noField.Strokes != nil)
	}
	if len(outs) != n {
		t.Errorf("len(outcomes) = %d, want %d", len(outs), n)
	}
}

// On a flat green putting from -X the travel axis at the hole is +X.
func TestEvalCellAxis(t *testing.T) {
	env := physics.NewEnv(green.NewPlanar(0, physics.DecelFromStimp(10)), physics.PennerVC0)
	res := EvalCell(env, physics.Vec2{X: -3}, testSkill(t, "tour"), 0.3, handField(), 50, 5)
	if math.Abs(res.Axis.X-1) > 1e-6 || math.Abs(res.Axis.Y) > 1e-6 {
		t.Errorf("axis = %+v, want (1, 0)", res.Axis)
	}
}

func TestParallelDoCoversAllIndices(t *testing.T) {
	for _, n := range []int{0, 1, 3, 100} {
		hits := make([]int32, n)
		ParallelDo(n, func(i int) { atomic.AddInt32(&hits[i], 1) })
		for i, h := range hits {
			if h != 1 {
				t.Errorf("n=%d: index %d executed %d times", n, i, h)
			}
		}
	}
}
