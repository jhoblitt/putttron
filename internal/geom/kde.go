package geom

import (
	"math"
	"slices"
)

// Grid is a regular 2-D scalar field. (X0, Y0) is the position of node
// (0, 0); Z is row-major, Z[iy*Nx+ix].
type Grid struct {
	X0, Y0, Dx, Dy float64
	Nx, Ny         int
	Z              []float64
}

func (g *Grid) At(ix, iy int) float64 { return g.Z[iy*g.Nx+ix] }

// Bilinear samples the field, clamping queries outside the grid to its edge.
func (g *Grid) Bilinear(x, y float64) float64 {
	if g.Nx < 2 || g.Ny < 2 {
		return g.Z[0]
	}
	fx := math.Max(0, math.Min(float64(g.Nx-1), (x-g.X0)/g.Dx))
	fy := math.Max(0, math.Min(float64(g.Ny-1), (y-g.Y0)/g.Dy))
	ix := min(int(fx), g.Nx-2)
	iy := min(int(fy), g.Ny-2)
	tx := fx - float64(ix)
	ty := fy - float64(iy)
	return (1-ty)*((1-tx)*g.At(ix, iy)+tx*g.At(ix+1, iy)) +
		ty*((1-tx)*g.At(ix, iy+1)+tx*g.At(ix+1, iy+1))
}

// bwFloor keeps a near-degenerate sample (a dying-pace cluster strung out
// along the hole line has almost no spread across it) from collapsing the
// kernel to zero width.
const bwFloor = 0.01 // m

// BandwidthScott is the per-axis 2-D Scott rule, h_i = σ_i·n^(−1/6).
func BandwidthScott(pts []Pt) (hx, hy float64) {
	n := len(pts)
	if n < 2 {
		return bwFloor, bwFloor
	}
	var mx, my float64
	for _, p := range pts {
		mx += p.X
		my += p.Y
	}
	nf := float64(n)
	mx /= nf
	my /= nf
	var vx, vy float64
	for _, p := range pts {
		dx, dy := p.X-mx, p.Y-my
		vx += dx * dx
		vy += dy * dy
	}
	f := math.Pow(nf, -1.0/6.0)
	return math.Max(math.Sqrt(vx/nf)*f, bwFloor), math.Max(math.Sqrt(vy/nf)*f, bwFloor)
}

// kernelTrunc is where the Gaussian kernel is cut off, in bandwidths; the
// discarded mass (~1.3e-4 in 2-D) is left off rather than renormalized.
const kernelTrunc = 4

// KDEGrid evaluates the product-Gaussian kernel density of pts on an nx×ny
// grid covering their bounding box padded by 3.5 bandwidths. The field
// integrates to ~1: Σ Z·Dx·Dy ≈ 1.
func KDEGrid(pts []Pt, hx, hy float64, nx, ny int) *Grid {
	if len(pts) == 0 || nx < 2 || ny < 2 {
		return &Grid{Dx: 1, Dy: 1, Nx: 1, Ny: 1, Z: []float64{0}}
	}
	minX, maxX := pts[0].X, pts[0].X
	minY, maxY := pts[0].Y, pts[0].Y
	for _, p := range pts[1:] {
		minX = math.Min(minX, p.X)
		maxX = math.Max(maxX, p.X)
		minY = math.Min(minY, p.Y)
		maxY = math.Max(maxY, p.Y)
	}
	pad := 3.5 * math.Max(hx, hy)
	minX, maxX = minX-pad, maxX+pad
	minY, maxY = minY-pad, maxY+pad

	g := &Grid{
		X0: minX, Y0: minY,
		Dx: (maxX - minX) / float64(nx-1),
		Dy: (maxY - minY) / float64(ny-1),
		Nx: nx, Ny: ny,
		Z: make([]float64, nx*ny),
	}
	norm := 1 / (float64(len(pts)) * 2 * math.Pi * hx * hy)
	rx := int(math.Ceil(kernelTrunc * hx / g.Dx))
	ry := int(math.Ceil(kernelTrunc * hy / g.Dy))
	for _, p := range pts {
		cx := int(math.Round((p.X - g.X0) / g.Dx))
		cy := int(math.Round((p.Y - g.Y0) / g.Dy))
		for iy := max(0, cy-ry); iy <= min(g.Ny-1, cy+ry); iy++ {
			dy := (g.Y0 + float64(iy)*g.Dy - p.Y) / hy
			for ix := max(0, cx-rx); ix <= min(g.Nx-1, cx+rx); ix++ {
				dx := (g.X0 + float64(ix)*g.Dx - p.X) / hx
				g.Z[iy*g.Nx+ix] += norm * math.Exp(-0.5*(dx*dx+dy*dy))
			}
		}
	}
	return g
}

// HDRLevel returns the density threshold whose superlevel set holds fraction
// mass of the sample — the highest-density region of Hyndman, "Computing and
// Graphing Highest Density Regions", Am. Stat. 50 (1996): the (1−mass)
// quantile of the density evaluated at the sample points.
func HDRLevel(g *Grid, pts []Pt, mass float64) float64 {
	if len(pts) == 0 {
		return math.Inf(1)
	}
	d := make([]float64, len(pts))
	for i, p := range pts {
		d[i] = g.Bilinear(p.X, p.Y)
	}
	slices.Sort(d)
	k := int((1 - mass) * float64(len(d)))
	return d[min(max(k, 0), len(d)-1)]
}
