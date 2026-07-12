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
  LICENSE                 # Apache-2.0
  .github/workflows/      # pages.yml deploys results/pace-matrix.html on push to main
  docs/
    literature.md         # survey with citations; source of all human-error numbers
    physics.md            # derivations: equations of motion, capture model, stimp calibration
  cmd/putttron/           # CLI: calibrate, fit, sweep, dispersion, report (+ pace_matrix.tmpl.html)
  internal/green/         # green surfaces (analytic planes + heightmap greens)
  internal/physics/       # ball dynamics, integrator, hole capture
  internal/player/        # skill profiles, aim/pace solver, error sampling
  internal/sim/           # Monte Carlo engine, expected-strokes field, cell evaluation
  internal/geom/          # hulls, polygon areas, KDE grids, HDR levels, marching squares
  internal/npz/           # NumPy npz/npy reader (+ npztest fixture writer)
  internal/course/        # greens-repo loader: index.json + heightmap.npz -> Heightmap
  results/                # committed run outputs (CSVs + manifests + reports + pace-matrix.html)
```

## Architecture

Four layers, each behind a small interface so greens, physics fidelity, and
player models can evolve independently:

### 1. Green (`internal/green`)

```go
type Surface interface {
    Elevation(x, y float64) float64          // z, meters
    Gradient(x, y float64) (gx, gy float64)  // ∂z/∂x, ∂z/∂y (dimensionless slope)
    DecelCoeff(x, y, dirX, dirY float64) float64 // flat-equivalent rolling deceleration a_d (m/s²),
                                                 // direction-dependent to allow grain
}
```

- `Planar`: analytic tilted plane, parameterized by slope as **% grade**
  (rise/run × 100 — the unit golfers, green books, and green_maps use; never
  degrees at any user-facing boundary). Phase 1 uses only this. Uniform
  friction derived from a Stimp value.
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
    Name            string
    DirSigmaDeg     float64 // direction error, length-independent component (deg)
    DirSigmaDegPerM float64 // direction error, length-proportional component (deg/m):
                            // σ_dir(L)² = σ0² + (σ1·L)² — read error scales with break
    DistSigmaPct    float64 // distance error as % of length, applied to v² (Broadie form)
    DistSigmaFloor  float64 // absolute distance-error floor, m
}
```

Profiles: **tour**, **scratch**, **mid** (~10 hcp), **high** (~20 hcp /
90-shooter), **hcp30** (Broadie's Am3 band, ~26–45 hcp). The ladder
deliberately STOPS at hcp30 — no shot-level putting data is published beyond
~45 hcp, and extrapolated tiers are not added (user decision 2026-07-06).
Direction sigmas are EFFECTIVE values fitted by `putttron fit` against
published make-% tables (they absorb green-reading error, which the source
papers model separately); distance sigmas come from Bansal & Broadie.
Provenance and fit quality live in `docs/literature.md` §5.

The player also owns the **aim solver**: given a green, ball, and hole, and a
*pace policy* ("target rollout": the path length past the hole the error-free
ball travels before stopping, hole treated as filled), find the launch
direction and speed whose trajectory passes through the hole center and stops
that far past. Implemented as damped 2-D Newton (numerical Jacobian) on the
residual (lateral miss at closest approach, path-length error), with
multi-start over perturbed initial aims — per-axis fixed-point updates
limit-cycle on strongly breaking putts, and dying sidehill putts sit on a
knife edge.

### 4. Monte Carlo engine (`internal/sim`)

- **Trial**: sample direction + speed errors around the solved aim, integrate,
  record outcome (holed, or final rest position → leave distance).
- **Expected strokes**: follow-up putts are scored with an
  **expected-strokes field** E(r,ψ) on a polar grid around the hole, built
  per (green config, skill) by value iteration — every putt in the field
  plays the same skill's errors under a fixed lag pace policy (`sweep -lag`,
  default 0.25 m). A first-putt miss looks up E at its rest position; this
  is what penalizes blasting it 6 ft past. The field also carries
  P(make next) for 3-putt probability. First-putt optima are therefore
  *conditional on that follow-up policy*, not a jointly optimized strategy
  — state this wherever results are published, and check it with `-lag`
  sensitivity runs.
- **Sweep**: for each (stimp × skill × slope × clock × putt length), sweep
  target rollout 0–1.2 m in 0.1 m steps with **common random numbers**
  (same seed across the rollout axis → smooth E-vs-rollout curves); report
  make %, 3-putt %, expected strokes, the expected-stroke-minimizing rollout
  (sub-grid refined by a parabola through the argmin's neighbors — sound
  under CRN) **and** the mean distance past the hole of missed putts at that
  optimum (the founding question asks for the latter — note it differs from
  the error-free target rollout). The sweep also differences per-trial
  stroke counts against the group's best rollout (CRN makes trials paired)
  and emits paired ΔE + SE columns (`de_vs_best`, `de_pair_se`); the
  report's "plateau" uses this paired SE — the marginal per-row SE
  overstates the equivalence band because CRN correlates the estimates.
