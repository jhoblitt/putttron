// Package geom is a small 2-D geometry and scalar-field toolkit: convex
// hulls, polygon areas, ring decimation, Gaussian-KDE grids, and
// marching-squares contours. Coordinates are meters throughout.
package geom

import (
	"cmp"
	"slices"
)

type Pt struct{ X, Y float64 }

func cross(o, a, b Pt) float64 {
	return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X)
}

// ConvexHullIndices returns indices into pts of the convex hull in CCW order
// (Andrew's monotone chain), without repeating the first point. Degenerate
// inputs (n < 3 or all collinear) return the 0-2 extreme indices.
func ConvexHullIndices(pts []Pt) []int {
	idx := make([]int, len(pts))
	for i := range idx {
		idx[i] = i
	}
	slices.SortFunc(idx, func(a, b int) int {
		if c := cmp.Compare(pts[a].X, pts[b].X); c != 0 {
			return c
		}
		if c := cmp.Compare(pts[a].Y, pts[b].Y); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	uniq := idx[:0]
	for _, i := range idx {
		if len(uniq) > 0 && pts[i] == pts[uniq[len(uniq)-1]] {
			continue
		}
		uniq = append(uniq, i)
	}
	if len(uniq) <= 2 {
		return uniq
	}

	hull := make([]int, 0, len(uniq)+1)
	for _, i := range uniq {
		for len(hull) >= 2 && cross(pts[hull[len(hull)-2]], pts[hull[len(hull)-1]], pts[i]) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, i)
	}
	limit := len(hull) + 1
	for j := len(uniq) - 2; j >= 0; j-- {
		i := uniq[j]
		for len(hull) >= limit && cross(pts[hull[len(hull)-2]], pts[hull[len(hull)-1]], pts[i]) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, i)
	}
	return hull[:len(hull)-1]
}

// ConvexHull is a convenience returning the hull points themselves.
func ConvexHull(pts []Pt) []Pt {
	idx := ConvexHullIndices(pts)
	hull := make([]Pt, len(idx))
	for i, j := range idx {
		hull[i] = pts[j]
	}
	return hull
}

// PolyArea is the signed shoelace area of an implicitly-closed polygon
// (CCW positive). Fewer than 3 vertices -> 0.
func PolyArea(poly []Pt) float64 {
	if len(poly) < 3 {
		return 0
	}
	var s float64
	for i, p := range poly {
		q := poly[(i+1)%len(poly)]
		s += p.X*q.Y - q.X*p.Y
	}
	return s / 2
}

// DecimateRing subsamples a closed ring to at most max vertices by uniform
// stride, always keeping the first vertex; rings already <= max return
// unchanged.
func DecimateRing(ring []Pt, max int) []Pt {
	if len(ring) <= max {
		return ring
	}
	if max < 1 {
		max = 1
	}
	stride := (len(ring) + max - 1) / max
	out := make([]Pt, 0, max)
	for i := 0; i < len(ring); i += stride {
		out = append(out, ring[i])
	}
	return out
}
