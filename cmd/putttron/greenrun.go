package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"sync"

	"github.com/jhoblitt/putttron/internal/course"
	"github.com/jhoblitt/putttron/internal/geom"
	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
	"github.com/jhoblitt/putttron/internal/player"
	"github.com/jhoblitt/putttron/internal/sim"
)

// Ball placement outcomes. A position that cannot be played is reported with
// its reason rather than dropped, so a ring always accounts for all of its
// hours.
const (
	statusOK          = "ok"
	statusSolveFailed = "solve_failed"
	statusOffGreen    = "skipped_off_green"
	statusTooClose    = "skipped_too_close"
)

// flatGradient is the slope below which a pin has no usable fall line and the
// clock falls back to compass bearings (0.2% grade).
const flatGradient = 0.002

// pinMargin is how much on-green room a pin needs all around it.
const pinMargin = 0.5 // m

type BallSpec struct {
	Pos    physics.Vec2
	Hour   int    // 0 for explicitly placed balls
	Mode   string // "fall", "compass", or "xy"
	DistFt float64
	Status string
}

type RunSpec struct {
	Green      *course.Green
	GreensRepo string
	Stimp      float64
	Pin        physics.Vec2
	Balls      []BallSpec
	Skills     []player.Skill
	Rollouts   []float64
	Trials     int
	FieldNodes int // trials per field node per sweep
	FieldSweep int
	Lag        float64
	OffPenalty float64
	Seed       uint64

	// Echoed into the manifest so a run can be reproduced from it.
	RingFt    float64
	Hours     []int
	ClockMode string
}

type GreenRow struct {
	BallIdx int
	Ball    BallSpec
	Skill   string
	Rollout float64
	Res     sim.CellResult
	DE, DSE float64
}

// ContourLevel is a probability contour of a miss cloud, in green-local
// meters so it can be drawn straight onto the green map.
type ContourLevel struct {
	P     int            `json:"p"`
	A     float64        `json:"a"`
	Rings [][][2]float64 `json:"rings"`
}

type BallDispersion struct {
	BallIdx int            `json:"ball"`
	Skill   string         `json:"skill"`
	Rollout float64        `json:"rollout"`
	Pts     [][2]float64   `json:"pts"`
	Hull    [][2]float64   `json:"hull"`
	HullA   float64        `json:"hullA"`
	HDR     []ContourLevel `json:"hdr"`
	Holed   int            `json:"holed"`
	Miss    int            `json:"miss"`
	Off     int            `json:"off"`
}

type RunResult struct {
	Rows            []GreenRow
	Dispersion      []BallDispersion
	SlopeAtPinPct   float64
	FallAzimuthDeg  float64 // bearing of the upslope direction, degrees from +Y (grid north)
	CompassFallback bool
	Runaway         bool // the pin sits on a grade too steep to hold a ball at this speed
}

// upslope returns the unit vector pointing directly uphill at p, and whether
// the surface is too flat there for it to mean anything.
func upslope(surf green.Surface, p physics.Vec2) (physics.Vec2, bool) {
	gx, gy := surf.Gradient(p.X, p.Y)
	// The gradient points toward increasing elevation, i.e. uphill.
	m := math.Hypot(gx, gy)
	if m < flatGradient {
		return physics.Vec2{Y: 1}, true
	}
	return physics.Vec2{X: gx / m, Y: gy / m}, false
}

// ringBalls lays a clock face of ball positions around the pin. Twelve
// o'clock is directly upslope of the pin, so a 12 o'clock ball putts DOWNhill
// — the Phase 1 convention — and the hours run clockwise from there. In
// compass mode 12 o'clock is grid north instead.
func ringBalls(surf *green.Heightmap, pin physics.Vec2, distM float64, hours []int, mode string) ([]BallSpec, bool) {
	up := physics.Vec2{Y: 1}
	fallback := false
	if mode != "compass" {
		var flat bool
		up, flat = upslope(surf, pin)
		if flat {
			fallback = true
		}
	}
	balls := make([]BallSpec, 0, len(hours))
	for _, h := range hours {
		th := float64(h) * math.Pi / 6
		c, s := math.Cos(th), math.Sin(th)
		// Rotate the upslope direction clockwise by the hour.
		dir := physics.Vec2{X: up.X*c + up.Y*s, Y: -up.X*s + up.Y*c}
		b := BallSpec{
			Pos:    pin.Add(dir.Scale(distM)),
			Hour:   h,
			Mode:   mode,
			DistFt: distM / 0.3048,
			Status: statusOK,
		}
		if fallback {
			b.Mode = "compass"
		}
		switch {
		case !surf.OnGreen(b.Pos.X, b.Pos.Y):
			b.Status = statusOffGreen
		case distM < 0.25:
			b.Status = statusTooClose
		}
		balls = append(balls, b)
	}
	return balls, fallback
}

