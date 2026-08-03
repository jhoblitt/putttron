package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jhoblitt/putttron/internal/course"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
)

// defaultGreensRepo is a green_maps output tree (see the "Real greens"
// section of CLAUDE.md); override with -greens.
const defaultGreensRepo = "~/github/crooked_tree_greens"

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}

func cmdGreensweep(args []string) {
	fs := newFlagSet("greensweep")
	greensDir := fs.String("greens", defaultGreensRepo, "greens repository (green_maps output tree)")
	label := fs.String("green", "", "green label, e.g. hole_07 (required)")
	pinF := fs.String("pin", "", "pin position in green-local meters, \"x,y\" (required)")
	ballsF := fs.String("balls", "", "explicit ball positions \"x,y;x,y\" (overrides the ring)")
	ringFt := fs.Float64("ringft", 20, "ring radius around the pin, feet")
	hoursF := fs.String("hours", "1,2,3,4,5,6,7,8,9,10,11,12", "clock positions on the ring")
	clockMode := fs.String("clock", "fall", "clock frame: fall (12 = upslope) or compass (12 = grid north)")
	skillsF := fs.String("skills", "", "comma-separated skill names (default: all)")
	stimp := fs.Float64("stimp", 10, "green speed, Stimp feet")
	trials := fs.Int("trials", 8000, "trials per cell")
	fieldTrials := fs.Int("fieldtrials", 1500, "trials per field node per value-iteration sweep")
	fieldSweeps := fs.Int("fieldsweeps", 5, "value-iteration sweeps")
	lag := fs.Float64("lag", 0.25, "pace policy for follow-up putts, m past hole")
	offPenalty := fs.Float64("offpenalty", 0.5, "expected-stroke penalty for leaving the green")
	seed := fs.Uint64("seed", 1, "master RNG seed")
	outDir := fs.String("out", "results/greens", "output directory")
	tag := fs.String("tag", "", "output file basename (default: <green>-ring<ft>)")
	fs.Parse(args)

	if *label == "" || *pinF == "" {
		fmt.Fprintln(os.Stderr, "greensweep needs -green and -pin (use putttron serve to pick a pin)")
		os.Exit(2)
	}
	repo := expandHome(*greensDir)
	idx, err := course.LoadIndex(repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	g, err := course.LoadGreen(idx, *label, physics.DecelFromStimp(*stimp))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pin, err := parsePoint(*pinF)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad -pin: %v\n", err)
		os.Exit(2)
	}

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

	surf := g.Surf.WithDecel(physics.DecelFromStimp(*stimp))
	var balls []BallSpec
	var hours []int
	if *ballsF != "" {
		for _, part := range strings.Split(*ballsF, ";") {
			p, err := parsePoint(part)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bad -balls entry %q: %v\n", part, err)
				os.Exit(2)
			}
			b := BallSpec{
				Pos: p, Mode: "xy", Status: statusOK,
				DistFt: p.Sub(pin).Norm() / 0.3048,
			}
			switch {
			case !surf.OnGreen(p.X, p.Y):
				b.Status = statusOffGreen
			case p.Sub(pin).Norm() < 0.25:
				b.Status = statusTooClose
			}
			balls = append(balls, b)
		}
	} else {
		for _, h := range strings.Split(*hoursF, ",") {
			v, err := strconv.Atoi(strings.TrimSpace(h))
			if err != nil || v < 1 || v > 12 {
				fmt.Fprintf(os.Stderr, "bad -hours entry %q (want 1..12)\n", h)
				os.Exit(2)
			}
			hours = append(hours, v)
		}
		balls, _ = ringBalls(surf, pin, *ringFt*0.3048, hours, *clockMode)
	}

	spec := RunSpec{
		Green: g, GreensRepo: repo, Stimp: *stimp, Pin: pin, Balls: balls,
		Skills: profiles, Rollouts: standardRollouts(),
		Trials: *trials, FieldNodes: *fieldTrials, FieldSweep: *fieldSweeps,
		Lag: *lag, OffPenalty: *offPenalty, Seed: *seed,
		RingFt: *ringFt, Hours: hours, ClockMode: *clockMode,
	}

	start := time.Now()
	res, err := runGreen(spec, nil, func(phase string, done, total int) {
		if done == total || done%64 == 0 {
			fmt.Fprintf(os.Stderr, "\r%s: %d/%d (t=%s)      ",
				phase, done, total, time.Since(start).Round(time.Second))
		}
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	name := *tag
	if name == "" {
		name = fmt.Sprintf("%s-ring%.0f", *label, *ringFt)
	}
	base := filepath.Join(*outDir, name)
	writeGreenCSV(base+".csv", spec, res)
	writeGreenManifest(base+".manifest.yaml", spec, res)

	var skipped, failed int
	for _, r := range res.Rows {
		switch r.Ball.Status {
		case statusOffGreen, statusTooClose:
			skipped++
		case statusSolveFailed:
			failed++
		}
	}
	fmt.Fprintf(os.Stderr, "done in %s: %d rows -> %s.csv (slope at pin %.1f%%",
		time.Since(start).Round(time.Second), len(res.Rows), base, res.SlopeAtPinPct)
	if res.CompassFallback {
		fmt.Fprintf(os.Stderr, ", too flat for a fall line - clock is compass")
	}
	if res.Runaway {
		fmt.Fprintf(os.Stderr, ", STEEPER THAN THE NO-STOP GRADE at stimp %g", *stimp)
	}
	fmt.Fprintf(os.Stderr, "); %d skipped rows, %d solver failures\n", skipped, failed)
}

func parsePoint(s string) (physics.Vec2, error) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return physics.Vec2{}, fmt.Errorf("want \"x,y\", got %q", s)
	}
	x, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return physics.Vec2{}, err
	}
	y, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return physics.Vec2{}, err
	}
	return physics.Vec2{X: x, Y: y}, nil
}

