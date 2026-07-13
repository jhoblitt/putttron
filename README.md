# putttron

Physics-based Monte Carlo simulation of golf putting.

**Live results**: the interactive pace matrix (optimal pace by skill, putt
length, slope, and direction) is served at
<https://jhoblitt.github.io/putttron/>, deployed from
`results/pace-matrix.html` on every push to main.

Founding question: **for 10–20 ft putts, what is the ideal distance for the
ball to roll past the hole on missed putts?** Answered by simulating a ball
rolling on sloped greens (Penner/Holmes physics) under per-skill-level human
execution error models calibrated from published literature, and minimizing
expected strokes to hole out.

See [CLAUDE.md](CLAUDE.md) for the architecture and phase plan, and
`docs/literature.md` for the literature survey the error models are built on.

**Phase 1 answer** (planar greens, Stimp 8–12, slopes 0–5% grade, all
orientations — details in `results/findings-phase1.md`): the optimal
target rollout is skill- and length-dependent, not a constant. Tour-caliber
putters should play 10–20 ft putts to finish ~14–15 in past; a ~10
handicap ~12 in at 10 ft tapering to ~5 in at 20 ft; 20+ handicaps should
play dying pace at 15–20 ft. Green speed barely moves the optimum, and the
stakes near it are tiny (being 4 in off costs ~0.004 putts — one stroke
per ~250 putts). Pelz's 17 inches is fine advice for strong putters and
too aggressive for weak ones at 15 ft and beyond.

## Usage

Planar greens (the founding question):

```
go run ./cmd/putttron calibrate    # calibration gate vs published make-% tables
go run ./cmd/putttron sweep        # run the parameter-matrix sweep
go run ./cmd/putttron dispersion -in results/sweep-planar-v3.csv   # where the misses finish
go run ./cmd/putttron report -in results/sweep-planar-v3.csv -dispersion results/dispersion-v1
```

On the live page, **clicking any heatmap cell** opens the dispersion map for
that putt: every simulated miss's resting place, the total spread (convex
hull, in ft²), and 50/80/95% probability contours.

Real greens (Phase 2) — LiDAR surfaces from
[crooked_tree_greens](https://github.com/jhoblitt/crooked_tree_greens):

```
git clone https://github.com/jhoblitt/crooked_tree_greens ~/github/crooked_tree_greens

go run ./cmd/putttron serve        # interactive explorer at http://127.0.0.1:7888
go run ./cmd/putttron greensweep -green hole_07 -pin "0,0" -ringft 20   # headless equivalent
```

`serve` renders the green (slope shading, 2.5 cm contours, fall lines), lets
you click a pin and ball positions — or take the defaults: a ring at a chosen
distance with a clock face of positions, where 12 o'clock sits directly
upslope of the pin — then reports each position's optimal pace, make%, 3-putt%,
off-green%, and draws the miss dispersion on the green itself. Every run shows
its seed and hands back the `greensweep` command that reproduces it. Committed
examples are in `results/greens/`.

Results land in `results/`.
