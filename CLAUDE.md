# CLAUDE.md — putttron: putting physics simulation

## Project goal

Answer, from first principles, questions of putting strategy — the founding
question: **for 10–20 ft putts, what is the ideal distance for the ball to
roll past the hole on missed putts?** ("Ideal" = minimizes expected strokes to
hole out, not just first-putt make probability.)

Method: a physics simulation of a golf ball rolling on a putting green, driven
by Monte Carlo sampling of human execution errors (launch direction, speed
control, and optionally sidespin/grain), with error magnitudes per player
skill level calibrated against published literature (Penner, Holmes, Broadie,
Gelman & Nolan, Fearing et al., Pelz — see `docs/literature.md`). Published
answers (e.g. Pelz's "17 inches") are prior art to compare against, not
inputs.

## Language & conventions

- **Go 1.26**, module `github.com/jhoblitt/putttron`. Standard library only
  unless a dependency clearly earns its place.
- SI units internally everywhere: meters, seconds, radians, kg. Feet/inches
  and Stimp (feet) appear only at CLI/report boundaries; convert once at the
  edge.
- Coordinate frame: local tangent plane of the green, Z-up, meters. For
  analytic (planar) greens, +X = downslope ("fall line") direction, hole at
  origin. Clock positions describe the ball relative to the hole with 12
  o'clock directly upslope of the hole: a 6 o'clock ball putts uphill, a 12
  o'clock ball putts downhill, 3/9 o'clock are pure sidehill.
- Determinism: every stochastic run takes an explicit RNG seed; results in
  `results/` record the seed, parameter set, and git describe. A rerun with
  the same inputs must reproduce the same numbers.
- Testing: physics kernels get unit tests against closed-form cases (flat
  green stopping distance vs. Stimp, uphill/downhill asymmetry, capture
  thresholds from the literature). `go test ./...` and `go vet ./...` must
  pass before every commit. Run go commands directly, don't suggest them.

## Repository layout

```
putttron/
  CLAUDE.md               # this file
  README.md
  docs/
    literature.md         # survey with citations; source of all human-error numbers
    physics.md            # derivations: equations of motion, capture model, stimp calibration
  cmd/putttron/           # CLI (subcommands: sim, sweep, report)
  internal/green/         # green surfaces (analytic planes + heightmap greens)
  internal/physics/       # ball dynamics, integrator, hole capture
  internal/player/        # skill profiles, aim/pace solver, error sampling
  internal/sim/           # Monte Carlo engine, expected-strokes recursion, sweeps
  internal/report/        # CSV/JSON emitters, summary tables
  results/                # committed run outputs (small CSVs + markdown reports)
```

## Architecture

Four layers, each behind a small interface so greens, physics fidelity, and
player models can evolve independently:

### 1. Green (`internal/green`)

```go
type Surface interface {
    Elevation(x, y float64) float64          // z, meters
    Gradient(x, y float64) (gx, gy float64)  // ∂z/∂x, ∂z/∂y (dimensionless slope)
    Friction(x, y, dirx, diry float64) float64 // rolling deceleration coefficient at a point,
                                               // direction-dependent to allow grain
}
```

- `Planar`: analytic tilted plane, parameterized by slope angle (degrees) and
  fall-line azimuth. Phase 1 uses only this. Uniform friction derived from a
  Stimp value.
- `Heightmap`: bilinear/bicubic interpolation over a regular grid — the
  ingestion target for real greens (see "Real greens" below).
- Grain (future): anisotropic friction, e.g.
  `a_roll = a0 * (1 + k_grain * cos(θ_roll − θ_grain))`, plus a lateral force
  term; parameterization comes from the grain section of `docs/literature.md`.

### 2. Physics (`internal/physics`)

Ball state: position + velocity in the tangent plane (pure-roll assumption
makes spin state redundant except during the initial skid phase).

- **Rolling model (Penner 2002).** A rolling sphere on an inclined surface:
  `a = −a_d·v̂ + (5/7)·g_∥`, where `g_∥` is the in-plane gravity component
  from the local gradient and `a_d` is the rolling deceleration. The 5/7
  factor is the ring-a-sphere rotational-inertia correction (I = 2/5 mr²).
- **Stimp calibration.** The Stimpmeter releases the ball at ~1.83 m/s; a
  green of Stimp S feet gives `a_d = v0² / (2·S·0.3048)` (≈0.55 m/s² at
  Stimp 10). Green speed is an input parameter, default Stimp 10.
- **Skid phase.** Real putts skid ~10–20% of their length before pure roll;
  net effect is absorbed into the effective launch speed, and sidespin decays
  during skid. Sidespin is modeled (if literature supports a non-negligible
  effect) as a perturbation during a finite skid segment; default off.
- **Integrator.** Fixed-step RK4 (dt ≈ 1 ms) on the 4-state ODE; terminate
  when speed < ε and in-plane gravity can't overcome static friction (ball at
  rest on slope), or on capture. Trajectories are cheap (< a few thousand
  steps); correctness beats cleverness.
