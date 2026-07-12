package geom

import "math"

// edgeKey identifies a grid edge exactly (no float comparisons when joining
// segments): the edge leaving node (ix, iy) toward +x (dir 0) or +y (dir 1),
// in the internally padded index space.
type edgeKey struct {
	ix, iy, dir int
}

type segment struct{ from, to edgeKey }

// Contours extracts the iso-lines of g at level t as closed rings (implicitly
// closed: the last vertex is not a repeat of the first). Every segment is
// emitted with the z ≥ t region on its left, so outer rings come out CCW and
// holes CW — RingsArea of the result is therefore the superlevel-set area
// even when it has holes.
//
// The grid is padded internally with one ring of below-threshold nodes, so a
// region touching the data edge still closes.
func Contours(g *Grid, t float64) [][]Pt {
	if g == nil || g.Nx < 2 || g.Ny < 2 {
		return nil
	}
	nx, ny := g.Nx+2, g.Ny+2

	below := t - 1
	for _, v := range g.Z {
		if v-1 < below {
			below = v - 1
		}
	}
	val := func(ix, iy int) float64 {
		if ix == 0 || iy == 0 || ix == nx-1 || iy == ny-1 {
			return below
		}
		return g.Z[(iy-1)*g.Nx+(ix-1)]
	}
	nodeX := func(ix int) float64 { return g.X0 + float64(ix-1)*g.Dx }
	nodeY := func(iy int) float64 { return g.Y0 + float64(iy-1)*g.Dy }

	// point of an edge crossing, derived from the key alone so the two cells
	// sharing an edge always agree to the last bit.
	point := func(k edgeKey) Pt {
		a := val(k.ix, k.iy)
		if k.dir == 0 {
			b := val(k.ix+1, k.iy)
			f := (t - a) / (b - a)
			return Pt{nodeX(k.ix) + f*g.Dx, nodeY(k.iy)}
		}
		b := val(k.ix, k.iy+1)
		f := (t - a) / (b - a)
		return Pt{nodeX(k.ix), nodeY(k.iy) + f*g.Dy}
	}

	var segs []segment
	for iy := 0; iy < ny-1; iy++ {
		for ix := 0; ix < nx-1; ix++ {
			v00, v10 := val(ix, iy), val(ix+1, iy)
			v11, v01 := val(ix+1, iy+1), val(ix, iy+1)
			code := 0
			if v00 >= t {
				code |= 1
			}
			if v10 >= t {
				code |= 2
			}
			if v11 >= t {
				code |= 4
			}
			if v01 >= t {
				code |= 8
			}
			if code == 0 || code == 15 {
				continue
			}
			bottom := edgeKey{ix, iy, 0}
			top := edgeKey{ix, iy + 1, 0}
			left := edgeKey{ix, iy, 1}
			right := edgeKey{ix + 1, iy, 1}

			switch code {
			case 1:
				segs = append(segs, segment{bottom, left})
			case 2:
				segs = append(segs, segment{right, bottom})
			case 3:
				segs = append(segs, segment{right, left})
			case 4:
				segs = append(segs, segment{top, right})
			case 6:
				segs = append(segs, segment{top, bottom})
			case 7:
				segs = append(segs, segment{top, left})
			case 8:
				segs = append(segs, segment{left, top})
			case 9:
				segs = append(segs, segment{bottom, top})
			case 11:
				segs = append(segs, segment{right, top})
			case 12:
				segs = append(segs, segment{left, right})
			case 13:
				segs = append(segs, segment{bottom, right})
			case 14:
				segs = append(segs, segment{left, bottom})
			case 5, 10:
				// Saddle: the cell-center average decides whether the two
				// same-side corners connect through the middle.
				center := (v00 + v10 + v11 + v01) / 4
				joined := center >= t
				if code == 5 {
					if joined {
						segs = append(segs, segment{bottom, right}, segment{top, left})
					} else {
						segs = append(segs, segment{bottom, left}, segment{top, right})
					}
				} else {
					if joined {
						segs = append(segs, segment{left, bottom}, segment{right, top})
					} else {
						segs = append(segs, segment{right, bottom}, segment{left, top})
					}
				}
			}
		}
	}
	if len(segs) == 0 {
		return nil
	}

	// Each crossing edge is shared by exactly two cells and is the exit of
	// one segment and the entry of the other, so successor lookup is total.
	next := make(map[edgeKey]int, len(segs))
	for i, s := range segs {
		next[s.from] = i
	}
	used := make([]bool, len(segs))
	var rings [][]Pt
	for i := range segs {
		if used[i] {
			continue
		}
		var ring []Pt
		for j := i; !used[j]; {
			used[j] = true
			ring = append(ring, point(segs[j].from))
			k, ok := next[segs[j].to]
			if !ok {
				break
			}
			j = k
		}
		if len(ring) >= 3 {
			rings = append(rings, ring)
		}
	}
	return rings
}

// RingsArea sums the signed areas of rings: CW holes subtract from CCW outer
// rings, so the result is the area of the region they bound.
func RingsArea(rings [][]Pt) float64 {
	var a float64
	for _, r := range rings {
		a += PolyArea(r)
	}
	return a
}

// GridFromFunc samples f on an nx×ny grid spanning [minX,maxX]×[minY,maxY].
func GridFromFunc(minX, minY, maxX, maxY float64, nx, ny int, f func(x, y float64) float64) *Grid {
	g := &Grid{
		X0: minX, Y0: minY,
		Dx: (maxX - minX) / float64(nx-1),
		Dy: (maxY - minY) / float64(ny-1),
		Nx: nx, Ny: ny,
		Z: make([]float64, nx*ny),
	}
	for iy := 0; iy < ny; iy++ {
		for ix := 0; ix < nx; ix++ {
			g.Z[iy*nx+ix] = f(g.X0+float64(ix)*g.Dx, g.Y0+float64(iy)*g.Dy)
		}
	}
	return g
}

// Centroid is the mean of pts (the zero point for an empty set).
func Centroid(pts []Pt) Pt {
	if len(pts) == 0 {
		return Pt{}
	}
	var c Pt
	for _, p := range pts {
		c.X += p.X
		c.Y += p.Y
	}
	n := float64(len(pts))
	return Pt{c.X / n, c.Y / n}
}

// Dist is the Euclidean distance between two points.
func Dist(a, b Pt) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }
