package main

import (
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

type rptRow struct {
	stimp, slope       float64
	clock              int
	lengthFt, rollout  float64
	skill              string
	solveOK            bool
	make_, makeSE      float64
	threePlus          float64
	eStrokes, eSE      float64
	meanPastMiss       float64
	pctShort, meanLeav float64
	dE, dSE            float64 // paired ΔE vs group's best rollout (CRN)
	hasPaired          bool
}

type groupKey struct {
	stimp, slope float64
	clock        int
	lengthFt     float64
	skill        string
}

// cmdReport reads a sweep CSV and emits the optimal-rollout analysis.
func cmdReport(args []string) {
	fs := newFlagSet("report")
	in := fs.String("in", "results/sweep-planar-v1.csv", "sweep CSV(s), comma-separated")
	out := fs.String("out", "results/optimal-rollout.md", "output markdown")
	htmlOut := fs.String("html", "results/pace-matrix.html", "interactive pace-matrix page (empty to skip)")
	breakoutOut := fs.String("breakout", "results/breakout-slope-clock.md", "slope-by-direction markdown tables (empty to skip)")
	htmlStimp := fs.Float64("stimp", 10, "green speed shown in the pace-matrix and breakout views")
	fs.Parse(args)

	var rows []rptRow
	for _, path := range strings.Split(*in, ",") {
		r, err := readSweep(strings.TrimSpace(path))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		rows = append(rows, r...)
	}

	groups := map[groupKey][]rptRow{}
	for _, r := range rows {
		if !r.solveOK {
			continue
		}
		k := groupKey{r.stimp, r.slope, r.clock, r.lengthFt, r.skill}
		groups[k] = append(groups[k], r)
	}

	var keys []groupKey
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
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
	})

	var b strings.Builder
	fmt.Fprintf(&b, "# Optimal rollout past the hole (Phase 1, planar greens)\n\n")
	fmt.Fprintf(&b, "Source: `%s` (see matching .manifest.yaml). Optimal target rollout\n", *in)
	fmt.Fprintf(&b, "minimizes expected putts to hole out; rollout is path length past the\n")
	fmt.Fprintf(&b, "hole of the error-free putt, sub-grid refined by a parabola through the\n")
	fmt.Fprintf(&b, "0.1 m sweep grid around the argmin. \"plateau\" is the rollout range whose\n")
	fmt.Fprintf(&b, "expected strokes are within one paired-difference Monte Carlo SE of the\n")
	fmt.Fprintf(&b, "minimum (paired via common random numbers across the rollout axis) —\n")
	fmt.Fprintf(&b, "anywhere in it is statistically as good as the optimum. \"miss past\"\n")
	fmt.Fprintf(&b, "is the mean distance past the hole of missed putts at the optimum\n")
	fmt.Fprintf(&b, "(the founding question), with the share of misses left short.\n\n")
	fmt.Fprintf(&b, "Clock: 12 = above the hole (downhill putt), 6 = below (uphill), 3 = sidehill.\n\n")
	fmt.Fprintf(&b, "Caveats: optima are conditional on the follow-up pace policy in the\n")
	fmt.Fprintf(&b, "manifest (`followup_lag_rollout_m`) — later putts are not co-optimized;\n")
	fmt.Fprintf(&b, "and direction error is calibrated on flat greens only, so per-slope\n")
	fmt.Fprintf(&b, "cells assume read error does not grow with break. See the caveats in\n")
	fmt.Fprintf(&b, "`findings-phase1.md`.\n\n")

	for _, stimp := range distinct(keys, func(k groupKey) float64 { return k.stimp }) {
		fmt.Fprintf(&b, "## Stimp %g\n\n", stimp)
		fmt.Fprintf(&b, "| skill | ft | slope | clock | opt rollout | plateau | make%% | 3-putt%% | E[putts] | miss past | short%% |\n")
		fmt.Fprintf(&b, "|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, k := range keys {
			if k.stimp != stimp {
				continue
			}
			g := groups[k]
			sort.Slice(g, func(i, j int) bool { return g[i].rollout < g[j].rollout })
			best, roStar, lo, hi := argminSE(g)
			r := g[best]
			clock := fmt.Sprintf("%d", k.clock)
			if k.slope == 0 {
				clock = "—"
			}
			fmt.Fprintf(&b, "| %s | %.0f | %.0f%% | %s | %.2f m (%.0f in) | %.1f–%.1f m | %.1f | %.1f | %.3f | %.2f m (%.0f in) | %.0f |\n",
				k.skill, k.lengthFt, k.slope, clock,
				roStar, roStar*39.37,
				lo, hi,
				100*r.make_, 100*r.threePlus, r.eStrokes,
				r.meanPastMiss, r.meanPastMiss*39.37, 100*r.pctShort)
		}
		fmt.Fprintf(&b, "\n")
	}

	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d groups)\n", *out, len(groups))

	if *htmlOut != "" {
		if err := writePaceMatrix(*htmlOut, *htmlStimp, *in, keys, groups); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s (stimp %g)\n", *htmlOut, *htmlStimp)
	}
	if *breakoutOut != "" {
		if err := writeBreakout(*breakoutOut, *htmlStimp, keys, groups); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s (stimp %g)\n", *breakoutOut, *htmlStimp)
	}
}

