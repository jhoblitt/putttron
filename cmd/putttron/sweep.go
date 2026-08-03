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

// cellSeedFor derives the common-random-numbers seed for one sweep cell:
// identical across the rollout axis, so re-running any single cell with its
// seed reproduces the sweep's exact trial sequence.
func cellSeedFor(master uint64, stimp, slopePct float64, clock int, lengthFt float64) uint64 {
	return master<<32 ^ uint64(stimp*100)<<20 ^ uint64(slopePct)<<16 ^
		uint64(clock)<<8 ^ uint64(lengthFt)
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
// but rollout).
func pairGroup(g []*sweepRow) {
	group := make([]*sim.CellResult, len(g))
	for i, r := range g {
		group[i] = &r.res
	}
	dE, dSE := pairDeltas(group)
	for i, r := range g {
		r.dEBest, r.dEPairSE = dE[i], dSE[i]
	}
}

// pairDeltas differences each result's per-trial strokes against the group's
// best under common random numbers — trial t is the same error draw in every
// member — and returns the paired ΔE and its SE. That SE, not the marginal
// per-cell one, is what says whether a pace is distinguishable from the best:
// CRN correlates the estimates, so the marginal SE overstates the band. The
// per-trial vectors are released once differenced.
func pairDeltas(group []*sim.CellResult) (dE, dSE []float64) {
	dE = make([]float64, len(group))
	dSE = make([]float64, len(group))
	best := -1
	for i, r := range group {
		if r.SolveOK && len(r.Strokes) > 0 && (best < 0 || r.EStrokes < group[best].EStrokes) {
			best = i
		}
	}
	if best < 0 {
		return dE, dSE
	}
	ref := group[best].Strokes
	for i, r := range group {
		if !r.SolveOK || len(r.Strokes) != len(ref) {
			continue
		}
		var sum, sumSq float64
		for t, s := range r.Strokes {
			d := s - ref[t]
			sum += d
			sumSq += d * d
		}
		nf := float64(len(ref))
		mean := sum / nf
		dE[i] = mean
		dSE[i] = math.Sqrt(math.Max(sumSq/nf-mean*mean, 0) / nf)
	}
	for _, r := range group {
		r.Strokes = nil
	}
	return dE, dSE
}

func cmdSweep(args []string) {
	fs := newFlagSet("sweep")
	trials := fs.Int("trials", 10000, "trials per cell")
	fieldTrials := fs.Int("fieldtrials", 1500, "trials per field node per value-iteration sweep")
	fieldSweeps := fs.Int("fieldsweeps", 5, "value-iteration sweeps")
	seed := fs.Uint64("seed", 1, "master RNG seed")
	lag := fs.Float64("lag", 0.25, "pace policy for follow-up putts, m past hole")
	outDir := fs.String("out", "results", "output directory")
	tag := fs.String("tag", "sweep", "output file basename")
	stimpsF := fs.String("stimps", "8,10,12", "comma-separated stimp values")
	skillsF := fs.String("skills", "", "comma-separated skill names (default: all)")
	slopesF := fs.String("slopes", "0,1,2,3,4,5", "comma-separated slope grades, percent")
	lengthsF := fs.String("lengths", "10,15,20,30,40,50,60,70,80,90,100", "comma-separated putt lengths, feet")
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

	parseFloats := func(name, csv string) []float64 {
		var out []float64
		for _, s := range strings.Split(csv, ",") {
			v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bad %s %q: %v\n", name, s, err)
				os.Exit(2)
			}
			out = append(out, v)
		}
		return out
	}
	stimps := parseFloats("stimp", *stimpsF)
	slopes := parseFloats("slope", *slopesF)
	lengthsFt := parseFloats("length", *lengthsF)
	clocks := []int{12, 3, 6}
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
					c.res = sim.EvalCell(env, ball, sk, c.rolloutM, field, *trials,
						cellSeedFor(*seed, c.stimp, c.slopePct, c.clock, c.lengthFt))
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
	writeManifest(filepath.Join(*outDir, *tag+".manifest.yaml"), *trials, *fieldTrials, *fieldSweeps, *seed, *lag, stimps, slopes, lengthsFt)
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

func writeManifest(path string, trials, fieldTrials, fieldSweeps int, seed uint64, lag float64, stimps, slopes, lengthsFt []float64) {
	desc := "unknown"
	if out, err := exec.Command("git", "describe", "--always", "--dirty").Output(); err == nil {
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
	fmt.Fprintf(&b, "slopes_pct: [%s]\n", trimJoin(slopes))
	fmt.Fprintf(&b, "lengths_ft: [%s]\n", trimJoin(lengthsFt))
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
