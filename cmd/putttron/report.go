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
	htmlStimp := fs.Float64("stimp", 10, "green speed shown in the pace-matrix page")
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
	fmt.Fprintf(&b, "hole of the error-free putt. \"plateau\" is the rollout range whose\n")
	fmt.Fprintf(&b, "expected strokes are within one Monte Carlo SE of the minimum —\n")
	fmt.Fprintf(&b, "anywhere in it is statistically as good as the optimum. \"miss past\"\n")
	fmt.Fprintf(&b, "is the mean distance past the hole of missed putts at the optimum\n")
	fmt.Fprintf(&b, "(the founding question), with the share of misses left short.\n\n")
	fmt.Fprintf(&b, "Clock: 12 = above the hole (downhill putt), 6 = below (uphill), 3 = sidehill.\n\n")

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
			best, lo, hi := argminSE(g)
			r := g[best]
			clock := fmt.Sprintf("%d", k.clock)
			if k.slope == 0 {
				clock = "—"
			}
			fmt.Fprintf(&b, "| %s | %.0f | %.0f° | %s | %.2f m (%.0f in) | %.1f–%.1f m | %.1f | %.1f | %.3f | %.2f m (%.0f in) | %.0f |\n",
				k.skill, k.lengthFt, k.slope, clock,
				r.rollout, r.rollout*39.37,
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
	var skills []string
	lengthSet, slopeSet := map[float64]bool{}, map[float64]bool{}
	for _, k := range keys {
		if k.stimp != stimp {
			continue
		}
		g := groups[k]
		sort.Slice(g, func(i, j int) bool { return g[i].rollout < g[j].rollout })
		best, lo, hi := argminSE(g)
		r := g[best]
		key := fmt.Sprintf("%s|%.0f|%.0f|%d", k.skill, k.lengthFt, k.slope, k.clock)
		data[key] = cell{
			Ro: r.rollout, Plo: lo, Phi: hi,
			Make: math.Round(1000*r.make_) / 10, Tp: math.Round(1000*r.threePlus) / 10,
			E: math.Round(1000*r.eStrokes) / 1000, Past: math.Round(100*r.meanPastMiss) / 100,
			Short: math.Round(100 * r.pctShort),
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
	}
	if len(data) == 0 {
		return fmt.Errorf("no rows at stimp %g for pace-matrix page", stimp)
	}
	sort.Slice(skills, func(i, j int) bool { return skillOrder(skills[i]) < skillOrder(skills[j]) })

	skillPairs := make([][2]string, len(skills))
	for i, s := range skills {
		skillPairs[i] = [2]string{s, skillDesc[s]}
	}
	lengths := sortedKeys(lengthSet)
	var slopes [][2]any
	for _, s := range sortedKeys(slopeSet) {
		label := "flat"
		if s != 0 {
			label = fmt.Sprintf("%.1f%%", 100*math.Tan(s*math.Pi/180))
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

// argminSE returns the index of the minimum-E row plus the rollout range
// within one SE of the minimum.
func argminSE(g []rptRow) (best int, lo, hi float64) {
	best = 0
	for i, r := range g {
		if r.eStrokes < g[best].eStrokes {
			best = i
		}
	}
	min, se := g[best].eStrokes, g[best].eSE
	lo, hi = g[best].rollout, g[best].rollout
	for _, r := range g {
		if r.eStrokes <= min+se {
			if r.rollout < lo {
				lo = r.rollout
			}
			if r.rollout > hi {
				hi = r.rollout
			}
		}
	}
	return best, lo, hi
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
		rows = append(rows, rptRow{
			stimp: p(0), skill: r[1], slope: p(2), clock: clock,
			lengthFt: p(4), rollout: p(5), solveOK: r[6] == "true",
			make_: p(7), makeSE: p(8), threePlus: p(9),
			eStrokes: p(10), eSE: p(11),
			meanPastMiss: p(12), pctShort: p(13), meanLeav: p(14),
		})
	}
	return rows, nil
}
