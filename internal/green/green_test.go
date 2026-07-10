package green

import "testing"

func TestPlanar(t *testing.T) {
	p := NewPlanar(3, 0.55)

	// 3% grade: walking 10 m downhill (+X) drops 0.30 m; Y is level.
	if got := p.Elevation(10, 5) - p.Elevation(0, 5); got != -0.30 {
		t.Errorf("drop over 10 m at 3%% = %g, want -0.30", got)
	}
	if p.Elevation(2, -7) != p.Elevation(2, 7) {
		t.Error("elevation varies along Y on a fall-line-aligned plane")
	}

	gx, gy := p.Gradient(1, 1)
	if gx != -0.03 || gy != 0 {
		t.Errorf("gradient = (%g, %g), want (-0.03, 0)", gx, gy)
	}

	// Uniform friction: position- and direction-independent.
	if a, b := p.DecelCoeff(0, 0, 1, 0), p.DecelCoeff(3, -2, 0, 1); a != 0.55 || b != 0.55 {
		t.Errorf("DecelCoeff = %g, %g, want 0.55 uniform", a, b)
	}
}
