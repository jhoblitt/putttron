# Literature survey

Sources for the physics model and the per-skill human error models, with the
specific numbers putttron uses. Tags: **[primary]** peer-reviewed / original
data, **[secondary]** reputable analysis of primary data, **[estimate]**
interpolated or weakly sourced.

## 1. Physics of ball–green interaction

### Penner 2002 [primary]

A.R. Penner, "The physics of putting," *Canadian Journal of Physics* 80(2):
83–96 (2002). doi:10.1139/P01-137. http://raypenner.com/golf-putting.pdf

- Ball modeled as uniform solid sphere in pure rolling; retarding force from
  green deformation, parameterized by ρ_g (normalized contact-point offset).
  Level-green deceleration: `a = −(5/7)·ρ_g·g` (his Eq. 5). The 5/7 is
  1/(1+I/mr²) for a solid sphere.
- Stimpmeter release speed **1.83 m/s** (from Holmes 1986). Working range
  0.065 < ρ_g < 0.196 (fast ~12 ft stimp → slow ~4 ft), average 0.131.
- Straight up/downhill: `a = −(5/7)·g·(ρ_g·cosφ + sinφ)` (Eq. 36). Downhill
  runaway when ρ_g < tan|φ| (Eq. 18).
- **Hole capture** (building on Holmes): center-hit critical speed
  **1.31 m/s** free-fall only, **1.63 m/s** all mechanisms (lip roll,
  bounce-in). Off-center (impact parameter δ, hole radius R_H = 5.40 cm),
  his Eq. 23: `v_c(δ) = 1.63·(1 − (δ/R_H)²)` m/s — quadratic falloff.
  Slope correction (Eq. 30): multiply by `(1 − cosβ_f·sinφ)^(−1/2)`; on a 5°
  green: 1.71 m/s uphill entry, 1.56 m/s downhill.
- Run-out asymmetry illustration: matched just-missed 10 ft putts on a 5°
  slope finish ~2.9 ft past uphill vs ~13.1 ft past downhill.

### Holmes 1991 [primary]

B.W. Holmes, "Putting: How a golf ball and hole interact," *American Journal
of Physics* 59(2): 129–136 (1991).

- Source of the capture-space analysis Penner fits; 1.31/1.63 m/s values.
- Effective hole size shrinks with entry speed: ~25% smaller at 0.65 m/s,
  ~4% at 0.23 m/s (via Sheppard/Hurrion, Wesson 2008).

### Stimp / green speeds [secondary unless noted]

- Municipal/public greens stimp 7–10 (avg ~9); PGA Tour weekly ~10.5–12;
  majors 11.5–13+. Bansal & Broadie [primary] used stimp 11 for pros,
  stimp 9 for amateurs.

### Skid phase & sidespin

- Skid ends and pure roll begins after ~10–20% of putt length (Cochran &
  Stobbs; Daish; via Penner — who then treats pure roll from launch as a
  justified approximation). Quintic tracking agrees [secondary].
- **Sidespin is negligible in putting**: every physics model surveyed
  (Penner, Holmes, Broadie) models direction error purely as launch-angle
  error with no curving-spin term. putttron does the same; the requested
  "side spin error" axis is therefore absorbed into the direction error.

## 2. Human error models per skill level

### Bansal & Broadie 2008 [primary] — the calibration anchor

M. Bansal & M. Broadie, "A Simulation Model to Analyze the Impact of Hole
Size on Putting in Golf," *Proc. 2008 Winter Simulation Conference*,
2826–2834. https://www.informs-sim.org/wsc08papers/356.pdf

Randomness on v² (∝ putt length) and on launch angle; calibrated to ~15,000
ShotLink/Golfmetrics putts (RMS error ≈ 0.03 putts):

| Parameter | PGA pro | Amateur (~90 shooter) |
|---|---|---|
| Direction error σ_α | 1.0° | 1.5° |
| Long-putt distance error (% of length) | 6.5% | 8.5% |
| Green-reading error σ_g | 0.15 | 0.25 |
| Green speed used | stimp 11 | stimp 9 |

### Broadie & Shin 2014 [primary]

M. Broadie & D. Shin, "Golf Analytics: A Random Putting Model and its
Applications to Optimal Targeting Strategy and Attribution Analysis"
(Columbia GSB working paper, 2014).

Within-tour spread: σ_α 0.626° (best) / 0.791° (avg) / 0.903° (worst);
velocity error ~3–6% of length, piecewise in v² (smaller for short putts).
Also the closest prior art on optimal pace — see §4.

### Karlsen, Smith & Nilsson 2008 [primary]

*J. Sports Sciences* 26(3): 243–250. 71 elite players, 1,301 putts at ~4 m:
pure **stroke** direction variability 0.39° SD (face angle 80% of it); total
outcome direction error is dominated by green reading/aim, not stroke. So
0.39° is a floor; on-course effective σ_α ≈ 1° for pros is consistent.

### Gelman & Nolan 2002 [primary, with caveat]

"A Probability Model for Golf Putting," *Teaching Statistics* 24(3): 93–95.
Angle-only model fit to pro make% data: σ = 1.5°. Inflated — all misses are
attributed to angle (no distance term); don't use as a pure aim sigma.

### Fearing, Acimovic & Graves 2011 [primary] — outcome model for validation

"How to Catch a Tiger: Understanding Putting Performance on the PGA Tour,"
*J. Quantitative Analysis in Sports* 7(1) Art. 5. doi:10.2202/1559-0410.1268

- Make probability: logistic in distance d (ft):
  logit = 7.31 − 5.58·ln d + 0.676·d − 0.0197·d² + 2.93e−4·d³ − 1.62e−6·d⁴.