// writeBreakout emits the slope-by-direction markdown tables for one green
// speed — the same view as the pace-matrix page, in committable text form.
func writeBreakout(path string, stimp float64, keys []groupKey, groups map[groupKey][]rptRow) error {
	skills, lengths, slopes, clocks := axesAt(stimp, keys)
	var b strings.Builder
	fmt.Fprintf(&b, "# Optimal rollout by slope and putt direction (Stimp %g)\n\n", stimp)
	fmt.Fprintf(&b, "Optimal target rollout in inches (make%% / 3-putt%%); \"die\" = aim to have\n")
	fmt.Fprintf(&b, "the ball die at the front edge. Clock: 12 = ball above the hole putting\n")
	fmt.Fprintf(&b, "downhill, 6 = below putting uphill, 3 = sidehill (9 o'clock mirrors it).\n")
	fmt.Fprintf(&b, "Slope is %% grade. Green speed barely moves these (see findings).\n\n")
	fmt.Fprintf(&b, "Caveat: direction error is calibrated on flat greens and does not grow\n")
	fmt.Fprintf(&b, "with break, so the steeper cells are the least certain — they assume a\n")
	fmt.Fprintf(&b, "player reads a 5%% sidehill as well as a flat putt. Optima are also\n")
	fmt.Fprintf(&b, "conditional on the fixed follow-up lag policy (see findings-phase1.md).\n\n")
	clockName := map[int]string{12: "12 o'clock (downhill)", 3: "3 o'clock (sidehill)", 6: "6 o'clock (uphill)"}
	for _, sk := range skills {
		fmt.Fprintf(&b, "## %s\n\n", sk)
		for _, ft := range lengths {
			fmt.Fprintf(&b, "**%.0f ft**\n\n| slope |", ft)
			for _, c := range clocks {
				fmt.Fprintf(&b, " %s |", clockName[c])
			}
			fmt.Fprintf(&b, "\n|---|")
			for range clocks {
				fmt.Fprintf(&b, "---|")
			}
			fmt.Fprintf(&b, "\n")
			for _, s := range slopes {
				if s == 0 {
					fmt.Fprintf(&b, "| flat |")
				} else {
					fmt.Fprintf(&b, "| %.0f%% |", s)
				}
				for _, c := range clocks {
					g, ok := groups[groupKey{stimp, s, c, ft, sk}]
					if !ok {
						fmt.Fprintf(&b, " — |")
						continue
					}
					sort.Slice(g, func(i, j int) bool { return g[i].rollout < g[j].rollout })
					best, roStar, _, _ := argminSE(g)
					r := g[best]
					label := "die"
					if in := roStar * 39.37; in >= 0.5 {
						label = fmt.Sprintf("%.0f in", in)
					}
					fmt.Fprintf(&b, " **%s** (%.0f%% / %.0f%%) |", label, 100*r.make_, 100*r.threePlus)
				}
				fmt.Fprintf(&b, "\n")
			}
			fmt.Fprintf(&b, "\n")
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// axesAt derives the skill/length/slope/clock axes present at one stimp.
func axesAt(stimp float64, keys []groupKey) (skills []string, lengths, slopes []float64, clocks []int) {
	lengthSet, slopeSet := map[float64]bool{}, map[float64]bool{}
	clockSet := map[int]bool{}
	for _, k := range keys {
		if k.stimp != stimp {
			continue
		}
		seen := false
		for _, s := range skills {
			if s == k.skill {
				seen = true
			}
		}
		if !seen {
			skills = append(skills, k.skill)
		}
		lengthSet[k.lengthFt] = true
		slopeSet[k.slope] = true
		clockSet[k.clock] = true
	}
	sort.Slice(skills, func(i, j int) bool { return skillOrder(skills[i]) < skillOrder(skills[j]) })
	lengths = sortedKeys(lengthSet)
	slopes = sortedKeys(slopeSet)
	for _, c := range []int{12, 3, 6} {
		if clockSet[c] {
			clocks = append(clocks, c)
		}
	}
	return
}

//go:embed pace_matrix.tmpl.html
var paceMatrixTmpl string

var skillDesc = map[string]string{
	"tour":    "PGA Tour caliber",
	"scratch": "scratch (0 hcp)",
	"mid":     "mid handicap (~10)",
	"high":    "high handicap (~20, shoots ~90)",
	"hcp30":   "Broadie Am3 band (~26–45 hcp, shoots 98–120)",
}

// writePaceMatrix renders the self-contained interactive heatmap page for
// one green speed from the grouped sweep rows.
func writePaceMatrix(path string, stimp float64, inputs string, keys []groupKey, groups map[groupKey][]rptRow) error {
	type cell struct {
		Ro    float64 `json:"ro"`
		Plo   float64 `json:"plo"`
		Phi   float64 `json:"phi"`
		Make  float64 `json:"make"`
		Tp    float64 `json:"tp"`
		E     float64 `json:"E"`
		Past  float64 `json:"past"`
		Short float64 `json:"short"`
	}
	data := map[string]cell{}
	for _, k := range keys {
		if k.stimp != stimp {
			continue
		}
		g := groups[k]
		sort.Slice(g, func(i, j int) bool { return g[i].rollout < g[j].rollout })
		best, roStar, lo, hi := argminSE(g)
		r := g[best]
		key := fmt.Sprintf("%s|%.0f|%.0f|%d", k.skill, k.lengthFt, k.slope, k.clock)
		data[key] = cell{
			Ro: math.Round(1000*roStar) / 1000, Plo: lo, Phi: hi,
			Make: math.Round(1000*r.make_) / 10, Tp: math.Round(1000*r.threePlus) / 10,
			E: math.Round(1000*r.eStrokes) / 1000, Past: math.Round(100*r.meanPastMiss) / 100,
			Short: math.Round(100 * r.pctShort),
		}
	}
	if len(data) == 0 {
		return fmt.Errorf("no rows at stimp %g for pace-matrix page", stimp)
	}
	skills, lengths, slopeVals, _ := axesAt(stimp, keys)

	skillPairs := make([][2]string, len(skills))
	for i, s := range skills {
		skillPairs[i] = [2]string{s, skillDesc[s]}
	}
	var slopes [][2]any
	for _, s := range slopeVals {
		label := "flat"
		if s != 0 {
			label = fmt.Sprintf("%.0f%%", s)
		}
		slopes = append(slopes, [2]any{s, label})
	}

	var srcParts []string
	for _, p := range strings.Split(inputs, ",") {
		srcParts = append(srcParts, "<code>"+filepath.Base(strings.TrimSpace(p))+"</code>")
	}

	tmpl, err := template.New("pace").Parse(paceMatrixTmpl)
	if err != nil {
		return err
	}
	var out strings.Builder
	err = tmpl.Execute(&out, map[string]any{
		"Stimp":       stimp,
		"SourceNote":  strings.Join(srcParts, ", ") + " (seed in matching manifests)",
		"DataJSON":    mustJSON(data),
		"SkillsJSON":  mustJSON(skillPairs),
		"LengthsJSON": mustJSON(lengths),
		"SlopesJSON":  mustJSON(slopes),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func sortedKeys(m map[float64]bool) []float64 {
	var out []float64
	for v := range m {
		out = append(out, v)
	}
	sort.Float64s(out)
	return out
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// argminSE returns the index of the minimum-E row, a sub-grid refined
// optimum, and the rollout range within one SE of the minimum. The refined
// optimum is the vertex of a parabola through the argmin and its neighbors
// — sound because common random numbers make E(rollout) smooth — clamped to
// the neighbor interval; at a grid edge or under negative curvature it
// falls back to the grid point.
func argminSE(g []rptRow) (best int, roStar, lo, hi float64) {
	best = 0
	for i, r := range g {
		if r.eStrokes < g[best].eStrokes {
			best = i
		}
	}
	roStar = g[best].rollout
	if best > 0 && best < len(g)-1 {
		y0, y1, y2 := g[best-1].eStrokes, g[best].eStrokes, g[best+1].eStrokes
		curv := y0 - 2*y1 + y2
		if curv > 1e-12 {
			h := (g[best+1].rollout - g[best-1].rollout) / 2
			v := g[best].rollout - h/2*(y2-y0)/curv
			roStar = math.Max(g[best-1].rollout, math.Min(g[best+1].rollout, v))
		}
	}
	// Plateau: rollouts indistinguishable from the optimum. With paired
	// columns (CRN differencing done at sweep time) the yardstick is each
	// row's paired ΔE SE — much tighter than the marginal SE, which
	// overstates the plateau because CRN correlates the E estimates.
	min, se := g[best].eStrokes, g[best].eSE
	lo, hi = g[best].rollout, g[best].rollout
	for _, r := range g {
		ok := r.eStrokes <= min+se
		if r.hasPaired {
			ok = r.dE <= r.dSE
		}
		if ok {
			if r.rollout < lo {
				lo = r.rollout
			}
			if r.rollout > hi {
				hi = r.rollout
			}
		}
	}
	return best, roStar, lo, hi
}

func skillOrder(s string) int {
	switch s {
	case "tour":
		return 0
	case "scratch":
		return 1
	case "mid":
		return 2
	case "high":
		return 3
	case "hcp30":
		return 4
	}
	return 5
}

func distinct(keys []groupKey, f func(groupKey) float64) []float64 {
	seen := map[float64]bool{}
	var out []float64
	for _, k := range keys {
		v := f(k)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out
}

func readSweep(path string) ([]rptRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rec, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	var rows []rptRow
	for i, r := range rec {
		if i == 0 {
			continue
		}
		p := func(j int) float64 {
			v, _ := strconv.ParseFloat(r[j], 64)
			return v
		}
		clock, _ := strconv.Atoi(r[3])
		row := rptRow{
			stimp: p(0), skill: r[1], slope: p(2), clock: clock,
			lengthFt: p(4), rollout: p(5), solveOK: r[6] == "true",
			make_: p(7), makeSE: p(8), threePlus: p(9),
			eStrokes: p(10), eSE: p(11),
			meanPastMiss: p(12), pctShort: p(13), meanLeav: p(14),
		}
		if len(r) >= 17 {
			row.dE, row.dSE, row.hasPaired = p(15), p(16), true
		}
		rows = append(rows, row)
	}
	return rows, nil
}