- **Hole capture (Holmes 1991 / Penner 2002).** Hole radius R = 54 mm (4.25 in
  diameter). When the ball's path crosses the hole disk, capture is decided by
  speed at the rim vs. a critical speed that shrinks with impact parameter b
  (offset from center line): captured iff `v ≤ v_c(b)`, with `v_c(0)` and the
  b-dependence taken from the literature (Holmes-style: the ball must fall
  ~half a radius while crossing the chord; v_c(0) on the order of 1.3–1.6 m/s
  — exact form documented in `docs/physics.md`). Lip-out/rim interactions
  beyond the capture criterion are out of scope initially.

### 3. Player (`internal/player`)

A skill profile is the error model:

```go
type Skill struct {
    Name          string
    DirSigmaDeg   float64 // std dev of launch-line error, degrees
    SpeedSigmaPct float64 // std dev of speed error, % of intended speed
    ReadBiasFn    ...     // optional systematic under-read of break (future)
}
```

Profiles: **tour**, **scratch**, **mid** (~10 hcp), **high** (~20 hcp).
Numbers come from `docs/literature.md` (Broadie's putt model, Gelman & Nolan,
Fearing et al.) and are **validated** by reproducing published
make-%-vs-distance tables per skill level before any strategy results are
trusted (see "Calibration gate" below).

The player also owns the **aim solver**: given a green, ball, and hole, and a
*pace policy* ("target rollout": the distance past the hole the ball would
stop if it ran over a filled hole), find the launch direction and speed that
(a) would stop the error-free ball that far past the hole and (b) on breaking
putts, aims the error-free trajectory through the hole center. Solved by
shooting (bisection/secant on launch angle and speed against the simulator
itself — no closed form on slopes).

### 4. Monte Carlo engine (`internal/sim`)

- **Trial**: sample direction + speed errors around the solved aim, integrate,
  record outcome (holed, or final rest position → leave distance).
- **Expected strokes**: `E = P(make)·1 + Σ misses (1 + E(next putt))`,
  evaluated recursively by re-running the solver+trial from each miss's rest
  position (depth-limited; beyond depth 4 assume 2 more strokes — with sane
  parameters this is unreachable). The second putt uses the same skill's
  errors — this is what penalizes blasting it 6 ft past.
- **Sweep**: for each (skill × slope × clock position × putt length), sweep
  target rollout from 0 to ~1.5 m; report make %, 3-putt %, expected strokes,
  and the expected-stroke-minimizing rollout **and** the mean distance past
  the hole of missed putts at that optimum (the founding question asks for the
  latter — note it differs from the error-free target rollout).
- Concurrency: trials are embarrassingly parallel; worker pool over
  goroutines, one RNG per worker seeded from the master seed.

### Calibration gate

