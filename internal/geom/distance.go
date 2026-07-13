package geom

import "math"

// DistanceTransform returns each cell's Euclidean distance to the nearest
// false cell of mask, in world units (cell is the grid spacing). False cells
// get 0. Everything outside the grid counts as false, so a region touching an
// edge is not credited with room it does not have.
//
// Exact and O(n), via the separable parabola-envelope transform of
// Felzenszwalb & Huttenlocher, "Distance Transforms of Sampled Functions"
// (2012) — the usual chamfer masks are only approximate.
func DistanceTransform(mask []bool, nx, ny int, cell float64) []float64 {
	// Any real squared distance is under nx²+ny², so this stands in for
	// infinity while staying small enough that adding q² is exact.
	inf := 4 * float64(nx*nx+ny*ny)

	f := make([]float64, nx*ny)
	for i, ok := range mask {
		if ok {
			f[i] = inf
		}
	}

	col := make([]float64, ny)
	for ix := 0; ix < nx; ix++ {
		for iy := 0; iy < ny; iy++ {
			col[iy] = f[iy*nx+ix]
		}
		d := dt1d(col, inf)
		for iy := 0; iy < ny; iy++ {
			f[iy*nx+ix] = d[iy]
		}
	}
	row := make([]float64, nx)
	for iy := 0; iy < ny; iy++ {
		copy(row, f[iy*nx:(iy+1)*nx])
		d := dt1d(row, inf)
		copy(f[iy*nx:(iy+1)*nx], d)
	}

	out := make([]float64, nx*ny)
	for iy := 0; iy < ny; iy++ {
		for ix := 0; ix < nx; ix++ {
			i := iy*nx + ix
			edge := float64(min(min(ix, iy), min(nx-1-ix, ny-1-iy)) + 1)
			out[i] = math.Min(math.Sqrt(f[i]), edge) * cell
		}
	}
	return out
}

// dt1d is the 1-D squared-distance transform: the lower envelope of the
// parabolas rooted at each sample.
func dt1d(f []float64, inf float64) []float64 {
	n := len(f)
	d := make([]float64, n)
	v := make([]int, n)
	z := make([]float64, n+1)
	k := 0
	z[0], z[1] = -inf, inf
	for q := 1; q < n; q++ {
		s := ((f[q] + float64(q*q)) - (f[v[k]] + float64(v[k]*v[k]))) / float64(2*q-2*v[k])
		for s <= z[k] {
			k--
			s = ((f[q] + float64(q*q)) - (f[v[k]] + float64(v[k]*v[k]))) / float64(2*q-2*v[k])
		}
		k++
		v[k], z[k], z[k+1] = q, s, inf
	}
	k = 0
	for q := 0; q < n; q++ {
		for z[k+1] < float64(q) {
			k++
		}
		dq := float64(q - v[k])
		d[q] = dq*dq + f[v[k]]
	}
	return d
}
