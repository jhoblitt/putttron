package main

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jhoblitt/putttron/internal/geom"
)

// The stored sample is thinned, but the hull it yields must be the hull of
// every miss — otherwise the reported dispersion area would depend on the
// storage cap.
func TestKeepMissesHullInvariant(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 9))
	for _, shape := range []string{"round", "elongated"} {
		var all []missPt
		for i := 0; i < 3000; i++ {
			x, y := rng.NormFloat64(), rng.NormFloat64()
			if shape == "elongated" {
				y *= 0.02
			}
			all = append(all, missPt{trial: i, p: geom.Pt{X: x, Y: y}})
		}
		kept := keepMisses(all, 400)
		if len(kept) < 400 {
			t.Errorf("%s: kept %d points, want at least the cap (400)", shape, len(kept))
		}
		pts := func(ms []missPt) []geom.Pt {
			out := make([]geom.Pt, len(ms))
			for i, m := range ms {
				out[i] = m.p
			}
			return out
		}
		wantA := math.Abs(geom.PolyArea(geom.ConvexHull(pts(all))))
		gotA := math.Abs(geom.PolyArea(geom.ConvexHull(pts(kept))))
		if math.Abs(gotA-wantA) > 1e-9 {
			t.Errorf("%s: hull area from the kept sample = %.6f, from all misses = %.6f", shape, gotA, wantA)
		}
		for i := 1; i < len(kept); i++ {
			if kept[i].trial <= kept[i-1].trial {
				t.Fatalf("%s: kept points are not in trial order", shape)
			}
		}
		if same := keepMisses(all[:200], 400); len(same) != 200 {
			t.Errorf("%s: a sample under the cap was thinned to %d", shape, len(same))
		}
	}
}