func writeGreenCSV(path string, spec RunSpec, res *RunResult) {
	mustWrite(path, greenCSV(spec, res))
}

func writeGreenManifest(path string, spec RunSpec, res *RunResult) {
	mustWrite(path, greenManifest(spec, res))
}

// greenCSV is the run's committed table; serve exports the identical bytes.
func greenCSV(spec RunSpec, res *RunResult) string {
	var b strings.Builder
	b.WriteString("green,pin_x_m,pin_y_m,stimp,skill,mode,hour,ball_x_m,ball_y_m,dist_ft,slope_at_pin_pct," +
		"rollout_m,status,make,make_se,three_plus,exp_strokes,exp_strokes_se,off_green_pct," +
		"mean_past_miss_m,pct_miss_short,mean_leave_m,de_vs_best,de_pair_se\n")
	for _, r := range res.Rows {
		hour := ""
		if r.Ball.Hour > 0 {
			hour = strconv.Itoa(r.Ball.Hour)
		}
		fmt.Fprintf(&b, "%s,%.3f,%.3f,%g,%s,%s,%s,%.3f,%.3f,%.2f,%.2f,",
			spec.Green.Info.Label, spec.Pin.X, spec.Pin.Y, spec.Stimp, r.Skill,
			r.Ball.Mode, hour, r.Ball.Pos.X, r.Ball.Pos.Y, r.Ball.DistFt, res.SlopeAtPinPct)
		if r.Ball.Status != statusOK {
			// A position that was never played carries its reason and no stats.
			fmt.Fprintf(&b, ",%s,,,,,,,,,,\n", r.Ball.Status)
			continue
		}
		fmt.Fprintf(&b, "%.2f,%s,%.5f,%.5f,%.5f,%.5f,%.5f,%.5f,%.4f,%.4f,%.4f,%.5f,%.5f\n",
			r.Rollout, statusOK,
			r.Res.Make, r.Res.MakeSE, r.Res.ThreePlus, r.Res.EStrokes, r.Res.EStrokesSE,
			100*r.Res.OffGreen, r.Res.MeanPastMiss, r.Res.PctMissShort, r.Res.MeanLeave,
			r.DE, r.DSE)
	}
	return b.String()
}

