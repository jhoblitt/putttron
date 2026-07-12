// Package coursetest builds synthetic greens-repository directories
// (index.json + heightmap.npz + meta.json) so course loading and everything
// downstream can be tested without the real LiDAR data.
package coursetest

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/jhoblitt/putttron/internal/npz/npztest"
)

// Spec describes one synthetic green. Z gives the LOCAL-frame elevation at
// node (i, j) (row 0 = north edge); NaN marks a node off-green (nil = all
// valid). Build re-adds UTM-like offsets so loader recentering is exercised.
type Spec struct {
	Label                string
	Hole                 int
	Rows, Cols           int
	CellSize             float64 // 0 -> 0.25
	Z                    func(i, j int) float64
	NaN                  func(i, j int) bool
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
		gdir := filepath.Join(greensDir, sp.Label)
		if err := os.MkdirAll(gdir, 0o755); err != nil {
			return err
		}

		// Local origin: pick the grid center-ish, offset per green so
		// labels do not collide in UTM space.
		off := float64(gi) * 100
		lox := utmX0 + off + float64(sp.Cols)/2*dxm
		loy := utmY0 + off - float64(sp.Rows)/2*dxm
		x0 := utmX0 + off
		y0 := utmY0 + off

		z := make([]float32, sp.Rows*sp.Cols)
		for i := 0; i < sp.Rows; i++ {
			for j := 0; j < sp.Cols; j++ {
				if sp.NaN != nil && sp.NaN(i, j) {
					z[i*sp.Cols+j] = float32(math.NaN())
					continue
				}
				local := 0.0
				if sp.Z != nil {
					local = sp.Z(i, j)
				}
				z[i*sp.Cols+j] = float32(local + utmZ0)
			}
		}

		members := map[string]npztest.Member{
			"z":            {Shape: []int{sp.Rows, sp.Cols}, F32: z},
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
		ig.GridShape = []int{sp.Rows, sp.Cols}
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

// PlaneSpec is a convenience: a fully-valid tilted plane in the local frame,
// slopePct% grade falling toward +X (east), matching green.NewPlanar's
// convention.
func PlaneSpec(label string, rows, cols int, slopePct float64) Spec {
	return Spec{
		Label: label, Hole: 1, Rows: rows, Cols: cols,
		Z: func(i, j int) float64 {
			// Local x of node (i, j) given Build's origin layout.
			dxm := 0.25
			x := float64(j)*dxm - float64(cols)/2*dxm
			return -slopePct / 100 * x
		},
	}
}
