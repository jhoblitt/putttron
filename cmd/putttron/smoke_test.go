package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end: a tiny sweep writes a CSV+manifest, report reads it back and
// renders all three views. Catches column drift between writeCSV and
// readSweep and template/render regressions.
func TestSweepReportSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end Monte Carlo smoke")
	}
	dir := t.TempDir()
	cmdSweep([]string{
		"-trials", "60", "-fieldtrials", "40", "-fieldsweeps", "2", "-seed", "3",
		"-stimps", "10", "-slopes", "0", "-skills", "tour",
		"-out", dir, "-tag", "smoke",
	})

	csvPath := filepath.Join(dir, "smoke.csv")
	rows, err := readSweep(csvPath)
	if err != nil {
		t.Fatalf("readSweep: %v", err)
	}
	// 1 stimp × flat (single clock) × 3 lengths × 13 rollouts.
	if len(rows) != 39 {
		t.Fatalf("got %d rows, want 39", len(rows))
	}
	groups := map[float64][]rptRow{}
	for _, r := range rows {
		if !r.solveOK {
			t.Errorf("unsolved cell: %+v", r)
		}
		if !r.hasPaired {
			t.Error("paired ΔE columns missing")
		}
		if r.eStrokes < 1 {
			t.Errorf("E[strokes] = %g < 1", r.eStrokes)
		}
		groups[r.lengthFt] = append(groups[r.lengthFt], r)
	}
	for ft, g := range groups {
		zeros := 0
		for _, r := range g {
			if r.dE == 0 {
				zeros++
			}
			if r.dE < 0 {
				t.Errorf("%g ft: ΔE vs best is negative: %g", ft, r.dE)
			}
		}
		if zeros < 1 {
			t.Errorf("%g ft: no best row with ΔE = 0", ft)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "smoke.manifest.yaml")); err != nil {
		t.Errorf("manifest missing: %v", err)
	}

	mdPath := filepath.Join(dir, "optimal-rollout.md")
	htmlPath := filepath.Join(dir, "pace-matrix.html")
	breakoutPath := filepath.Join(dir, "breakout.md")
	cmdReport([]string{
		"-in", csvPath, "-out", mdPath, "-html", htmlPath,
		"-breakout", breakoutPath, "-stimp", "10",
	})
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("report output: %v", err)
	}
	if !strings.Contains(string(md), "| tour | 10 |") {
		t.Error("optimal-rollout.md missing the tour 10 ft row")
	}
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("pace-matrix output: %v", err)
	}
	if !strings.Contains(string(html), "tour|10|0|12") {
		t.Error("pace-matrix.html missing the tour 10 ft flat cell key")
	}
	if _, err := os.Stat(breakoutPath); err != nil {
		t.Errorf("breakout output missing: %v", err)
	}
}

func TestReadSweepRejectsMalformedNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.csv")
	csv := "stimp,skill,slope_pct,clock,length_ft,rollout_m,solve_ok,make,make_se,three_plus,exp_strokes,exp_strokes_se,mean_past_miss_m,pct_miss_short,mean_leave_m\n" +
		"10,tour,0,12,10,0.30,true,oops,0,0,2,0,0.3,0.4,0.5\n"
	if err := os.WriteFile(path, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSweep(path); err == nil {
		t.Error("malformed make column parsed without error")
	}
}
