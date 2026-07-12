package sim

import (
	"math"
	"testing"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
)

// boundedFlatGreen is flat and valid only for x <= edgeX.
func boundedFlatGreen(t *testing.T, edgeX float64) *green.Heightmap {
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
	h, err := green.NewHeightmap(z, rows, cols, x0, y0, dx, physics.DecelFromStimp(10))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Off-green trials must be charged 1 + E(exit) + OffPenalty, count as
// 3-putt-or-worse, and stay out of the leave geometry. The whole stroke
// accounting is recomputed from the recorded outcomes and must match.
func TestEvalCellOffGreenAccounting(t *testing.T) {
	env := physics.NewEnv(boundedFlatGreen(t, 1.0), physics.PennerVC0)
	f := handField()
	f.OffPenalty = 0.5
	// Heavy distance error so a fat tail of trials runs off the east edge.
	sk := player.Skill{Name: "wild", DirSigmaDeg: 0.5, DistSigmaPct: 0.30, DistSigmaFloor: 0.02}
	const n = 500

	res, outs := EvalCellRecord(env, physics.Vec2{X: -3}, sk, 0.3, f, n, 21)
	if !res.SolveOK {
		t.Fatal("solve failed")
	}
	if res.OffGreen == 0 {
		t.Fatal("expected some trials to leave the green")
	}

	var sumStrokes, sumMissNext, sumLeave float64
	var nOff, nShortOn, nMissOn int
	for _, o := range outs {
		switch {
		case o.Holed:
			sumStrokes++
		case o.Runaway:
			sumStrokes += 4
		case o.OffGreen:
			nOff++
			sumStrokes += 1 + f.EStrokes(o.Rest, env.HolePos) + f.OffPenalty
			sumMissNext++
		default:
			nMissOn++
			sumStrokes += 1 + f.EStrokes(o.Rest, env.HolePos)
			sumMissNext += 1 - f.PMake(o.Rest, env.HolePos)
			if o.Rest.Sub(env.HolePos).Dot(res.Axis) <= 0 {
				nShortOn++
			}
			sumLeave += o.Rest.Sub(env.HolePos).Norm()
		}
	}
	if got := float64(nOff) / n; got != res.OffGreen {
		t.Errorf("off-green fraction %.4f, aggregate says %.4f", got, res.OffGreen)
	}
	if want := sumStrokes / n; math.Abs(res.EStrokes-want) > 1e-12 {
		t.Errorf("EStrokes = %.6f, recomputed %.6f", res.EStrokes, want)
	}
	if want := sumMissNext / n; math.Abs(res.ThreePlus-want) > 1e-12 {
		t.Errorf("ThreePlus = %.6f, recomputed %.6f", res.ThreePlus, want)
	}
	if nMissOn > 0 {
		if want := float64(nShortOn) / float64(nMissOn); math.Abs(res.PctMissShort-want) > 1e-12 {
			t.Errorf("PctMissShort = %.4f, recomputed over on-green misses %.4f", res.PctMissShort, want)
		}
		if want := sumLeave / float64(nMissOn); math.Abs(res.MeanLeave-want) > 1e-12 {
			t.Errorf("MeanLeave = %.4f, recomputed %.4f", res.MeanLeave, want)
		}
	}
}

// Field nodes that fall off the green cannot be solved and must carry the
// pessimistic filler; the rest of the field stays finite.
func TestFieldOffGreenNodes(t *testing.T) {
	surf := boundedFlatGreen(t, 1.0)
	env := physics.NewEnv(surf, physics.PennerVC0)
	sk := testSkill(t, "tour")
	f := BuildField(env, sk, FieldOpts{LagRollout: 0.25, Trials: 100, Sweeps: 2, Seed: 3, OffPenalty: 0.5})

	if f.OffPenalty != 0.5 {
		t.Errorf("OffPenalty not carried onto the field: %g", f.OffPenalty)
	}
	fillerSeen := false
	for ri, r := range f.Rs {
		for pi, psi := range f.Psis {
			ball := physics.Vec2{X: r * math.Cos(psi), Y: r * math.Sin(psi)}
			e := f.E[ri][pi]
			if math.IsNaN(e) || math.IsInf(e, 0) {
				t.Fatalf("non-finite field value at r=%g psi=%g", r, psi)
			}
			if !surf.OnGreen(ball.X, ball.Y) && e == 2+r/2 && f.P[ri][pi] == 0 {
				fillerSeen = true
			}
		}
	}
	if !fillerSeen {
		t.Error("no off-green node carries the pessimistic filler")
	}
}
