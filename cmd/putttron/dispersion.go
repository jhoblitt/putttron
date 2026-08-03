package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jhoblitt/putttron/internal/geom"
	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
	"github.com/jhoblitt/putttron/internal/sim"
)

// dispCell is one parameter cell's recorded dispersion: the counts of every
// trial outcome plus the rest positions of the misses (downsampled, but
// always retaining the full set's hull vertices).
type dispCell struct {
	key                             groupKey
	rollout                         float64
	nTrials, nHoled, nRunaway, nOff int
	nMiss                           int
	ball                            physics.Vec2 // where the putt was struck from
	axis                            physics.Vec2 // travel direction at the hole
	pts                             []missPt
}

type missPt struct {
	trial int
	p     geom.Pt
}

// cmdDispersion re-simulates each cell's optimal-pace putt and records where
// the misses finished. Common random numbers make this exact: the sweep's
// cell seed reproduces its trial sequence, so the recorded make rate must
// match the sweep CSV to the last printed digit.
func cmdDispersion(args []string) {
	fs := newFlagSet("dispersion")
	in := fs.String("in", "results/sweep-planar-v4.csv", "sweep CSV to take per-cell optima from")
	outDir := fs.String("out", "results", "output directory")
	tag := fs.String("tag", "dispersion-v2", "output file basename")
	stimp := fs.Float64("stimp", 10, "green speed to capture (the pace-matrix view)")
	trials := fs.Int("trials", 10000, "trials per cell; must match the sweep for CRN reproduction")
	seed := fs.Uint64("seed", 1, "master RNG seed; must match the sweep manifest")
	limit := fs.Int("cap", 1500, "max stored miss positions per cell (hull vertices always kept)")
	fs.Parse(args)

	rows, err := readSweep(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	groups := map[groupKey][]rptRow{}
	for _, r := range rows {
		if !r.solveOK || r.stimp != *stimp {
			continue
		}
		groups[groupKey{r.stimp, r.slope, r.clock, r.lengthFt, r.skill}] = append(
			groups[groupKey{r.stimp, r.slope, r.clock, r.lengthFt, r.skill}], r)
	}
	if len(groups) == 0 {
		fmt.Fprintf(os.Stderr, "no solved rows at stimp %g in %s\n", *stimp, *in)
		os.Exit(1)
	}

	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return lessGroup(keys[i], keys[j]) })

	cells := make([]dispCell, len(keys))
	wantMake := make([]float64, len(keys))
	for i, k := range keys {
		g := groups[k]
		sort.Slice(g, func(a, b int) bool { return g[a].rollout < g[b].rollout })
		best, _, _, _ := argminSE(g)
		cells[i] = dispCell{key: k, rollout: g[best].rollout}
		wantMake[i] = g[best].make_
	}

	start := time.Now()
	mismatch := make([]string, len(cells))
	sim.ParallelDo(len(cells), func(i int) {
		c := &cells[i]
		ad := physics.DecelFromStimp(c.key.stimp)
		env := physics.NewEnv(green.NewPlanar(c.key.slope, ad), physics.PennerVC0)
		sk, ok := player.ProfileByName(c.key.skill)
		if !ok {
			mismatch[i] = fmt.Sprintf("unknown skill %q", c.key.skill)
			return
		}
		ball := clockPos(c.key.clock, c.key.lengthFt*0.3048)
		res, outs := sim.EvalCellRecord(env, ball, sk, c.rollout, nil, *trials,
			cellSeedFor(*seed, c.key.stimp, c.key.slope, c.key.clock, c.key.lengthFt))
		if !res.SolveOK {
			mismatch[i] = "solve failed on re-simulation"
			return
		}
		// The CSV prints make to 5 decimals and one trial moves it by
		// 1/trials, so an exact CRN reproduction agrees well inside 1e-5.
		if math.Abs(res.Make-wantMake[i]) > 1e-5 {
			mismatch[i] = fmt.Sprintf("reproduced make %.5f but the sweep CSV says %.5f",
				res.Make, wantMake[i])
			return
		}

		c.nTrials = *trials
		c.ball = ball
		c.axis = res.Axis
		var misses []missPt
		for t, o := range outs {
			switch {
			case o.Holed:
				c.nHoled++
			case o.Runaway:
				c.nRunaway++
			case o.OffGreen:
				c.nOff++
			default:
				misses = append(misses, missPt{trial: t, p: geom.Pt{X: o.Rest.X, Y: o.Rest.Y}})
			}
		}
		c.nMiss = len(misses)
		c.pts = keepMisses(misses, *limit)
	})
	for i, m := range mismatch {
		if m != "" {
			k := cells[i].key
			fmt.Fprintf(os.Stderr,
				"%s %g ft %g%% clock %d: %s\n  the dispersion run must use the sweep's seed and trial count; re-run the sweep if the physics changed\n",
				k.skill, k.lengthFt, k.slope, k.clock, m)
			os.Exit(1)
		}
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	base := filepath.Join(*outDir, *tag)
	writeDispPoints(base+".points.csv", cells)
	writeDispCells(base+".cells.csv", cells)
	writeDispManifest(base+".manifest.yaml", *in, *stimp, *trials, *seed, *limit, cells)
	fmt.Fprintf(os.Stderr, "done in %s: %d cells -> %s.{points,cells}.csv\n",
		time.Since(start).Round(time.Second), len(cells), base)
}

