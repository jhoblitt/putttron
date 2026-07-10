package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
	"github.com/jhoblitt/putttron/internal/sim"
)

// clockPos places the ball length meters from the hole (origin). +X is
// downhill; 12 o'clock is directly upslope of the hole (a downhill putt),
// 6 o'clock directly downslope (uphill putt), 3 o'clock pure sidehill
// (9 o'clock is its mirror image on a planar green).
func clockPos(clock int, length float64) physics.Vec2 {
	switch clock {
	case 12:
		return physics.Vec2{X: -length}
	case 6:
		return physics.Vec2{X: length}
	case 3:
		return physics.Vec2{Y: length}
	}
	panic("bad clock")
}

type sweepRow struct {
	stimp    float64
	skill    string
	slopePct float64
	clock    int
	lengthFt float64
	rolloutM float64
	res      sim.CellResult

	// Paired comparison against the same (clock, length) group's best
	// rollout: ΔE = E − E_best and its paired SE. Common random numbers
	// make the trials paired across the rollout axis, so this SE — not
	// the marginal EStrokesSE — is the yardstick for whether a rollout
	// is distinguishable from the optimum.
	dEBest   float64
	dEPairSE float64
}

// pairGroup fills dEBest/dEPairSE for one CRN group (rows sharing everything
// but rollout) from the per-trial stroke vectors, then drops the vectors.
func pairGroup(g []*sweepRow) {
	best := -1
	for i, r := range g {
		if r.res.SolveOK && (best < 0 || r.res.EStrokes < g[best].res.EStrokes) {
			best = i
		}
	}
	if best < 0 {
		return
	}
	ref := g[best].res.Strokes
	for _, r := range g {
		if !r.res.SolveOK || len(r.res.Strokes) != len(ref) {
			continue
		}
		var sum, sumSq float64
		for t, s := range r.res.Strokes {
			d := s - ref[t]
			sum += d
			sumSq += d * d
		}
		nf := float64(len(ref))
		mean := sum / nf
		r.dEBest = mean
		varD := sumSq/nf - mean*mean
		r.dEPairSE = math.Sqrt(math.Max(varD, 0) / nf)
	}
	for _, r := range g {
		r.res.Strokes = nil
	}
}

func cmdSweep(args []string) {
	fs := newFlagSet("sweep")
	trials := fs.Int("trials", 8000, "trials per cell")
	fieldTrials := fs.Int("fieldtrials", 1500, "trials per field node per value-iteration sweep")
	fieldSweeps := fs.Int("fieldsweeps", 5, "value-iteration sweeps")
	seed := fs.Uint64("seed", 1, "master RNG seed")
	lag := fs.Float64("lag", 0.25, "pace policy for follow-up putts, m past hole")
	outDir := fs.String("out", "results", "output directory")
	tag := fs.String("tag", "sweep", "output file basename")
	stimpsF := fs.String("stimps", "8,10,12", "comma-separated stimp values")
	skillsF := fs.String("skills", "", "comma-separated skill names (default: all)")
	fs.Parse(args)

	profiles := player.Profiles
	if *skillsF != "" {
		profiles = nil
		for _, name := range strings.Split(*skillsF, ",") {
			sk, ok := player.ProfileByName(strings.TrimSpace(name))
			if !ok {
				fmt.Fprintf(os.Stderr, "unknown skill %q\n", name)
				os.Exit(2)
			}
			profiles = append(profiles, sk)
		}
	}

	var stimps []float64
	for _, s := range strings.Split(*stimpsF, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bad stimp %q: %v\n", s, err)
			os.Exit(2)
		}
		stimps = append(stimps, v)
	}
	slopes := []float64{0, 1, 2, 3, 4, 5}
	clocks := []int{12, 3, 6}
	lengthsFt := []float64{10, 15, 20}
	rollouts := []float64{0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0, 1.1, 1.2}

	start := time.Now()
	var rows []sweepRow
	for _, stimp := range stimps {
		ad := physics.DecelFromStimp(stimp)
		for _, slope := range slopes {
			surf := green.NewPlanar(slope, ad)
			env := physics.NewEnv(surf, physics.PennerVC0)
			cks := clocks
			if slope == 0 {
				cks = []int{12} // direction is meaningless on a flat green
			}
			for _, sk := range profiles {
				fmt.Fprintf(os.Stderr, "field: stimp=%g slope=%g%% skill=%s (t=%s)\n",
					stimp, slope, sk.Name, time.Since(start).Round(time.Second))
				field := sim.BuildField(env, sk, sim.FieldOpts{
					LagRollout: *lag, Trials: *fieldTrials, Sweeps: *fieldSweeps, Seed: *seed,
				})

				var cells []sweepRow
				for _, ck := range cks {
					for _, ft := range lengthsFt {
						for _, ro := range rollouts {
							cells = append(cells, sweepRow{
								stimp: stimp, skill: sk.Name, slopePct: slope,
								clock: ck, lengthFt: ft, rolloutM: ro,
							})
						}
					}
				}
				sim.ParallelDo(len(cells), func(i int) {
					c := &cells[i]
					ball := clockPos(c.clock, c.lengthFt*0.3048)
					// Common random numbers: seed depends on everything BUT
					// rollout, so E-vs-rollout curves are smooth.
					cellSeed := *seed<<32 ^ uint64(c.stimp*100)<<20 ^ uint64(c.slopePct)<<16 ^
						uint64(c.clock)<<8 ^ uint64(c.lengthFt)
					c.res = sim.EvalCell(env, ball, sk, c.rolloutM, field, *trials, cellSeed)
				})
				crn := map[[2]float64][]*sweepRow{}
				for i := range cells {
					k := [2]float64{float64(cells[i].clock), cells[i].lengthFt}
					crn[k] = append(crn[k], &cells[i])
				}
				for _, g := range crn {
					pairGroup(g)
				}
				rows = append(rows, cells...)
			}
		}
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	writeCSV(filepath.Join(*outDir, *tag+".csv"), rows)
	writeManifest(filepath.Join(*outDir, *tag+".manifest.yaml"), *trials, *fieldTrials, *fieldSweeps, *seed, *lag, stimps)
	fmt.Fprintf(os.Stderr, "done in %s: %d cells -> %s/%s.csv\n",
		time.Since(start).Round(time.Second), len(rows), *outDir, *tag)
}

