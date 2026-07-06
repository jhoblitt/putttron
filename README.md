# putttron

Physics-based Monte Carlo simulation of golf putting.

Founding question: **for 10–20 ft putts, what is the ideal distance for the
ball to roll past the hole on missed putts?** Answered by simulating a ball
rolling on sloped greens (Penner/Holmes physics) under per-skill-level human
execution error models calibrated from published literature, and minimizing
expected strokes to hole out.

See [CLAUDE.md](CLAUDE.md) for the architecture and phase plan, and
`docs/literature.md` for the literature survey the error models are built on.

**Phase 1 answer** (planar greens, Stimp 8–12, slopes 0–3°, all
orientations — details in `results/findings-phase1.md`): the optimal
target rollout is skill- and length-dependent, not a constant. Tour-caliber
putters should play 10–20 ft putts to finish ~12–16 in past; a ~10
handicap ~12 in at 10 ft tapering to ~4 in at 20 ft; 20+ handicaps should
play dying pace at 15–20 ft. Green speed barely moves the optimum, and the
optimum is a wide plateau (±4–8 in costs nothing measurable). Pelz's
17 inches is fine advice for strong putters and too aggressive for weak
ones at 15 ft and beyond.

## Usage

```
go run ./cmd/putttron sweep   # run the parameter-matrix sweep
go run ./cmd/putttron report --calibration
```

Results land in `results/`.