// validatePin rejects a pin that is off the green or crowded against its
// edge: a hole needs room around it for the ball to be puttable from every
// side. The margin is a practical stand-in for course-setup guidance, not a
// rule of golf.
func validatePin(surf *green.Heightmap, pin physics.Vec2) error {
	if !surf.OnGreen(pin.X, pin.Y) {
		return fmt.Errorf("pin (%.2f, %.2f) is not on the green", pin.X, pin.Y)
	}
	for i := 0; i < 16; i++ {
		th := 2 * math.Pi * float64(i) / 16
		p := pin.Add(physics.Vec2{X: math.Cos(th), Y: math.Sin(th)}.Scale(pinMargin))
		if !surf.OnGreen(p.X, p.Y) {
			return fmt.Errorf("pin (%.2f, %.2f) is within %.1f m of the edge of the green",
				pin.X, pin.Y, pinMargin)
		}
	}
	return nil
}

// Seeds are derived by hashing the run's identity rather than packing it into
// bit fields: the pin is a float and the green a string. Rollout is
// deliberately absent from cellSeed so trials are paired across the rollout
// axis (common random numbers).
func cellSeed(master uint64, greenLabel string, pin physics.Vec2, ballIdx int, skill string) uint64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "cell|%s|%.4f|%.4f|%d|%s", greenLabel, pin.X, pin.Y, ballIdx, skill)
	return master ^ h.Sum64()
}

func fieldSeed(master uint64, greenLabel string, pin physics.Vec2, skill string) uint64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "field|%s|%.4f|%.4f|%s", greenLabel, pin.X, pin.Y, skill)
	return master ^ h.Sum64()
}

// fieldCache keys expected-strokes fields by everything that shapes them, so
// moving a ball around a fixed pin (the interactive case) does not rebuild
// them.
type fieldCache struct {
	mu sync.Mutex
	m  map[string]*sim.Field
}

func newFieldCache() *fieldCache { return &fieldCache{m: map[string]*sim.Field{}} }

func (c *fieldCache) get(key string, build func() *sim.Field) *sim.Field {
	if c == nil {
		return build()
	}
	c.mu.Lock()
	f, ok := c.m[key]
	c.mu.Unlock()
	if ok {
		return f
	}
	f = build()
	c.mu.Lock()
	c.m[key] = f
	c.mu.Unlock()
	return f
}