// greenManifest records everything needed to reproduce the run, including the
// identity of the heightmap it was run against.
func greenManifest(spec RunSpec, res *RunResult) string {
	desc := "unknown"
	if out, err := exec.Command("git", "describe", "--always", "--dirty").Output(); err == nil {
		desc = strings.TrimSpace(string(out))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "generated: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "git: %s\n", desc)
	fmt.Fprintf(&b, "greens_repo: %s\n", spec.GreensRepo)
	fmt.Fprintf(&b, "greens_git: %s\n", course.GitDescribe(spec.GreensRepo))
	fmt.Fprintf(&b, "green: %s\n", spec.Green.Info.Label)
	fmt.Fprintf(&b, "npz:\n")
	fmt.Fprintf(&b, "  path: %s\n", spec.Green.NPZPath)
	fmt.Fprintf(&b, "  size: %d\n", spec.Green.NPZSize)
	fmt.Fprintf(&b, "  sha256: %s\n", spec.Green.NPZSHA256)
	fmt.Fprintf(&b, "pin_local_m: [%.3f, %.3f]\n", spec.Pin.X, spec.Pin.Y)
	fmt.Fprintf(&b, "slope_at_pin_pct: %.2f\n", res.SlopeAtPinPct)
	fmt.Fprintf(&b, "fall_azimuth_deg: %.1f  # bearing of upslope, from grid north\n", res.FallAzimuthDeg)
	if res.CompassFallback {
		fmt.Fprintf(&b, "clock_fallback: compass  # pin too flat for a fall line\n")
	}
	if res.Runaway {
		fmt.Fprintf(&b, "warning: pin grade exceeds the no-stop grade at this green speed\n")
	}
	if len(spec.Hours) > 0 {
		hours := make([]string, len(spec.Hours))
		for i, h := range spec.Hours {
			hours[i] = strconv.Itoa(h)
		}
		fmt.Fprintf(&b, "ring: {dist_ft: %g, hours: [%s], mode: %s}\n",
			spec.RingFt, strings.Join(hours, ", "), spec.ClockMode)
	} else {
		fmt.Fprintf(&b, "ring: null  # explicit ball positions\n")
	}
	fmt.Fprintf(&b, "stimp: %g\n", spec.Stimp)
	fmt.Fprintf(&b, "seed: %d\n", spec.Seed)
	fmt.Fprintf(&b, "seed_scheme: \"pcg(master ^ fnv1a(cell|green|pin|ball|skill), 0xce11); rollout excluded (CRN)\"\n")
	fmt.Fprintf(&b, "trials_per_cell: %d\n", spec.Trials)
	fmt.Fprintf(&b, "field_trials_per_node: %d\n", spec.FieldNodes)
	fmt.Fprintf(&b, "field_sweeps: %d\n", spec.FieldSweep)
	fmt.Fprintf(&b, "followup_lag_rollout_m: %g\n", spec.Lag)
	fmt.Fprintf(&b, "off_green_penalty_strokes: %g  # docs/physics.md\n", spec.OffPenalty)
	fmt.Fprintf(&b, "physics:\n")
	fmt.Fprintf(&b, "  capture_vc0_ms: %g\n", physics.PennerVC0)
	fmt.Fprintf(&b, "  hole_radius_m: %g\n", physics.HoleRadius)
	fmt.Fprintf(&b, "green_meta:\n")
	fmt.Fprintf(&b, "  slope_mean_pct: %g\n", spec.Green.Meta.SlopeMeanPct)
	fmt.Fprintf(&b, "  slope_max_sustained_pct: %g\n", spec.Green.Meta.SlopeMaxSustainedPct)
	fmt.Fprintf(&b, "  fit_rms_m: %g\n", spec.Green.Meta.FitRMSM)
	fmt.Fprintf(&b, "  needs_review: %t\n", spec.Green.Meta.NeedsReview)
	fmt.Fprintf(&b, "  flags: %v\n", spec.Green.Meta.Flags)
	fmt.Fprintf(&b, "  vertical_fidelity: %q\n", spec.Green.Meta.VerticalFidelity)
	fmt.Fprintf(&b, "skills:  # docs/literature.md §5\n")
	for _, sk := range spec.Skills {
		fmt.Fprintf(&b, "  %s: {dir_sigma_deg: %g, dir_sigma_deg_per_m: %g, dist_sigma_pct: %g, dist_sigma_floor_m: %g}\n",
			sk.Name, sk.DirSigmaDeg, sk.DirSigmaDegPerM, sk.DistSigmaPct, sk.DistSigmaFloor)
	}
	return b.String()
}
