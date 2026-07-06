# Phase 1 findings: how far past the hole should you putt?

Data: `sweep-planar-v1.csv` + `sweep-planar-v1-hcp30.csv` (manifests
alongside; seed 1, 8000 trials/cell, 450 parameter groups, zero solver
failures). Full per-cell tables in `optimal-rollout.md`; calibration gate in
`calibration.md`. "Target rollout" = the distance past the hole the
error-free putt would finish (the pace you *aim* for); "miss past" = mean
distance past the hole of simulated missed putts at that target.

## The headline answer (10–20 ft putts)

Optimal target rollout, median (and range) across Stimp 8/10/12, slopes
0–3°, and all ball-to-hole orientations:

| Skill | 10 ft | 15 ft | 20 ft |
|---|---|---|---|
| tour | 0.30 m ≈ 12 in (8–24) | 0.40 m ≈ 16 in (12–20) | 0.40 m ≈ 16 in (12–20) |
| scratch | 0.30 m ≈ 12 in | 0.30 m ≈ 12 in (8–20) | 0.30 m ≈ 12 in (8–20) |
| mid (~10 hcp) | 0.30 m ≈ 12 in | 0.20 m ≈ 8 in | 0.10 m ≈ 4 in (0–12) |
| high (~20 hcp) | 0.30 m ≈ 12 in (8–12) | 0.20 m ≈ 8 in (4–12) | **0.00 m — die it** (0–4) |
| hcp30 (~26–45 hcp) | 0.20 m ≈ 8 in (4–16) | **0.00 m — die it** (0–8) | **0.00 m — die it** (0–4) |

At those targets, misses that go past finish on average 0.35–0.55 m
(14–22 in) past for tour/scratch and 0.4–0.8 m for the weaker tiers (their
distance-control sigma is larger, so the misses that do go long go well
long even when the target is the front edge).

**The optimum is a plateau, not a point.** Within each cell, a ±10–20 cm
band of targets is statistically indistinguishable from the optimum
(one Monte Carlo SE ≈ 0.006 putts). Pace precision matters much less than
picking the right *regime*: firm for good putters at 10 ft, dying pace for
weak putters at 20 ft.

## Structure in the result

- **Skill is the dominant variable.** Good putters should be aggressive
  because their comeback putts are near-automatic (tour 3-putt rate at the
  optimum: ≤2% everywhere). Weak putters must protect against the 3-putt:
  at 20 ft, hcp30's 3-putt probability is 21–42% even at the optimum, and
  every centimeter of extra pace feeds it.
- **Putt length steepens the skill gradient.** Everyone should play
  10-footers with ~8–12 in of pace; by 20 ft the recommendation splits
  completely (tour 16 in vs. die-at-the-hole for high/hcp30).
- **Green speed barely matters.** The optimal rollout in *distance* units
  is nearly Stimp-invariant (8 vs 12 moves it at most one 0.1 m grid step).
- **Slope direction matters more than slope magnitude.** Straight downhill
  putts (ball above the hole) reward slightly *more* aggression and give
  much higher make% — on a planar slope the fall line is a lateral
  attractor, so direction errors self-correct on downhill putts and grow
  on uphill ones (tour, 10 ft, 3°, Stimp 10: 70% make downhill vs. 33%
  uphill). The cost of downhill putts is 3-putt risk, not make rate.
- **Sidehill putts are where firmness pays most for skilled putters**:
  the 3° sidehill 10-footer has the most aggressive optimum in the matrix
  (tour: 0.6 m ≈ 24 in) because firm pace takes out break; but for weak
  putters the same cell flips to dying pace (hcp30: 0.1 m) because the
  downhill comebacker after a firm miss is brutal.

## Against prior art

- **Pelz's "17 inches"** (0.43 m): sits inside our tour/scratch plateau for
  10–20 ft putts — good advice for strong putters — but is measurably too
  aggressive for mid-and-worse handicaps at 15–20 ft, where the optimum
  collapses toward zero. A single constant cannot be right; Pelz's own
  "lumpy donut" mechanism (surface degradation near the hole) is not in
  our model, which if real would push weak-putter optima slightly firmer
  than we report.
- **Broadie & Shin (2014)** found ~1–2 ft past optimal for a 12 ft putt on
  Stimp 11 with tour-calibrated errors, growing with slope and speed — our
  tour row (12–16 in) reproduces this independently with a different
  physics engine, and matches their finding that the stakes are small near
  the optimum.
- **Aimpoint's ~9 in / PGA manual's ~12 in** land inside the plateau for
  everyone at 10 ft, and are better default advice than 17 in for
  mid/high handicappers at longer range.

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
  heatmap page (open directly in a browser; hover a cell for plateau,
  E[putts], and miss geometry). Data is embedded from the sweep CSVs.

## Reproduce

```
go run ./cmd/putttron sweep -trials 8000 -fieldtrials 1500 -fieldsweeps 5 -seed 1 -tag sweep-planar-v1
go run ./cmd/putttron sweep -skills hcp30 -trials 8000 -fieldtrials 1500 -fieldsweeps 5 -seed 1 -tag sweep-planar-v1-hcp30
go run ./cmd/putttron report -in results/sweep-planar-v1.csv,results/sweep-planar-v1-hcp30.csv
```