// runGreen sweeps the target rollout for every playable ball position and
// skill on a real green, and captures the miss dispersion at each position's
// optimal pace.
func runGreen(spec RunSpec, cache *fieldCache, progress func(phase string, done, total int)) (*RunResult, error) {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	surf := spec.Green.Surf.WithDecel(physics.DecelFromStimp(spec.Stimp))
	if err := validatePin(surf, spec.Pin); err != nil {
		return nil, err
	}

	res := &RunResult{}
	gx, gy := surf.Gradient(spec.Pin.X, spec.Pin.Y)
	res.SlopeAtPinPct = 100 * math.Hypot(gx, gy)
	up, flat := upslope(surf, spec.Pin)
	res.CompassFallback = flat
	res.FallAzimuthDeg = math.Mod(math.Atan2(up.X, up.Y)*180/math.Pi+360, 360)
	// Above this grade gravity beats rolling resistance and nothing stops.
	res.Runaway = res.SlopeAtPinPct > 100*7*physics.DecelFromStimp(spec.Stimp)/(5*physics.G)

	newEnv := func() *physics.Env {
		e := physics.NewEnv(surf, physics.PennerVC0)
		e.HolePos = spec.Pin
		return e
	}

	type job struct {
		ballIdx, skillIdx, rolloutIdx int
	}
	var jobs []job
	for bi, b := range spec.Balls {
		if b.Status != statusOK {
			continue
		}
		for si := range spec.Skills {
			for ri := range spec.Rollouts {
				jobs = append(jobs, job{bi, si, ri})
			}
		}
	}

	// One field per skill: it is the expensive part and does not depend on
	// where the ball is.
	fields := make([]*sim.Field, len(spec.Skills))
	for si, sk := range spec.Skills {
		progress("field", si, len(spec.Skills))
		key := fmt.Sprintf("%s|%.4f|%.4f|%g|%s|%g|%d|%d|%d|%g",
			spec.Green.Info.Label, spec.Pin.X, spec.Pin.Y, spec.Stimp, sk.Name,
			spec.Lag, spec.FieldNodes, spec.FieldSweep, spec.Seed, spec.OffPenalty)
		sk := sk
		fields[si] = cache.get(key, func() *sim.Field {
			return sim.BuildField(newEnv(), sk, sim.FieldOpts{
				LagRollout: spec.Lag, Trials: spec.FieldNodes, Sweeps: spec.FieldSweep,
				Seed:       fieldSeed(spec.Seed, spec.Green.Info.Label, spec.Pin, sk.Name),
				OffPenalty: spec.OffPenalty,
			})
		})
	}
	progress("field", len(spec.Skills), len(spec.Skills))

	results := make([]sim.CellResult, len(jobs))
	var done int64
	var mu sync.Mutex
	sim.ParallelDo(len(jobs), func(i int) {
		j := jobs[i]
		sk := spec.Skills[j.skillIdx]
		results[i] = sim.EvalCell(newEnv(), spec.Balls[j.ballIdx].Pos, sk,
			spec.Rollouts[j.rolloutIdx], fields[j.skillIdx], spec.Trials,
			cellSeed(spec.Seed, spec.Green.Info.Label, spec.Pin, j.ballIdx, sk.Name))
		mu.Lock()
		done++
		if done%8 == 0 || int(done) == len(jobs) {
			progress("trials", int(done), len(jobs))
		}
		mu.Unlock()
	})

	// Pair the rollout axis within each (ball, skill): common random numbers
	// make the trials paired, so this is the honest yardstick for whether a
	// pace is distinguishable from the best one.
	type pairKey struct{ ball, skill int }
	byPair := map[pairKey][]int{}
	for i, j := range jobs {
		byPair[pairKey{j.ballIdx, j.skillIdx}] = append(byPair[pairKey{j.ballIdx, j.skillIdx}], i)
	}
	deltas := make([]float64, len(jobs))
	deltaSEs := make([]float64, len(jobs))
	bestRollout := map[pairKey]float64{}
	for pk, idx := range byPair {
		sort.Slice(idx, func(a, b int) bool {
			return spec.Rollouts[jobs[idx[a]].rolloutIdx] < spec.Rollouts[jobs[idx[b]].rolloutIdx]
		})
		group := make([]*sim.CellResult, len(idx))
		for a, i := range idx {
			group[a] = &results[i]
		}
		dE, dSE := pairDeltas(group)
		for a, i := range idx {
			deltas[i] = dE[a]
			deltaSEs[i] = dSE[a]
			if dE[a] == 0 && group[a].SolveOK {
				bestRollout[pk] = spec.Rollouts[jobs[i].rolloutIdx]
			}
		}
	}

	for i, j := range jobs {
		b := spec.Balls[j.ballIdx]
		if !results[i].SolveOK {
			b.Status = statusSolveFailed
		}
		res.Rows = append(res.Rows, GreenRow{
			BallIdx: j.ballIdx, Ball: b, Skill: spec.Skills[j.skillIdx].Name,
			Rollout: spec.Rollouts[j.rolloutIdx], Res: results[i],
			DE: deltas[i], DSE: deltaSEs[i],
		})
	}
	// Positions that were never played still get a row, so a ring always
	// accounts for every hour.
	for bi, b := range spec.Balls {
		if b.Status == statusOK {
			continue
		}
		for _, sk := range spec.Skills {
			res.Rows = append(res.Rows, GreenRow{BallIdx: bi, Ball: b, Skill: sk.Name})
		}
	}
	sort.SliceStable(res.Rows, func(a, b int) bool {
		ra, rb := res.Rows[a], res.Rows[b]
		if ra.BallIdx != rb.BallIdx {
			return ra.BallIdx < rb.BallIdx
		}
		if ra.Skill != rb.Skill {
			return skillOrder(ra.Skill) < skillOrder(rb.Skill)
		}
		return ra.Rollout < rb.Rollout
	})

	progress("dispersion", 0, len(byPair))
	pairs := make([]pairKey, 0, len(byPair))
	for pk := range byPair {
		pairs = append(pairs, pk)
	}
	sort.Slice(pairs, func(a, b int) bool {
		if pairs[a].ball != pairs[b].ball {
			return pairs[a].ball < pairs[b].ball
		}
		return pairs[a].skill < pairs[b].skill
	})
	disp := make([]BallDispersion, len(pairs))
	sim.ParallelDo(len(pairs), func(i int) {
		pk := pairs[i]
		ro, ok := bestRollout[pk]
		if !ok {
			return
		}
		sk := spec.Skills[pk.skill]
		cr, outs := sim.EvalCellRecord(newEnv(), spec.Balls[pk.ball].Pos, sk, ro, nil, spec.Trials,
			cellSeed(spec.Seed, spec.Green.Info.Label, spec.Pin, pk.ball, sk.Name))
		if !cr.SolveOK {
			return
		}
		disp[i] = dispersionOf(pk.ball, sk.Name, ro, outs)
	})
	res.Dispersion = disp
	progress("dispersion", len(pairs), len(pairs))
	return res, nil
}

