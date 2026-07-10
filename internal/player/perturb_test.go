package player

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestDirSigmaAt(t *testing.T) {
	s := Skill{DirSigmaDeg: 1.5, DirSigmaDegPerM: 0.4}
	if got := s.DirSigmaAt(0); math.Abs(got-1.5) > 1e-12 {
		t.Errorf("σ_dir(0) = %g, want 1.5", got)
	}
	want := math.Sqrt(1.5*1.5 + (0.4*5)*(0.4*5))
	if got := s.DirSigmaAt(5); math.Abs(got-want) > 1e-12 {
		t.Errorf("σ_dir(5) = %g, want %g", got, want)
	}
}

// The sampled errors must realize the profile's sigmas: direction error is
// Gaussian around the aim with sd σ_dir(L), and the fractional error on v²
// has sd DistSigmaPct (Bansal & Broadie form).
func TestPerturbMoments(t *testing.T) {
	s := Skill{DirSigmaDeg: 2.0, DirSigmaDegPerM: 0.3, DistSigmaPct: 0.08, DistSigmaFloor: 0.02}
	aim := Aim{Dir: 0.7, Speed: 2.2}
	const dist, n = 4.0, 200000
	rng := rand.New(rand.NewPCG(3, 4))

	var sumD, sumD2, sumF, sumF2 float64
	for i := 0; i < n; i++ {
		dir, speed := s.Perturb(aim, dist, rng)
		d := dir - aim.Dir
		sumD += d
		sumD2 += d * d
		f := speed*speed/(aim.Speed*aim.Speed) - 1 // realized fractional v² error
		sumF += f
		sumF2 += f * f
	}
	meanD, meanF := sumD/n, sumF/n
	sdD := math.Sqrt(sumD2/n - meanD*meanD)
	sdF := math.Sqrt(sumF2/n - meanF*meanF)

	wantSdD := s.DirSigmaAt(dist) * math.Pi / 180
	if math.Abs(meanD) > 3*wantSdD/math.Sqrt(n) {
		t.Errorf("direction error biased: mean %g rad", meanD)
	}
	if math.Abs(sdD-wantSdD)/wantSdD > 0.02 {
		t.Errorf("direction sd = %g rad, want %g (±2%%)", sdD, wantSdD)
	}
	if math.Abs(sdF-s.DistSigmaPct)/s.DistSigmaPct > 0.02 {
		t.Errorf("v² fractional sd = %g, want %g (±2%%)", sdF, s.DistSigmaPct)
	}
}

// Below floor/pct meters the absolute floor takes over from the % error.
func TestPerturbDistanceFloor(t *testing.T) {
	s := Skill{DistSigmaPct: 0.08, DistSigmaFloor: 0.03}
	aim := Aim{Dir: 0, Speed: 1.0}
	const dist, n = 0.15, 100000 // floor/dist = 0.20 > 0.08
	rng := rand.New(rand.NewPCG(5, 6))

	var sum, sum2 float64
	for i := 0; i < n; i++ {
		_, speed := s.Perturb(aim, dist, rng)
		f := speed*speed - 1
		sum += f
		sum2 += f * f
	}
	mean := sum / n
	sd := math.Sqrt(sum2/n - mean*mean)
	want := s.DistSigmaFloor / dist
	if math.Abs(sd-want)/want > 0.03 {
		t.Errorf("floored v² sd = %g, want %g (±3%%)", sd, want)
	}
}

func TestProfileByName(t *testing.T) {
	for _, name := range []string{"tour", "scratch", "mid", "high", "hcp30"} {
		sk, ok := ProfileByName(name)
		if !ok || sk.Name != name {
			t.Errorf("ProfileByName(%q) = %+v, %t", name, sk, ok)
		}
	}
	if _, ok := ProfileByName("hustler"); ok {
		t.Error("unknown profile reported found")
	}
}
