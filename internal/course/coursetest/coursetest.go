// Package coursetest builds synthetic greens-repository directories
// (index.json + heightmap.npz + meta.json) so course loading and everything
// downstream can be tested without the real LiDAR data.
//
// Fixtures mirror the real pipeline's contract: the gridded surface is the
// putting green DILATED by a collar buffer, and the loader erodes that back
// off. A fixture green of radius r therefore writes a support disc of radius
// r + collar.
package coursetest

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/jhoblitt/putttron/internal/npz/npztest"
)

// CollarM is the buffer the real pipeline adds around each green polygon
// before gridding (green_maps: "collar/surrounds provide sim boundary and
// fitting support").
const CollarM = 12.0

// Spec describes one synthetic green. Z gives the local-frame elevation in
// meters at a point; the putting surface is a disc of GreenRadiusM about the
// origin, and the modeled grid extends CollarM beyond it.
type Spec struct {
	Label                string
	Hole                 int
	GreenRadiusM         float64 // default 9 m (a ~250 m² green)
	CellSize             float64 // default 0.25
	Z                    func(x, y float64) float64
	Flags                []string
	NeedsReview          bool
	SlopeMaxSustainedPct float64
}

const (
	utmX0 = 494000.0
	utmY0 = 3581000.0
	utmZ0 = 715.0
)

// Build writes a greens repository under dir.
func Build(dir string, specs []Spec) error {
	greensDir := filepath.Join(dir, "outputs", "greens")
	if err := os.MkdirAll(greensDir, 0o755); err != nil {
		return err
	}

	type idxGreen struct {
		Label     string `json:"label"`
		Hole      int    `json:"hole"`
		Dir       string `json:"dir"`
		Artifacts struct {
			HeightmapNPZ string `json:"heightmap_npz"`
			MetaJSON     string `json:"meta_json"`
		} `json:"artifacts"`
		LocalOriginUTM []float64 `json:"local_origin_utm"`
		GridShape      []int     `json:"grid_shape"`
		Flags          []string  `json:"flags"`
		NeedsReview    bool      `json:"needs_review"`
	}
	index := map[string]any{
		"course":         "coursetest synthetic",
		"crs_horizontal": "EPSG:6341",
		"cell_size_m":    0.25,
	}
	var idxGreens []idxGreen

	for gi, sp := range specs {
		dxm := sp.CellSize
		if dxm == 0 {
			dxm = 0.25
		}
		greenR := sp.GreenRadiusM
		if greenR == 0 {
			greenR = 9
		}
		support := greenR + CollarM
		half := support + dxm // one cell of always-invalid border
		n := 2*int(math.Ceil(half/dxm)) + 1

		gdir := filepath.Join(greensDir, sp.Label)
		if err := os.MkdirAll(gdir, 0o755); err != nil {
			return err
		}

		// Local origin sits at the grid center; the file stores UTM, so the
		// loader has to recenter to get back to (0, 0).
		off := float64(gi) * 100
		x0 := utmX0 + off
		y0 := utmY0 + off
		lox := x0 + float64(n-1)/2*dxm
		loy := y0 - float64(n-1)/2*dxm

		z := make([]float32, n*n)
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				x := float64(j)*dxm - float64(n-1)/2*dxm
				y := float64(n-1)/2*dxm - float64(i)*dxm
				if math.Hypot(x, y) > support {
					z[i*n+j] = float32(math.NaN())
					continue
				}
				local := 0.0
				if sp.Z != nil {
					local = sp.Z(x, y)
				}
				z[i*n+j] = float32(local + utmZ0)
			}
		}

		members := map[string]npztest.Member{
			"z":            {Shape: []int{n, n}, F32: z},
			"x0":           {Shape: []int{}, F64: []float64{x0}},
			"y0":           {Shape: []int{}, F64: []float64{y0}},
			"dx":           {Shape: []int{}, F64: []float64{dxm}},
			"local_origin": {Shape: []int{3}, F64: []float64{lox, loy, utmZ0}},
			"crs":          {Str: "EPSG:6341 (NAD83(2011)/UTM 12N) + NAVD88 m (EPSG:5703)"},
			"layout":       {Str: "row-major north-up: z[0,0] node at (x0, y0), row step -dx north->south"},
		}
		if err := npztest.Write(filepath.Join(gdir, "heightmap.npz"), members, false); err != nil {
			return err
		}

		meta := map[string]any{
			"label":                   sp.Label,
			"hole":                    sp.Hole,
			"needs_review":            sp.NeedsReview,
			"flags":                   sp.Flags,
			"slope_max_sustained_pct": sp.SlopeMaxSustainedPct,
			"green_area_m2":           math.Pi * greenR * greenR,
			"fit_rms_m":               0.03,
			"vertical_fidelity":       "synthetic",
		}
		mb, err := json.MarshalIndent(meta, "", " ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(gdir, "meta.json"), mb, 0o644); err != nil {
			return err
		}

		var ig idxGreen
		ig.Label = sp.Label
		ig.Hole = sp.Hole
		ig.Dir = fmt.Sprintf("outputs/greens/%s", sp.Label)
		ig.Artifacts.HeightmapNPZ = ig.Dir + "/heightmap.npz"
		ig.Artifacts.MetaJSON = ig.Dir + "/meta.json"
		ig.LocalOriginUTM = []float64{lox, loy, utmZ0}
		ig.GridShape = []int{n, n}
		ig.Flags = sp.Flags
		ig.NeedsReview = sp.NeedsReview
		idxGreens = append(idxGreens, ig)
	}

	index["greens"] = idxGreens
	b, err := json.MarshalIndent(index, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(greensDir, "index.json"), b, 0o644)
}

// PlaneSpec is a tilted plane falling slopePct% toward +X, matching
// green.NewPlanar's convention.
func PlaneSpec(label string, greenRadiusM, slopePct float64) Spec {
	return Spec{
		Label: label, Hole: 1, GreenRadiusM: greenRadiusM,
		Z: func(x, y float64) float64 { return -slopePct / 100 * x },
	}
}

// SteepSpec is a green with a face too steep to hold a ball at normal green
// speeds — the ball runs off. Real courses have these (crooked_tree flags
// several); the simulator must handle them without hanging.
func SteepSpec(label string, greenRadiusM float64) Spec {
	return Spec{
		Label: label, Hole: 2, GreenRadiusM: greenRadiusM,
		Z: func(x, y float64) float64 {
			// Gentle on the west half, a 12% ramp falling east.
			if x < 0 {
				return -0.01 * x
			}
			return -0.12 * x
		},
		Flags:                []string{"max_sustained_12.0%_gt_8%"},
		NeedsReview:          true,
		SlopeMaxSustainedPct: 12,
	}
}
