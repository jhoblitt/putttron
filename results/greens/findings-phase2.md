# Phase 2: putting on real greens

Two committed runs against the LiDAR greens of Crooked Tree Golf Course
(Tucson AZ), from [crooked_tree_greens](https://github.com/jhoblitt/crooked_tree_greens)
at `5b26b21`. Both put the pin at the green's centroid and ring it with all
twelve clock positions at 20 ft, Stimp 10, 8000 trials per cell, seed 1.
Everything needed to reproduce them — including the heightmap's sha256 — is in
the manifest beside each CSV.

- `hole01-ring20.csv` — a benign green (296 m², 1.7% mean slope, no review flags)
- `hole07-ring20.csv` — a flagged one (575 m², 3.8% mean, with a face sustaining
  **10.9%** — steeper than the ~7.8% at which a ball cannot stop at all on
  Stimp 10)

Clock convention as in Phase 1: **12 o'clock is directly upslope of the pin**,
so a 12 o'clock ball putts downhill.

## The planar answer survives contact with real terrain

| | tour | mid (~10 hcp) | hcp30 |
|---|---|---|---|
| **Phase 1, planar, 20 ft** | ~14 in | ~5 in | die it |
| **hole_01** (1.4% at the pin) | 12 in (8–16) | 4 in (4–8) | **0 in** (0–4) |
| **hole_07** (2.0% at the pin) | 12 in (12–16) | 0 in (0–8) | **0 in** (all 12 hours) |

The founding question's answer does not change on real greens: strong putters
should still finish about a foot past, and weak ones should still die the ball
at the hole. If anything, real terrain pushes *slightly* further toward dying
pace than a plane of the same average slope — undulation means a firm miss
finds a worse spot than it would on a plane, and hcp30 on hole_07 wants dying
pace from **every one of the twelve positions**.

Make rates land where Phase 1 puts them for 20 ft (tour ~14%, mid ~6%, hcp30
~4–5%), which is a real check on the whole chain: the same calibrated error
models, run over a LiDAR surface instead of an analytic one, reproduce the
planar make rates.

## Where you putt from matters more than how hard you hit it

On hole_07 the spread between the easiest and hardest clock position is **5.5
points of make% for tour** (16.6% at 11 o'clock against 11.0% at 7 o'clock) —
larger than the entire benefit of pace optimization within a position. The
best putters are the ones who can exploit that; hcp30's spread is only 1.4
points, because their dispersion swamps the geometry.

The pattern from Phase 1 repeats: the positions above the hole (11, 12, 1
o'clock — putting downhill) are consistently the *easiest*, not the hardest.
The fall line acts as a lateral attractor, so direction errors partly
self-correct on a downhill putt and compound on an uphill one. What downhill
putts cost is 3-putt risk, not make rate.

## Caveats specific to real greens

- **The grid is not the green.** green_maps buffers each green by 12 m of
  collar before gridding, so the raw NaN mask is 3–5× the green's area.
  putttron erodes it back to recover the putting surface, and checks the result
  against the area the pipeline reports (all 20 greens within 2%). Anything
  reading the mask directly would have balls rolling through the rough at green
  speed. See `docs/physics.md`.
- **Vertical fidelity is macro-contour only** (source RMSE 5–10 cm). Micro-break
  is below the noise floor. These greens tell you about the big movements, not
  about the last two feet.
- The Phase 1 caveats still bind: optima are conditional on the 0.25 m
  follow-up lag policy, and the direction-error model is calibrated on flat
  greens, so it does not grow with break.
- Both runs happen to put the pin somewhere gentle (1.4% and 2.0%). A pin cut
  on hole_07's 10.9% face is a different story: no pace can hold a ball there,
  and putttron reports `solve_failed` for every position rather than inventing
  an answer. `putttron serve` warns before you run it.

## Reproduce

```
git clone https://github.com/jhoblitt/crooked_tree_greens ~/github/crooked_tree_greens

go run ./cmd/putttron greensweep -green hole_01 -pin "0,0" -ringft 20 \
  -skills tour,mid,hcp30 -stimp 10 -trials 8000 -fieldtrials 1500 -fieldsweeps 5 \
  -seed 1 -out results/greens -tag hole01-ring20
go run ./cmd/putttron greensweep -green hole_07 -pin "0,0" -ringft 20 \
  -skills tour,mid,hcp30 -stimp 10 -trials 8000 -fieldtrials 1500 -fieldsweeps 5 \
  -seed 1 -out results/greens -tag hole07-ring20
```

Or explore interactively — pick a green, click a pin, see the dispersion drawn
on the green itself:

```
go run ./cmd/putttron serve
```
