package player

// Profiles are the Phase-1 skill ladder. Distance sigmas are the Bansal &
// Broadie (2008) ShotLink calibration (tour = their PGA pro, 6.5% of
// length; high = their ~90-shooter amateur, 8.5%). Direction sigmas are
// EFFECTIVE two-parameter models σ(L)² = σ0² + (σ1·L)², fitted by
// `putttron fit` to reproduce published make-%-by-distance tables on this
// simulator (tour RMS 0.8 points over 3–30 ft, high RMS 2.0), absorbing
// green-reading error and green imperfection, which B&B model separately
// (read error scales with break, hence the length-proportional term).
// Consistency check: tour σ(10ft) = 1.43° vs B&B's 1.0° execution sigma
// implies a ~1.0° read/imperfection component in quadrature — matching
// Gelman & Nolan's 1.5° angle-only fit and Karlsen's finding that reading,
// not stroke, dominates direction error. Scratch/mid interpolate the
// anchors at the same fractions as B&B's execution scale (20% and 60% of
// the tour→high gap).
// hcp30 represents Broadie's Am3 band — scores 98–120, roughly 26–45 hcp,
// centered ~30-35 — the HIGHEST handicap range with published putting data
// anywhere; the ladder deliberately stops at this calibration frontier.
// σ_dir fitted by `putttron fit` against a make% table synthesized from
// the Am3 anchors and Shot Scope 25-hcp data (docs/literature.md §2E);
// σ_dist tuned so the independent Am3 anchors validate (one-putt% ==
// three-putt% at 12 ft, ~2.7 avg putts from 40 ft).
var Profiles = []Skill{
	{Name: "tour", DirSigmaDeg: 1.361, DirSigmaDegPerM: 0.147, DistSigmaPct: 0.065, DistSigmaFloor: 0.02},
	{Name: "scratch", DirSigmaDeg: 1.477, DirSigmaDegPerM: 0.229, DistSigmaPct: 0.070, DistSigmaFloor: 0.02},
	{Name: "mid", DirSigmaDeg: 1.710, DirSigmaDegPerM: 0.393, DistSigmaPct: 0.077, DistSigmaFloor: 0.025},
	{Name: "high", DirSigmaDeg: 1.943, DirSigmaDegPerM: 0.557, DistSigmaPct: 0.085, DistSigmaFloor: 0.03},
	{Name: "hcp30", DirSigmaDeg: 2.370, DirSigmaDegPerM: 0.268, DistSigmaPct: 0.165, DistSigmaFloor: 0.04},
}

func ProfileByName(name string) (Skill, bool) {
	for _, s := range Profiles {
		if s.Name == name {
			return s, true
		}
	}
	return Skill{}, false
}