func writeCSV(path string, rows []sweepRow) {
	var b strings.Builder
	b.WriteString("stimp,skill,slope_pct,clock,length_ft,rollout_m,solve_ok,make,make_se,three_plus,exp_strokes,exp_strokes_se,mean_past_miss_m,pct_miss_short,mean_leave_m,de_vs_best,de_pair_se\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%g,%s,%g,%d,%g,%.2f,%t,%.5f,%.5f,%.5f,%.5f,%.5f,%.4f,%.4f,%.4f,%.5f,%.5f\n",
			r.stimp, r.skill, r.slopePct, r.clock, r.lengthFt, r.rolloutM,
			r.res.SolveOK, r.res.Make, r.res.MakeSE, r.res.ThreePlus,
			r.res.EStrokes, r.res.EStrokesSE,
			r.res.MeanPastMiss, r.res.PctMissShort, r.res.MeanLeave,
			r.dEBest, r.dEPairSE)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeManifest(path string, trials, fieldTrials, fieldSweeps int, seed uint64, lag float64, stimps []float64) {
	desc := "unknown"
	if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		desc = strings.TrimSpace(string(out))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "generated: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "git: %s\n", desc)
	fmt.Fprintf(&b, "seed: %d\n", seed)
	fmt.Fprintf(&b, "trials_per_cell: %d\n", trials)
	fmt.Fprintf(&b, "field_trials_per_node: %d\n", fieldTrials)
	fmt.Fprintf(&b, "field_sweeps: %d\n", fieldSweeps)
	fmt.Fprintf(&b, "followup_lag_rollout_m: %g\n", lag)
	fmt.Fprintf(&b, "stimps: [%s]\n", trimJoin(stimps))
	fmt.Fprintf(&b, "physics:\n")
	fmt.Fprintf(&b, "  capture_vc0_ms: %g  # Penner 2002 / Holmes 1991\n", physics.PennerVC0)
	fmt.Fprintf(&b, "  stimp_release_ms: %g\n", physics.StimpSpeed)
	fmt.Fprintf(&b, "  hole_radius_m: %g\n", physics.HoleRadius)
	fmt.Fprintf(&b, "skills:  # docs/literature.md §5 (Bansal & Broadie 2008 anchors)\n")
	for _, sk := range player.Profiles {
		fmt.Fprintf(&b, "  %s: {dir_sigma_deg: %g, dist_sigma_pct: %g, dist_sigma_floor_m: %g}\n",
			sk.Name, sk.DirSigmaDeg, sk.DistSigmaPct, sk.DistSigmaFloor)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func trimJoin(vs []float64) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
	}
	return strings.Join(parts, ", ")
}