- Concurrency: trials are embarrassingly parallel; `sim.ParallelDo` worker
  pool over cells/nodes, one deterministic RNG per work item derived from
  the master seed.

### Calibration gate

Strategy conclusions are only publishable when the pipeline first reproduces,
within reasonable tolerance, (1) flat-green stopping distances implied by the
Stimp rating, (2) published capture-speed thresholds, and (3) published
make-%-by-distance curves per skill level on flat greens. `putttron
calibrate` emits the comparison table (committed as
`results/calibration.md`); `putttron fit` is the tool that fits the
effective direction sigmas to those published tables in the first place.

## Phase plan

1. **Phase 1 — planar greens (COMPLETE, v2).** Physics core, planar green,
   skill profiles from literature, calibration gate, then the
   founding-question sweep: skills × {0–5}% grades in 1% steps ×
   {12,3,6 o'clock} × 10/15/20 ft × green speeds Stimp {8, 10, 12}
   (tour-speed 13+ can be added but collides with the downhill-runaway
   degeneracy below). (9 o'clock mirrors 3 o'clock by symmetry on a planar
   green — noted, not simulated.) Deliverables in `results/`:
   `sweep-planar-v3.csv` + manifest (v3 = v2's trials + paired ΔE columns),
   lag-policy/seed sensitivity slices (`sens-*.csv`), `findings-phase1.md`
   (headline answer),
   `optimal-rollout.md`, `breakout-slope-clock.md`, `pace-matrix.html`.
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

- Every sweep writes, under `results/`, all committed: a CSV table (one row
  per cell of the parameter matrix — CSV because these are large numeric
  tables meant for pandas/R/spreadsheets) and a YAML run manifest (seed,
  trial counts, physics constants, skill table used with literature
  citations, git describe). Rule of thumb: **YAML for configuration and
  manifests** (small, structured, commentable), **CSV for bulk tabular
  results** — don't put a 10k-row table in YAML or a run manifest in CSV.
- `putttron report` regenerates ALL derived views from the sweep CSV(s):
  `optimal-rollout.md` (full per-cell tables), `breakout-slope-clock.md`
  (slope × direction tables at one Stimp), and `pace-matrix.html` (the
  interactive heatmap page, rendered from
  `cmd/putttron/pace_matrix.tmpl.html`). Never hand-edit these outputs —
  change the generator and rerun.
- **Dispersion maps.** `putttron dispersion` re-simulates each pace-matrix
  cell at its optimal rollout using the *sweep's own cell seed* — common
  random numbers make this the identical trial sequence — and records where
  the misses came to rest (`dispersion-v1.{cells,points}.csv`). It hard-fails
  if a reproduced make% disagrees with the sweep CSV, so the two can never
  drift apart. `report -dispersion <base>` embeds per-cell geometry (convex
  hull + area, 50/80/95% KDE highest-density contours, a thinned scatter) into
  the page, where clicking a cell opens it. Points are downsampled to a cap
  but the full set's hull vertices are always retained, so the reported spread
  area does not depend on the cap.
- `results/pace-matrix.html` is served at
  <https://jhoblitt.github.io/putttron/> by `.github/workflows/pages.yml`
  on every push to main. Workflow actions are SHA-pinned (pinact) and
  actionlint-clean; Dependabot bumps the pins.
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
- Downhill putts on fast greens can fail to stop (grade > 7·a_d/(5·g), i.e.
  gravity along the slope beats rolling resistance); detect and report
  "runaway" cells instead of integrating forever. The no-stop grade is
  ~7.8% at Stimp 10, ~6.5% at Stimp 12, ~6.0% at Stimp 13 — the swept 5%
  max clears these, but a 5% downhill cell at high Stimp still produces
  extreme (correct) numbers, and pushing the sweep past 6% or to Stimp 13+
  will hit the degeneracy.
- Error sigmas from different papers are not directly comparable (aim error
  vs. total directional dispersion; % distance error vs. absolute leave).
  `docs/literature.md` must state, per number, exactly what it measures;
  reconcile via the calibration gate, not by mixing definitions.
- The effective σ_dir (which absorbs green-reading error) is calibrated on
  flat greens only and does not grow with break — per-slope cells in the
  breakout/pace-matrix views extrapolate a flat calibration into sloped
  conditions and are the least certain results; the fall-line-attractor
  finding (downhill putts self-correct) is the most sensitive to this.
  Keep the caveat attached until a break-dependent read-error model is
  calibrated (Phase 3).
- Direction error IS length-dependent (σ_dir(L)² = σ0² + (σ1·L)²) — a
  constant σ_dir fitted to 10–20 ft data badly under-makes short comeback
  putts for weak skills, which biases the optimal-rollout answer short.
  Distance error stays a constant % of length (Phase 1 approximation;
  Broadie & Shin's piecewise-in-v² data would refine it).
- Real high-handicap misses skew short and low-side (documented in
  literature.md §2E); the symmetric Gaussian model can't reproduce that, so
  hcp30's 3-footers over-make by ~10 points. Bias terms are Phase 3 work —
  keep the caveat attached to weak-skill conclusions until then.