// dispersionOf summarizes where a position's misses finished, in green-local
// meters so the UI can draw them directly on the green.
func dispersionOf(ballIdx int, skill string, rollout float64, outs []sim.TrialOutcome) BallDispersion {
	d := BallDispersion{BallIdx: ballIdx, Skill: skill, Rollout: rollout}
	var pts []geom.Pt
	for _, o := range outs {
		switch {
		case o.Holed:
			d.Holed++
		case o.OffGreen:
			d.Off++
			pts = append(pts, geom.Pt{X: o.Rest.X, Y: o.Rest.Y})
		case o.Runaway:
		default:
			pts = append(pts, geom.Pt{X: o.Rest.X, Y: o.Rest.Y})
		}
	}
	d.Miss = len(pts) - d.Off
	if len(pts) == 0 {
		return d
	}
	hull := geom.ConvexHull(pts)
	d.HullA = math.Abs(geom.PolyArea(hull))
	d.Hull = flatten(hull)

	shown := pts
	if step := (len(pts) + embedMaxPts - 1) / embedMaxPts; step > 1 {
		shown = nil
		for i := 0; i < len(pts); i += step {
			shown = append(shown, pts[i])
		}
	}
	d.Pts = flatten(shown)

	if len(pts) >= minKDEMisses {
		hx, hy := geom.BandwidthScott(pts)
		g := geom.KDEGrid(pts, hx, hy, kdeGridN, kdeGridN)
		for _, mass := range hdrMasses {
			rings := geom.Contours(g, geom.HDRLevel(g, pts, mass))
			lvl := ContourLevel{P: int(math.Round(100 * mass)), A: geom.RingsArea(rings)}
			for _, r := range rings {
				lvl.Rings = append(lvl.Rings, flatten(geom.DecimateRing(r, maxRingVerts)))
			}
			d.HDR = append(d.HDR, lvl)
		}
	}
	return d
}

func flatten(pts []geom.Pt) [][2]float64 {
	out := make([][2]float64, len(pts))
	for i, p := range pts {
		out[i] = [2]float64{math.Round(p.X*1000) / 1000, math.Round(p.Y*1000) / 1000}
	}
	return out
}

// standardRollouts is the swept pace policy axis, 0 to 1.2 m past the hole.
func standardRollouts() []float64 {
	out := make([]float64, 0, 13)
	for i := 0; i <= 12; i++ {
		out = append(out, float64(i)/10)
	}
	return out
}
