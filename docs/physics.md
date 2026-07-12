# Physics model

Implemented in `internal/physics`. Citations in `docs/literature.md` §1.

## Rolling dynamics

State: horizontal position and velocity (x, y, vx, vy). Pure rolling from
launch (skid phase ≈ first 10–20% of the putt is absorbed into the launch
speed; Penner makes the same approximation).

Acceleration (Penner 2002, small-slope planar form generalized to a gradient
field):

    a⃗ = (5/7)·g⃗_par − a_d·cosθ·v̂

- `g⃗_par = −g·∇z / sqrt(1+|∇z|²)`: in-plane gravity component.
- `cosθ = 1/sqrt(1+|∇z|²)`.
- `a_d`: flat-green rolling deceleration, `a_d = (5/7)·ρ_g·g` in Penner's
  parameterization. Calibrated from the Stimpmeter: release speed
  v₀ = 1.83 m/s rolling S feet gives `a_d = v₀²/(2·S·0.3048)` —
  Stimp 8 → 0.687, Stimp 10 → 0.549, Stimp 12 → 0.458 m/s².
- The 5/7 = 1/(1+2/5) is the solid-sphere rotational-inertia factor.

At (numerically) zero speed, rolling resistance opposes incipient downslope
motion: the ball stays at rest iff `(5/7)·g·sinθ ≤ a_d·cosθ`, i.e.
`tanθ ≤ (7/5)·a_d/g` (equivalent to Penner's ρ_g ≥ tanθ). Runaway grades:
>7.8% at Stimp 10, >6.5% at Stimp 12.

Integrator: fixed-step RK4, dt = 1 ms, terminate at rest (speed < 1 mm/s and
the rest condition holds) or capture; runaway if still moving at MaxTime.

## Hole capture

Hole radius R_H = 54 mm; ball radius 21.335 mm. When the ball's center enters
the hole disk, compute the impact parameter b (perpendicular distance from
the hole center to the entry velocity line). Captured iff

    v ≤ v_c(b) = VC0·(1 − (b/R_H)²) / sqrt(1 − s)

- VC0 = 1.63 m/s: Penner/Holmes center-hit critical speed including lip-roll
  and bounce-in (free-fall alone would be 1.31 m/s).
- Quadratic falloff in b: Penner Eq. 23.
- s = sine of green slope along travel direction, positive uphill: Penner
  Eq. 30 slope correction (uphill entries tolerate slightly more speed).

A ball that comes to rest with its center over the hole disk is also holed.
A ball entering the disk too fast is assumed to roll/bounce across and
continue (rim dynamics beyond the capture criterion are not modeled).

## Off-green termination and scoring (Phase 2, bounded surfaces)

Real-green heightmaps have a finite valid region (NaN cells in the source
grid mark everything outside the buffered green polygon). Surfaces with a
boundary implement `green.Bounded`; the integrator checks `OnGreen` each
step and ends the putt at the first sample outside the region — the outcome
records the exit point. The interpolated surface itself is inpainted to be
finite everywhere, so leaving the region is a modeling decision, not a
numerical necessity. The aim solver treats an exiting trajectory like a
runaway ("too hot") and backs the speed off.

Scoring: an off-green trial is charged

    strokes = 1 + E(exit point) + OffPenalty

where E is the expected-strokes field looked up at the exit point (its
beyond-grid extrapolation applies) and `OffPenalty` (default **0.5
strokes**, a run parameter recorded in every manifest) is the cost of the
recovery — Broadie's strokes-gained baselines put fringe/short-rough shots
roughly 0.3–0.6 strokes above same-distance on-green putts across skill
levels, and 0.5 is the round midpoint. Off-green trials count as
3-putt-or-worse (no 2-putt save from off the green) and are excluded from
the miss-leave geometry (`MeanPastMiss`, `PctMissShort`, `MeanLeave`
describe putt leaves); the per-cell `OffGreen` fraction is reported so
readers can see the exposure.

Two known biases, both mild and both toward pessimism: expected-strokes
field nodes that fall off the green cannot be solved and carry the
pessimistic filler (E = 2 + r/2, P = 0), so boundary-adjacent lookups mix
filler values; and the exit-point E treats the ball as putting from the
boundary when the real ball would finish somewhere beyond it.

## What is deliberately not modeled (Phase 1)

- Sidespin: no curving-spin term — all surveyed models treat putt direction
  error as launch-angle error only (literature.md §1D).
- Grain: isotropic friction; anisotropic extension specified in
  literature.md §3, planned for Phase 3.
- The "lumpy donut" (surface degradation near the hole) — Pelz's original
  argument for firm pace; would need a stochastic bump model near the hole.
- Ball-rim collision dynamics (horseshoes, pop-outs) beyond the v_c(b)
  criterion.