// Dispersion data that does not correspond to the sweep being reported must
// be rejected: the page can never show a scatter that disagrees with the
// make% printed beside it.
func TestDispersionStaleness(t *testing.T) {
	k := groupKey{stimp: 10, slope: 0, clock: 12, lengthFt: 10, skill: "tour"}
	groups := map[groupKey][]rptRow{k: {
		{rollout: 0.2, solveOK: true, eStrokes: 1.7, make_: 0.30, eSE: 0.01},
		{rollout: 0.3, solveOK: true, eStrokes: 1.6, make_: 0.40, eSE: 0.01},
		{rollout: 0.4, solveOK: true, eStrokes: 1.8, make_: 0.35, eSE: 0.01},
	}}
	good := &dispData{rollout: 0.3, nTrials: 1000, nHoled: 400, nMiss: 600}

	if err := validateDispersion(map[groupKey]*dispData{k: good}, groups, 10); err != nil {
		t.Fatalf("matching dispersion rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		d    *dispData
		key  groupKey
		want string
	}{
		{"wrong rollout", &dispData{rollout: 0.4, nTrials: 1000, nHoled: 400}, k, "rollout"},
		{"wrong make", &dispData{rollout: 0.3, nTrials: 1000, nHoled: 512}, k, "disagree"},
		{"unknown cell", good, groupKey{stimp: 10, slope: 3, clock: 6, lengthFt: 15, skill: "mid"}, "no matching sweep group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDispersion(map[groupKey]*dispData{tc.key: tc.d}, groups, 10)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
	if err := validateDispersion(map[groupKey]*dispData{k: good}, groups, 12); err == nil {
		t.Error("dispersion at another stimp than the page was accepted")
	}
}

// sweep -> dispersion -> report, end to end: the counts must reconcile, the
// run must be reproducible, and the page must carry drawable geometry.
func TestDispersionReportIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end Monte Carlo")
	}
	dir := t.TempDir()
	sweepArgs := []string{
		"-trials", "400", "-fieldtrials", "40", "-fieldsweeps", "2", "-seed", "3",
		"-stimps", "10", "-slopes", "0,2", "-skills", "mid", "-out", dir, "-tag", "smoke",
	}
	cmdSweep(sweepArgs)
	csvPath := filepath.Join(dir, "smoke.csv")

	dispArgs := func(out string) []string {
		return []string{
			"-in", csvPath, "-out", out, "-tag", "disp", "-stimp", "10",
			"-trials", "400", "-seed", "3", "-cap", "120",
		}
	}
	// A make% that disagreed with the sweep would exit here, so reaching the
	// end proves the common-random-numbers reproduction is exact.
	cmdDispersion(dispArgs(dir))

	base := filepath.Join(dir, "disp")
	disp, err := readDispersion(base)
	if err != nil {
		t.Fatalf("readDispersion: %v", err)
	}
	// flat (clock 12 only) + 2% (3 clocks), each at 3 lengths.
	if len(disp) != 12 {
		t.Fatalf("got %d dispersion cells, want 12", len(disp))
	}
	for k, d := range disp {
		if d.nTrials != 400 || d.nHoled+d.nRunaway+d.nOff+d.nMiss != 400 {
			t.Errorf("%+v: counts do not reconcile: %+v", k, d)
		}
		if len(d.pts) != d.nKept {
			t.Errorf("%+v: %d points for %d kept", k, len(d.pts), d.nKept)
		}
		if math.Abs(math.Hypot(d.axis.X, d.axis.Y)-1) > 1e-6 {
			t.Errorf("%+v: travel axis is not a unit vector: %+v", k, d.axis)
		}
	}

	dir2 := t.TempDir()
	cmdDispersion(dispArgs(dir2))
	for _, name := range []string{"disp.points.csv", "disp.cells.csv"} {
		a, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir2, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s is not reproducible across runs", name)
		}
	}

	htmlPath := filepath.Join(dir, "pace.html")
	cmdReport([]string{
		"-in", csvPath, "-dispersion", base, "-out", filepath.Join(dir, "opt.md"),
		"-html", htmlPath, "-breakout", filepath.Join(dir, "bo.md"), "-stimp", "10",
	})
	embeds := parseDispEmbeds(t, htmlPath)
	if len(embeds) != 12 {
		t.Fatalf("page carries %d dispersion cells, want 12", len(embeds))
	}
	for key, e := range embeds {
		if e.N != e.Holed+e.Runaway+e.Miss {
			t.Errorf("%s: embedded counts do not sum: %+v", key, e)
		}
		if len(e.Pts)%2 != 0 || len(e.Pts)/2 > e.Kept {
			t.Errorf("%s: %d embedded coordinates for %d kept points", key, len(e.Pts), e.Kept)
		}
		if e.HullA < 0 {
			t.Errorf("%s: negative hull area %g", key, e.HullA)
		}
		if e.Miss >= minKDEMisses && len(e.HDR) != len(hdrMasses) {
			t.Errorf("%s: %d misses but %d density levels", key, e.Miss, len(e.HDR))
		}
		if e.Miss < minKDEMisses && len(e.HDR) != 0 {
			t.Errorf("%s: only %d misses but density levels were drawn", key, e.Miss)
		}
		for i := 1; i < len(e.HDR); i++ {
			if e.HDR[i].A <= e.HDR[i-1].A {
				t.Errorf("%s: %d%% region (%.4f m²) is not larger than the %d%% one (%.4f m²)",
					key, e.HDR[i].P, e.HDR[i].A, e.HDR[i-1].P, e.HDR[i-1].A)
			}
		}
	}

	html := readFile(t, htmlPath)
	for _, want := range []string{`role="dialog"`, "hasmap", "convex hull", "highest-density"} {
		if !strings.Contains(html, want) {
			t.Errorf("page is missing the dispersion UI (%q not found)", want)
		}
	}

	// Without the flag the page must degrade to exactly the old behavior.
	plain := filepath.Join(dir, "plain.html")
	cmdReport([]string{
		"-in", csvPath, "-out", filepath.Join(dir, "opt2.md"), "-html", plain,
		"-breakout", filepath.Join(dir, "bo2.md"), "-stimp", "10",
	})
	if !strings.Contains(readFile(t, plain), "const DISP={};") {
		t.Error("a report without -dispersion did not emit an empty DISP")
	}
}

var dispRe = regexp.MustCompile(`(?m)^const DISP=(\{.*\});$`)

func parseDispEmbeds(t *testing.T, path string) map[string]dispEmbed {
	t.Helper()
	m := dispRe.FindStringSubmatch(readFile(t, path))
	if m == nil {
		t.Fatal("no DISP payload in the generated page")
	}
	var out map[string]dispEmbed
	if err := json.Unmarshal([]byte(m[1]), &out); err != nil {
		t.Fatalf("DISP payload is not valid JSON: %v", err)
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
