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
`tanθ ≤ (7/5)·a_d/g` (equivalent to Penner's ρ_g ≥ tanθ). Runaway slopes:
>4.5° at Stimp 10, >3.7° at Stimp 12.

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

## What is deliberately not modeled (Phase 1)

- Sidespin: no curving-spin term — all surveyed models treat putt direction
  error as launch-angle error only (literature.md §1D).
- Grain: isotropic friction; anisotropic extension specified in
  literature.md §3, planned for Phase 3.
- The "lumpy donut" (surface degradation near the hole) — Pelz's original
  argument for firm pace; would need a stochastic bump model near the hole.
- Ball-rim collision dynamics (horseshoes, pop-outs) beyond the v_c(b)
  criterion.