- Leave distance after a miss: gamma, mean μ_d = exp(0.95 − 0.35·ln d +
  0.046·d − 1.6e−4·d²), shape k = 2.132 (CoV ≈ 0.68, mode 2–3 ft).

### Make % by distance [secondary — ShotLink + Broadie amateur data]

| Distance (ft) | Tour | 90-shooter |
|---|---|---|
| 3 | 96% | 84% |
| 4 | 88% | 65% |
| 5 | 77% | 50% |
| 6 | 66% | 39% |
| 8 | 50% | 27% |
| 10 | 40% | 20% |
| 15 | 23% | 11% |
| 20 | 15% | 6% |
| 30 | 7% | 2% |

(golfingfocus.com compilation of PGA ShotLink + Broadie *Every Shot Counts*.)
Three-putt/one-putt crossover: ~35 ft tour, ~16 ft for 15–19 handicap.

## 3. Grain

- Magnitude: up to 24–30 in roll-distance difference with vs. against grain
  on grainy/higher-cut greens [secondary, era/green-specific]; on modern
  low-cut bentgrass, directional stimp differences are sub-6-inch — small
  [primary, USGA/Asian Turfgrass measurement notes]. Bermuda break effect
  "10–20% of read break" is coaching folklore [estimate].
- Strong grain: bermudagrass (esp. ultradwarf), creeping bent, paspalum;
  weak: Poa annua, fescues (USGA "Grain on the Brain," 2018). Mowing
  height/brushing dominate (Lulis et al., Crop Science 2021 / ITSRJ 2022:
  maintenance effects on roll ~1 ft) [primary].
- **No peer-reviewed anisotropic-friction grain model exists.** Planned
  putttron parameterization (physically consistent Penner extension):
  ρ_g(ψ) = ρ_g0·(1 − k_grain·cosψ), ψ = angle between roll direction and
  down-grain; k_grain ~0.05–0.15 strong bermuda, ~0.01–0.03 modern bent.
  Phase 3; off by default.

## 4. Prior art on optimal pace

- **Pelz** (*Putt Like the Pros* 1989; *Putting Bible* 2000): 17 in past,
  from "lumpy donut" (foot-traffic ring 1–6 ft around the hole) + capture
  speed experiments. A single constant; critics note it's condition-
  dependent. Aimpoint teaches ~9 in; PGA Teaching Manual ~12 in.
- **Broadie & Shin 2014**: numerically minimize expected putts over target
  distance past hole (stimp 11, ShotLink-calibrated): optimum is NOT a
  constant — grows with green speed and slope, larger downhill than uphill;
  ~1–2 ft past for a 12 ft putt, 1–6 ft for a 3 ft putt depending on
  slope/angle. Stakes are small: optimal targeting saves ~0.014 putts on a
  5-footer.
- **Bansal & Broadie 2008**: optimal strategy targets past the hole with few
  putts left short; larger holes → more aggressive optimum.
- **Sheppard, Hurrion & Collinson 2016** [primary]: measured entry speeds on
  a 3° slope, stimp 10–10.5: finishing ~1 ft past needs 0.66 m/s entry
  uphill vs 0.29 m/s downhill — constant roll-past ≠ constant entry speed.
- **Grober 2011** (arXiv:1106.1698): aim-point geometry on planar surfaces
  ("target diamond" on the fall line); useful for read models, not pace.

## 5. Parameters adopted by putttron (Phase 1)

Direction error is an *effective* two-parameter model
σ_dir(L)² = σ0² + (σ1·L)²: B&B's execution sigmas (1.0°/1.5°) model green
reading as a separate term and their make% targets come from real greens,
so a simulator without a read model must inflate σ_dir — and read error
scales with break while execution error does not, hence the
length-proportional term. `putttron fit` measures the effective angular
make-window w(L) with one constant-σ simulation pass, converts the
published make% table to a needed σ per distance, and regresses σ² on L².

| Skill | σ0 (deg) | σ1 (deg/m) | σ at 10 ft | σ_dist (% of length) | Basis |
|---|---|---|---|---|---|
| tour | 1.361 | 0.147 | 1.43° | 6.5% | [primary] B&B pro, σ_dir fitted; RMS 0.8 pts (3–30 ft) |
| scratch | 1.477 | 0.229 | 1.63° | 7.0% | [estimate] interpolation |
| mid (~10 hcp) | 1.710 | 0.393 | 2.06° | 7.7% | [estimate] interpolation |
| high (~20 hcp / 90 shooter) | 1.943 | 0.557 | 2.58° | 8.5% | [primary] B&B amateur, σ_dir fitted; RMS 2.0 pts |

Cross-check: tour σ(10 ft) = 1.43° effective vs 1.0° execution implies a
~1.0° read/imperfection component in quadrature — consistent with Gelman &
Nolan's 1.5° angle-only fit and Karlsen's finding that reading, not stroke,
dominates direction error.

Implementation notes (all from the sources above):

1. Distance error is applied to v² (equivalently to rolled length), not to v.
2. Direction error is launch-angle only; no sidespin term (§1).
3. Green-reading error (σ_g) is a separate, slope-interacting term in
   Bansal & Broadie; Phase 1 folds it into the effective sigmas (they are
   calibrated against outcome data) and revisits in Phase 3.
4. Capture: v_c(δ) = 1.63·(1 − (δ/R_H)²) m/s with Penner's slope correction.
5. Validation targets: tour ≈ 40/23/15% makes at 10/15/20 ft, 90-shooter
   ≈ 20/11/6%; miss-leave distribution vs. Fearing's gamma (mode 2–3 ft).
