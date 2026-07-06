# putttron

Physics-based Monte Carlo simulation of golf putting.

Founding question: **for 10–20 ft putts, what is the ideal distance for the
ball to roll past the hole on missed putts?** Answered by simulating a ball
rolling on sloped greens (Penner/Holmes physics) under per-skill-level human
execution error models calibrated from published literature, and minimizing
expected strokes to hole out.

See [CLAUDE.md](CLAUDE.md) for the architecture and phase plan, and
`docs/literature.md` for the literature survey the error models are built on.

## Usage

```
go run ./cmd/putttron sweep   # run the parameter-matrix sweep
go run ./cmd/putttron report --calibration
```

Results land in `results/`.
