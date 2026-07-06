// Package green provides putting surfaces in a local Z-up tangent frame.
// Units are meters; the hole is conventionally at the origin.
package green

// Surface is a putting surface. Gradient returns ∂z/∂x, ∂z/∂y
// (dimensionless). DecelCoeff returns the flat-green-equivalent rolling
// deceleration a_d (m/s²) at a point for a roll direction (unit vector);
// direction dependence exists so grain models can plug in later.
type Surface interface {
	Elevation(x, y float64) float64
	Gradient(x, y float64) (gx, gy float64)
	DecelCoeff(x, y, dirX, dirY float64) float64
}

// Planar is a uniformly tilted plane with uniform friction. The fall line is
// along +X: elevation decreases as x increases, so +X is downhill and a ball
// at negative x is above the hole. Slope is expressed as % grade (rise/run ×
// 100, i.e. 100·tanθ) — the unit golfers, green books, and green_maps use.
type Planar struct {
	slope float64 // grade as a fraction (tan of the slope angle)
	decel float64 // a_d, m/s²
}

func NewPlanar(slopePct, decel float64) *Planar {
	return &Planar{slope: slopePct / 100, decel: decel}
}

func (p *Planar) Elevation(x, y float64) float64 { return -p.slope * x }

func (p *Planar) Gradient(x, y float64) (float64, float64) { return -p.slope, 0 }

func (p *Planar) DecelCoeff(x, y, dirX, dirY float64) float64 { return p.decel }