func lessGroup(a, b groupKey) bool {
	if a.stimp != b.stimp {
		return a.stimp < b.stimp
	}
	if a.skill != b.skill {
		return skillOrder(a.skill) < skillOrder(b.skill)
	}
	if a.lengthFt != b.lengthFt {
		return a.lengthFt < b.lengthFt
	}
	if a.slope != b.slope {
		return a.slope < b.slope
	}
	return a.clock < b.clock
}

// keepMisses downsamples to at most limit points by uniform stride but always
// retains the FULL set's convex-hull vertices, so the hull computed from the
// kept sample is exactly the hull of every miss — the reported dispersion
// area does not depend on the storage cap.
func keepMisses(misses []missPt, limit int) []missPt {
	if len(misses) <= limit || limit < 1 {
		return misses
	}
	pts := make([]geom.Pt, len(misses))
	for i, m := range misses {
		pts[i] = m.p
	}
	keep := map[int]bool{}
	for _, i := range geom.ConvexHullIndices(pts) {
		keep[i] = true
	}
	n := len(misses)
	for j := 0; j < limit; j++ {
		keep[j*n/limit] = true
	}
	idx := make([]int, 0, len(keep))
	for i := range keep {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	out := make([]missPt, len(idx))
	for i, j := range idx {
		out[i] = misses[j]
	}
	return out
}

func writeDispPoints(path string, cells []dispCell) {
	var b strings.Builder
	b.WriteString("stimp,skill,slope_pct,clock,length_ft,rollout_m,trial,x_m,y_m\n")
	for _, c := range cells {
		for _, m := range c.pts {
			fmt.Fprintf(&b, "%g,%s,%g,%d,%g,%.2f,%d,%.3f,%.3f\n",
				c.key.stimp, c.key.skill, c.key.slope, c.key.clock, c.key.lengthFt,
				c.rollout, m.trial, m.p.X, m.p.Y)
		}
	}
	mustWrite(path, b.String())
}

func writeDispCells(path string, cells []dispCell) {
	var b strings.Builder
	b.WriteString("stimp,skill,slope_pct,clock,length_ft,rollout_m,n_trials,n_holed,n_runaway,n_off_green,n_miss,n_kept,ball_x_m,ball_y_m,axis_x,axis_y\n")
	for _, c := range cells {
		fmt.Fprintf(&b, "%g,%s,%g,%d,%g,%.2f,%d,%d,%d,%d,%d,%d,%.3f,%.3f,%.4f,%.4f\n",
			c.key.stimp, c.key.skill, c.key.slope, c.key.clock, c.key.lengthFt, c.rollout,
			c.nTrials, c.nHoled, c.nRunaway, c.nOff, c.nMiss, len(c.pts),
			c.ball.X, c.ball.Y, c.axis.X, c.axis.Y)
	}
	mustWrite(path, b.String())
}

func writeDispManifest(path, in string, stimp float64, trials int, seed uint64, limit int, cells []dispCell) {
	desc := "unknown"
	if out, err := exec.Command("git", "describe", "--always", "--dirty").Output(); err == nil {
		desc = strings.TrimSpace(string(out))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "generated: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "git: %s\n", desc)
	fmt.Fprintf(&b, "source_sweep: %s\n", in)
	fmt.Fprintf(&b, "seed: %d\n", seed)
	fmt.Fprintf(&b, "trials_per_cell: %d\n", trials)
	fmt.Fprintf(&b, "stimp: %g\n", stimp)
	fmt.Fprintf(&b, "cells: %d\n", len(cells))
	fmt.Fprintf(&b, "stored_points_cap: %d\n", limit)
	fmt.Fprintf(&b, "notes:\n")
	fmt.Fprintf(&b, "  rollout: per-group grid argmin from the source sweep; the cell seed is the\n")
	fmt.Fprintf(&b, "    sweep's own (common random numbers), so these are the sweep's exact trials\n")
	fmt.Fprintf(&b, "  points: rest positions of MISSED putts only, hole at the origin, meters,\n")
	fmt.Fprintf(&b, "    green frame (+X downhill); holed and runaway trials appear only as counts\n")
	fmt.Fprintf(&b, "  sampling: uniform stride down to stored_points_cap, with every convex-hull\n")
	fmt.Fprintf(&b, "    vertex of the FULL miss set retained, so the hull area is exact\n")
	fmt.Fprintf(&b, "  ball: where the putt was struck from, same frame as the points\n")
	fmt.Fprintf(&b, "  axis: unit direction of error-free travel AT THE HOLE — the past/short axis.\n")
	fmt.Fprintf(&b, "    On a breaking putt this is not the ball-to-hole line: the ball is still\n")
	fmt.Fprintf(&b, "    curving as it arrives (up to ~36° away on a 5%% sidehill 10-footer)\n")
	mustWrite(path, b.String())
}

func mustWrite(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
