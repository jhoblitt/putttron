package main

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
	"github.com/jhoblitt/putttron/internal/sim"
)

// cmdFit calibrates the effective direction-error model σ_dir(L)² =
// σ0² + (σ1·L)² per anchored skill (tour, high) against the published
// make-%-by-distance tables, absorbing green-reading error and green
// imperfection (docs/literature.md §5).
//
// Method: make% is, to good approximation, P(|N(0,σ_dir)| < w(L)) with w(L)
// the effective angular make-window (which also absorbs the distance-error
// contribution). One simulation pass with a KNOWN constant σ_ref measures
// w(L) = σ_ref·z(p_sim(L)) with z(p) = √2·erfinv(p); the σ needed at each
// distance is then w(L)/z(p_pub(L)), and (σ0, σ1) come from a linear
// regression of σ_needed² on L². A verification pass reruns the simulator
// with the fitted model.
func cmdFit(args []string) {
	fs := newFlagSet("fit")
	trials := fs.Int("trials", 40000, "trials per distance")
	seed := fs.Uint64("seed", 1, "master RNG seed")
	rollout := fs.Float64("rollout", 0.25, "pace policy: target m past hole")
	skills := fs.String("skills", "tour,high,hcp30", "comma-separated skills to fit")
	fs.Parse(args)

	for _, name := range strings.Split(*skills, ",") {
		base, _ := player.ProfileByName(name)
		pub := published[name]
		var dists []float64
		for ft := range pub {
			dists = append(dists, ft)
		}
		sort.Float64s(dists)

		ref := base
		ref.DirSigmaDegPerM = 0
		env := physics.NewEnv(green.NewPlanar(0, physics.DecelFromStimp(calibStimp[name])), physics.PennerVC0)

		pSim := make([]float64, len(dists))
		sim.ParallelDo(len(dists), func(i int) {
			m, _, ok := sim.MakeRate(env, physics.Vec2{X: -dists[i] * 0.3048}, ref, *rollout, *trials, *seed)
			if !ok {
				m = math.NaN()
			}
			pSim[i] = m
		})

		// Regression of σ_needed² on L² (L in meters).
		var sx, sy, sxx, sxy float64
		n := 0
		for i, ft := range dists {
			if math.IsNaN(pSim[i]) || pSim[i] <= 0 || pSim[i] >= 1 {
				continue
			}
			w := ref.DirSigmaDeg * zOf(pSim[i])
			sigNeed := w / zOf(pub[ft])
			l := ft * 0.3048
			x, y := l*l, sigNeed*sigNeed
			sx += x
			sy += y
			sxx += x * x
			sxy += x * y
			n++
		}
		nf := float64(n)
		slope := (nf*sxy - sx*sy) / (nf*sxx - sx*sx)
		intercept := (sy - slope*sx) / nf
		sig0 := math.Sqrt(math.Max(intercept, 0.01))
		sig1 := math.Sqrt(math.Max(slope, 0))

		fitted := base
		fitted.DirSigmaDeg = sig0
		fitted.DirSigmaDegPerM = sig1
		fmt.Printf("%s: sigma0=%.3f° sigma1=%.4f°/m  (σ at 10ft=%.2f°, 20ft=%.2f°, 30ft=%.2f°)\n",
			name, sig0, sig1, fitted.DirSigmaAt(3.048), fitted.DirSigmaAt(6.096), fitted.DirSigmaAt(9.144))

		verify := make([]float64, len(dists))
		sim.ParallelDo(len(dists), func(i int) {
			m, _, _ := sim.MakeRate(env, physics.Vec2{X: -dists[i] * 0.3048}, fitted, *rollout, *trials, *seed+7)
			verify[i] = m
		})
		sse := 0.0
		for i, ft := range dists {
			d := verify[i] - pub[ft]
			sse += d * d
			fmt.Printf("  %2.0f ft: sim %.1f%% pub %.0f%% diff %+.1f\n", ft, 100*verify[i], 100*pub[ft], 100*d)
		}
		fmt.Printf("  rms=%.1f pts\n", 100*rms(sse, len(dists)))

		if name == "hcp30" {
			// Independent validation against Broadie's Am3 anchors that were
			// NOT fitted: one-putt% == three-putt% at 12 ft, and ~2.7
			// average putts from 40 ft.
			field := sim.BuildField(env, fitted, sim.DefaultFieldOpts())
			r12 := sim.EvalCell(env, physics.Vec2{X: -12 * 0.3048}, fitted, *rollout, field, 40000, *seed+13)
			r40 := sim.EvalCell(env, physics.Vec2{X: -40 * 0.3048}, fitted, *rollout, field, 40000, *seed+17)
			fmt.Printf("  Am3 anchors: 12 ft one-putt %.1f%% vs three-putt %.1f%% (want equal); 40 ft avg putts %.2f (want ~2.7)\n",
				100*r12.Make, 100*r12.ThreePlus, r40.EStrokes)
		}
	}
}

// zOf returns z with P(|N(0,1)| < z) = p.
func zOf(p float64) float64 {
	return math.Sqrt2 * math.Erfinv(p)
}

func rms(sse float64, n int) float64 {
	return math.Sqrt(sse / float64(n))
}