Strategy conclusions are only publishable when the pipeline first reproduces,
within reasonable tolerance, (1) flat-green stopping distances implied by the
Stimp rating, (2) published capture-speed thresholds, and (3) published
make-%-by-distance curves per skill level on flat greens. `putttron report
--calibration` emits this comparison table; it goes in `results/` alongside
any strategy claims.

## Phase plan

1. **Phase 1 — planar greens (current).** Physics core, planar green, skill
   profiles from literature, calibration gate, then the founding-question
   sweep: skills × {0,1,2,3}° slopes × {12,3,6 o'clock} × 10/15/20 ft.
   (9 o'clock mirrors 3 o'clock by symmetry on a planar green — note it,
   don't burn CPU on it.) Deliverable: `results/optimal-rollout.md`.
2. **Phase 2 — real greens.** Ingest `green_maps` outputs; hole/ball placement
   grids on real surfaces; per-green strategy maps.
3. **Phase 3 — richer physics** as literature justifies: grain (anisotropic
   friction), skid-phase sidespin, lip interaction, off-center capture
   refinements, green-reading bias models.

## Real greens (`~/github/green_maps` interface)

The green_maps project (LiDAR → surface models, see its CLAUDE.md) emits per
green: `heightmap.npz` — keys `z` (2D float32, meters, NaN outside green,
row-major, north-up), `x0`, `y0` (UTM EPSG:6341 grid origin), `dx` (= dy,
0.25 m), `local_origin` (green centroid UTM) — plus `mesh.obj`/`mesh.glb` in
local centroid-origin coordinates, `meta.json`, and a repo-level
`outputs/greens/index.json` enumerating greens.

putttron will read `heightmap.npz` directly (npz = zip of npy; a small npy
parser is ~100 lines of Go — no cgo, no Python at sim time), recentering to
`local_origin` on load. Caveats to honor from green_maps: vertical fidelity is
macro-contour only (source RMSE ~5–10 cm; micro-break below noise floor), and
cells outside the buffered polygon are NaN — treat NaN as out-of-bounds
(putt ends, ball is "off the green"; score via a leave-distance penalty).
Bicubic interpolation for `Elevation`, analytic derivative of the interpolant
for `Gradient` — never finite-difference the raw grid at sub-cell scale.

## Results & reporting

- Every sweep writes a CSV (one row per cell of the parameter matrix) and a
  human-readable markdown summary under `results/`, both committed. Include:
  seed, trial count, skill table used (with literature citations), physics
  constants.
- Report uncertainty: Monte Carlo standard errors on make % and expected
  strokes; enough trials that the optimal-rollout argmin is stable (check by
  re-running with a different seed).
- Compare headline numbers against prior art (Pelz 17 in; Broadie/Fearing
  aggressiveness findings) and explain divergences rather than hiding them.

## Gotchas

- Do not conflate "target rollout" (pace policy: where the error-free putt
  would stop past the hole) with "mean distance past the hole of actual
  missed putts" — misses include speed error and breaking-putt geometry; the
  question asks about misses, the policy knob is the target. Report both.
- Capture speed matters more than anything: dying putts use the full 108 mm
  hole width; a putt arriving at 1.5 m/s uses a fraction of it. Get
  `v_c(b)` right and unit-test it before believing any optimum.
- Downhill putts on fast greens can fail to stop (a_d < (5/7)·g·sinθ);
  detect and report "runaway" cells instead of integrating forever. At Stimp
  10, ~3.15° is the theoretical no-stop slope — a 3° downhill cell at high
  Stimp is near-degenerate and the numbers will be extreme, not wrong.
- Error sigmas from different papers are not directly comparable (aim error
  vs. total directional dispersion; % distance error vs. absolute leave).
  `docs/literature.md` must state, per number, exactly what it measures;
  reconcile via the calibration gate, not by mixing definitions.
- Skill sigmas are per-putt-length dependent in some models (distance control
  degrades with length). Keep `Skill` extensible to length-dependent sigmas —
  a constant-% model is the Phase 1 approximation.
