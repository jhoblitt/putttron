package player

// Profiles are the Phase-1 skill ladder, anchored on the Bansal & Broadie
// (2008) ShotLink calibration: tour = their PGA pro (σ_dir 1.0°, distance
// error 6.5% of length), high = their ~90-shooter amateur (1.5°, 8.5%);
// scratch and mid are interpolations. See docs/literature.md §2 and §5;
// validated against published make-%-by-distance tables via `putttron
// calibrate`. Sigmas are total outcome dispersion (read + stroke).
var Profiles = []Skill{
	{Name: "tour", DirSigmaDeg: 1.00, DistSigmaPct: 0.065, DistSigmaFloor: 0.02},
	{Name: "scratch", DirSigmaDeg: 1.10, DistSigmaPct: 0.070, DistSigmaFloor: 0.02},
	{Name: "mid", DirSigmaDeg: 1.30, DistSigmaPct: 0.077, DistSigmaFloor: 0.025},
	{Name: "high", DirSigmaDeg: 1.50, DistSigmaPct: 0.085, DistSigmaFloor: 0.03},
}

func ProfileByName(name string) (Skill, bool) {
	for _, s := range Profiles {
		if s.Name == name {
			return s, true
		}
	}
	return Skill{}, false
}
