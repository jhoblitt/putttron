package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhoblitt/putttron/internal/course/coursetest"
)

// fixtureCourse writes a synthetic greens repository: a gentle green and one
// with a face steeper than a ball can hold, so the runaway/off-green paths
// are exercised without the real LiDAR data.
func fixtureCourse(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := coursetest.Build(dir, []coursetest.Spec{
		coursetest.PlaneSpec("hole_01", 10, 2),
		coursetest.SteepSpec("hole_02", 10),
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGreensweepIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end Monte Carlo")
	}
	repo := fixtureCourse(t)
	out := t.TempDir()
	args := func(dir string) []string {
		return []string{
			"-greens", repo, "-green", "hole_01", "-pin", "0,0",
			"-ringft", "15", "-hours", "12,3,6,9", "-clock", "fall",
			"-skills", "mid", "-stimp", "10",
			"-trials", "200", "-fieldtrials", "60", "-fieldsweeps", "2", "-seed", "5",
			"-out", dir, "-tag", "gs",
		}
	}
	cmdGreensweep(args(out))

	csv := readFile(t, filepath.Join(out, "gs.csv"))
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	// 4 hours x 1 skill x 13 rollouts, all playable on a 10 m green at 15 ft.
	if want := 1 + 4*13; len(lines) != want {
		t.Errorf("CSV has %d lines, want %d (header + 4 positions x 13 rollouts)", len(lines), want)
	}
	header := lines[0]
	for _, col := range []string{"green", "hour", "slope_at_pin_pct", "status", "off_green_pct", "de_vs_best"} {
		if !strings.Contains(header, col) {
			t.Errorf("CSV header is missing %q: %s", col, header)
		}
	}
	zeros := 0
	for _, ln := range lines[1:] {
		f := strings.Split(ln, ",")
		if f[12] != statusOK {
			t.Errorf("unexpected status %q on a playable position", f[12])
		}
		if f[len(f)-2] == "0.00000" {
			zeros++
		}
	}
	// Exactly one rollout per position is the paired best (ΔE == 0).
	if zeros != 4 {
		t.Errorf("%d rows have ΔE = 0, want one best rollout per position", zeros)
	}

	man := readFile(t, filepath.Join(out, "gs.manifest.yaml"))
	for _, key := range []string{"greens_repo:", "sha256:", "pin_local_m:", "ring:", "seed_scheme:",
		"off_green_penalty_strokes:", "slope_at_pin_pct:"} {
		if !strings.Contains(man, key) {
			t.Errorf("manifest is missing %q", key)
		}
	}
	// Go's %v prints a slice as "[1 2 3]", which is not a YAML sequence.
	if !strings.Contains(man, "hours: [12, 3, 6, 9]") {
		t.Errorf("the manifest's hours are not a valid YAML sequence:\n%s",
			lineWith(man, "ring:"))
	}

	// Same seed, same numbers.
	out2 := t.TempDir()
	cmdGreensweep(args(out2))
	if readFile(t, filepath.Join(out2, "gs.csv")) != csv {
		t.Error("greensweep is not reproducible from its seed")
	}
}

// A pin on a face steeper than the no-stop grade has no puttable pace at all:
// the ball cannot come to rest there. The run must say so — per position, and
// in the manifest — rather than hanging or inventing an answer.
func TestGreensweepSteepFaceIsUnsolvable(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end Monte Carlo")
	}
	repo := fixtureCourse(t)
	out := t.TempDir()
	// The east half of hole_02 falls at 12%; at Stimp 12 nothing stops above
	// about 6.5%.
	cmdGreensweep([]string{
		"-greens", repo, "-green", "hole_02", "-pin", "4,0",
		"-ringft", "12", "-hours", "12,6", "-clock", "fall",
		"-skills", "high", "-stimp", "12",
		"-trials", "150", "-fieldtrials", "40", "-fieldsweeps", "2", "-seed", "2",
		"-out", out, "-tag", "steep",
	})
	for _, ln := range strings.Split(strings.TrimSpace(readFile(t, filepath.Join(out, "steep.csv"))), "\n")[1:] {
		if f := strings.Split(ln, ","); f[12] != statusSolveFailed {
			t.Errorf("a putt on a 12%% face reported %q, want %q", f[12], statusSolveFailed)
		}
	}
	if man := readFile(t, filepath.Join(out, "steep.manifest.yaml")); !strings.Contains(man, "no-stop grade") {
		t.Error("the manifest does not warn that the pin is past the no-stop grade")
	}
}

// Misses on a pin cut near the edge must run off the green and be charged for
// it, rather than rolling on through the collar as if it were putting surface.
func TestGreensweepOffGreen(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end Monte Carlo")
	}
	repo := fixtureCourse(t)
	out := t.TempDir()
	// hole_01 is a 10 m green; this pin leaves only 1.5 m of green behind it.
	cmdGreensweep([]string{
		"-greens", repo, "-green", "hole_01", "-pin", "8.5,0",
		"-ringft", "10", "-hours", "12", "-clock", "fall",
		"-skills", "hcp30", "-stimp", "10",
		"-trials", "400", "-fieldtrials", "60", "-fieldsweeps", "2", "-seed", "4",
		"-out", out, "-tag", "edge",
	})
	worst := 0.0
	for _, ln := range strings.Split(strings.TrimSpace(readFile(t, filepath.Join(out, "edge.csv"))), "\n")[1:] {
		f := strings.Split(ln, ",")
		if f[12] != statusOK {
			continue
		}
		var pct float64
		if _, err := fmt.Sscanf(f[18], "%f", &pct); err == nil && pct > worst {
			worst = pct
		}
	}
	if worst == 0 {
		t.Error("no putt ran off a green with the pin 1.5 m from its edge")
	}
	t.Logf("firmest pace ran %.1f%% of putts off the green", worst)
}

func TestGreensweepRejectsBadPin(t *testing.T) {
	repo := fixtureCourse(t)
	// A pin out in the collar is not on the putting surface.
	idx, g := loadFixtureGreen(t, repo, "hole_01")
	_ = idx
	if err := validatePin(g.Surf, vec(15, 0)); err == nil {
		t.Error("a pin 15 m out — in the collar — was accepted")
	}
	if err := validatePin(g.Surf, vec(0, 0)); err != nil {
		t.Errorf("a pin in the middle of the green was rejected: %v", err)
	}
	// Right at the edge of a 10 m green there is not room for the margin.
	if err := validatePin(g.Surf, vec(9.8, 0)); err == nil {
		t.Error("a pin hard against the green's edge was accepted")
	}
}

func TestParsePoint(t *testing.T) {
	p, err := parsePoint(" 1.5 , -2.25 ")
	if err != nil || p.X != 1.5 || p.Y != -2.25 {
		t.Errorf("parsePoint = %+v, %v", p, err)
	}
	for _, bad := range []string{"1.5", "a,b", "1,2,3", ""} {
		if _, err := parsePoint(bad); err == nil {
			t.Errorf("parsePoint(%q) did not error", bad)
		}
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := expandHome("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("expandHome = %q", got)
	}
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("expandHome mangled an absolute path: %q", got)
	}
}

func lineWith(s, want string) string {
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, want) {
			return ln
		}
	}
	return "(not found)"
}
