package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
	"github.com/jhoblitt/putttron/internal/sim"
)

// Published make-% by distance (docs/literature.md §2C): ShotLink tour data
// and Broadie's ~90-shooter amateur data. Keys are feet.
var calibDistances = []float64{3, 4, 5, 6, 8, 10, 15, 20, 30}

var published = map[string]map[float64]float64{
	"tour": {3: 0.96, 4: 0.88, 5: 0.77, 6: 0.66, 8: 0.50, 10: 0.40, 15: 0.23, 20: 0.15, 30: 0.07},
	"high": {3: 0.84, 4: 0.65, 5: 0.50, 6: 0.39, 8: 0.27, 10: 0.20, 15: 0.11, 20: 0.06, 30: 0.02},
	// hcp30 is SYNTHESIZED (docs/literature.md §2E): anchored on Broadie's
	// Am3 band (50% one-putt at 3.8 ft) and shaped to sit just below Shot
	// Scope's 25-hcp bucket table (3–6 ft 48%, 6–9 30%, 9–12 17%,
	// 12–18 12%, 18–24 6%, 24–30 4%, 30+ 2%).
	"hcp30": {3: 0.60, 4: 0.48, 5: 0.40, 6: 0.33, 8: 0.24, 10: 0.18, 15: 0.10, 20: 0.055, 30: 0.02},
}

// Source-matched green speeds: Bansal & Broadie calibrated pros on stimp 11
// and ~90-shooters on stimp 9.
var calibStimp = map[string]float64{
	"tour": 11, "scratch": 11, "mid": 9, "high": 9, "hcp30": 9,
}

func cmdCalibrate(args []string) {
	fs := newFlagSet("calibrate")
	trials := fs.Int("trials", 20000, "trials per distance")
	seed := fs.Uint64("seed", 1, "master RNG seed")
	rollout := fs.Float64("rollout", 0.25, "pace policy: target m past hole")
	fs.Parse(args)

	type cell struct {
		skill player.Skill
		ft    float64
		make  float64
		se    float64
		ok    bool
	}
	var cells []cell
	for _, sk := range player.Profiles {
		for _, ft := range calibDistances {
			cells = append(cells, cell{skill: sk, ft: ft})
		}
	}
	sim.ParallelDo(len(cells), func(i int) {
		c := &cells[i]
		env := physics.NewEnv(green.NewPlanar(0, physics.DecelFromStimp(calibStimp[c.skill.Name])), physics.PennerVC0)
		ball := physics.Vec2{X: -c.ft * 0.3048}
		c.make, c.se, c.ok = sim.MakeRate(env, ball, c.skill, *rollout, *trials, *seed)
	})

	var b strings.Builder
	fmt.Fprintf(&b, "# Calibration gate: flat-green make %% vs published\n\n")
	fmt.Fprintf(&b, "trials=%d seed=%d rollout=%.2fm; stimp per source conditions (tour/scratch 11, mid/high 9)\n\n",
		*trials, *seed, *rollout)
	fmt.Fprintf(&b, "| skill | ft | sim %% | published %% | diff |\n|---|---|---|---|---|\n")
	for _, c := range cells {
		if !c.ok {
			fmt.Fprintf(&b, "| %s | %.0f | SOLVE FAIL | | |\n", c.skill.Name, c.ft)
			continue
		}
		if pub, have := published[c.skill.Name][c.ft]; have {
			fmt.Fprintf(&b, "| %s | %.0f | %.1f ± %.1f | %.0f | %+.1f |\n",
				c.skill.Name, c.ft, 100*c.make, 100*c.se, 100*pub, 100*(c.make-pub))
		} else {
			fmt.Fprintf(&b, "| %s | %.0f | %.1f ± %.1f | — | |\n", c.skill.Name, c.ft, 100*c.make, 100*c.se)
		}
	}
	fmt.Print(b.String())
	if err := os.MkdirAll("results", 0o755); err == nil {
		if err := os.WriteFile("results/calibration.md", []byte(b.String()), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write results/calibration.md:", err)
		}
	}
}
