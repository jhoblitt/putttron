# Phase 1 findings: how far past the hole should you putt?

Data: `sweep-planar-v2.csv` (manifest alongside; seed 1, 8000 trials/cell,
720 parameter groups — Stimp 8/10/12 × slopes 0–5% grade in 1% steps ×
12/3/6 o'clock × 10/15/20 ft × 13 rollout targets × 5 skill tiers — zero
solver failures). Full per-cell tables in `optimal-rollout.md`; calibration
gate in `calibration.md`. "Target rollout" = the distance past the hole the
error-free putt would finish (the pace you *aim* for); "miss past" = mean
distance past the hole of simulated missed putts at that target. Reported
optima are sub-grid refined (parabola through the 0.1 m sweep grid around
the argmin); the one-SE plateau is the honest uncertainty.

## The headline answer (10–20 ft putts)

Optimal target rollout, median (and range) across Stimp 8/10/12, grades
0–5%, and all ball-to-hole orientations:

| Skill | 10 ft | 15 ft | 20 ft |
|---|---|---|---|
| tour | 14 in (10–22) | 15 in (12–18) | 14 in (10–19) |
| scratch | 14 in (10–19) | 13 in (10–20) | 10 in (8–19) |
| mid (~10 hcp) | 12 in (9–16) | 9 in (6–14) | 5 in (0–11) |
| high (~20 hcp) | 10 in (8–15) | 6 in (2–12) | **die it** (0–7) |
| hcp30 (~26–45 hcp) | 7 in (4–15) | **die it** (0–7) | **die it** (0–8) |

At those targets, misses that go past finish on average ~14–22 in past for
tour/scratch and up to ~2.5 ft for the weaker tiers (their distance-control
sigma is larger, so the misses that do go long go well long even when the
target is the front edge).

**The optimum is a plateau, not a point.** Within each cell, a ±4–8 in band
of targets is statistically indistinguishable from the optimum (one Monte
Carlo SE ≈ 0.006 putts). Pace precision matters much less than picking the
right *regime*: firm for good putters at 10 ft, dying pace for weak putters
at 20 ft.

## Structure in the result

- **Skill is the dominant variable.** Good putters should be aggressive
  because their comeback putts are near-automatic (tour 3-putt rate at the
  optimum: ≤2% everywhere). Weak putters must protect against the 3-putt:
  at 20 ft, hcp30's 3-putt probability runs 20–45% even at the optimum, and
  every inch of extra pace feeds it.
- **Putt length steepens the skill gradient.** Everyone should play
  10-footers with real pace (7–14 in); by 20 ft the recommendation splits
  completely (tour ~14 in vs. die-at-the-hole for high/hcp30).
- **Green speed barely matters.** The optimal rollout in *distance* units
  is nearly Stimp-invariant from 8 to 12.
- **Slope direction matters more than slope magnitude.** Straight downhill
  putts (ball above the hole) give much higher make% — on a planar slope
  the fall line is a lateral attractor, so direction errors self-correct on
  downhill putts and grow on uphill ones (tour, 10 ft, 5% grade, Stimp 10:
  67% make downhill vs. 31% uphill). The cost of downhill putts is 3-putt
  risk, not make rate.
- **Sidehill putts are where firmness pays most for skilled putters**: the
  4–5% sidehill 10-footer has the most aggressive optimum in the matrix
  (tour: ~19–20 in) because firm pace takes out break; but for weak putters
  the same cell flips toward dying pace (hcp30: ~7 in) because the downhill
  comebacker after a firm miss is brutal — it is also their worst 3-putt
  cell (13–17% from 10 ft).

## Against prior art

- **Pelz's "17 inches"** (43 cm): sits inside our tour/scratch plateau for
  10–20 ft putts — good advice for strong putters — but is measurably too
  aggressive for mid-and-worse handicaps at 15–20 ft, where the optimum
  collapses toward zero. A single constant cannot be right; Pelz's own
  "lumpy donut" mechanism (surface degradation near the hole) is not in
  our model, which if real would push weak-putter optima slightly firmer
  than we report.
- **Broadie & Shin (2014)** found ~1–2 ft past optimal for a 12 ft putt on
  Stimp 11 with tour-calibrated errors, growing with slope and speed — our
  tour row (~14–15 in) reproduces this independently with a different
  physics engine, and matches their finding that the stakes are small near
  the optimum.
- **Aimpoint's ~9 in / PGA manual's ~12 in** land inside the plateau for
  everyone at 10 ft, and are better default advice than 17 in for mid/high
  handicappers at longer range.

## Caveats

- Planar, uniform greens; no lumpy donut, no grain, no wind, no mishit fat
  tails. Real-green surface degradation near the hole would penalize dying
  pace somewhat (Pelz's argument) — Phase 2/3 material.
- Symmetric Gaussian error model: real high-handicap misses skew short and
  low; hcp30 conclusions are the least certain (see docs/literature.md §5).
- Follow-up putts always use a fixed 0.25 m lag policy; the swept policy
  applies to the first putt only.
- Miss-leave distributions are somewhat tighter than Fearing et al.'s
  ShotLink gamma model (mean leave ~0.5 m vs ~0.65 m from 20 ft),
  i.e. real tour misses spread a bit more than simulated ones — our
  3-putt rates are correspondingly optimistic at the margin.

## Companion views

- `breakout-slope-clock.md` — optimal pace broken out by slope % and putt
  direction (12/3/6 o'clock) per skill and length, Stimp 10.
- `pace-matrix.html` — the same breakout as a self-contained interactive
  heatmap page, served at <https://jhoblitt.github.io/putttron/> (hover a
  cell for plateau, E[putts], and miss geometry).

Both are generated, together with `optimal-rollout.md`, by `putttron
report` from the sweep CSV.

## Reproduce

```
go run ./cmd/putttron sweep -trials 8000 -fieldtrials 1500 -fieldsweeps 5 -seed 1 -tag sweep-planar-v2
go run ./cmd/putttron report -in results/sweep-planar-v2.csv
```
