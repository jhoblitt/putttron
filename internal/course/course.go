// Package course loads a greens repository — the outputs/greens directory
// tree the green_maps LiDAR pipeline emits (index.json enumerating greens,
// per-green heightmap.npz + meta.json) — into putttron surfaces, recentered
// to each green's local frame.
package course

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/npz"
)

// layoutContract is what the loader requires of the npz layout string; any
// drift in grid orientation upstream must fail loudly, not misread the map.
const layoutContract = "row-major north-up"

// CollarBufferM is how far the green_maps pipeline buffers each green polygon
// before gridding it ("collar/surrounds provide sim boundary and fitting
// support"). The gridded mask is therefore NOT the green: it is the green
// dilated by this much. Eroding it back recovers the putting surface — a
// claim the loader checks against the green area the pipeline reports, so a
// change in the upstream buffer shows up as a warning rather than a green
// silently three times its real size.
const CollarBufferM = 12.0

// greenAreaTolerance is how far the recovered putting surface may fall from
// the pipeline's own green area before the loader complains.
const greenAreaTolerance = 0.20

type Index struct {
	Course        string      `json:"course"`
	CRSHorizontal string      `json:"crs_horizontal"`
	CellSizeM     float64     `json:"cell_size_m"`
	Greens        []GreenInfo `json:"greens"`

	// Dir is the repository root the index was loaded from (not part of
	// the JSON).
	Dir string `json:"-"`
}

type GreenInfo struct {
	Label     string `json:"label"`
	Hole      int    `json:"hole"`
	Dir       string `json:"dir"`
	Artifacts struct {
		HeightmapNPZ string `json:"heightmap_npz"`
		PinZonesNPZ  string `json:"pin_zones_npz"`
		MetaJSON     string `json:"meta_json"`
	} `json:"artifacts"`
	LocalOriginUTM   [3]float64 `json:"local_origin_utm"`
	GridShape        [2]int     `json:"grid_shape"`
	SlopeMeanPct     float64    `json:"slope_mean_pct"`
	ElevationRangeM  float64    `json:"elevation_range_m"`
	FitRMSM          float64    `json:"fit_rms_m"`
	LegalPinAreaM2   float64    `json:"legal_pin_area_m2"`
	LegalPinFraction float64    `json:"legal_pin_fraction"`
	ScarceLegalArea  bool       `json:"scarce_legal_area"`
	Flags            []string   `json:"flags"`
	NeedsReview      bool       `json:"needs_review"`
}

type Meta struct {
	SlopeMeanPct           float64     `json:"slope_mean_pct"`
	SlopeMaxPct            float64     `json:"slope_max_pct"`
	SlopeMaxSustainedPct   float64     `json:"slope_max_sustained_pct"`
	FitRMSM                float64     `json:"fit_rms_m"`
	ElevationRangeOnGreenM float64     `json:"elevation_range_on_green_m"`
	GreenAreaM2            float64     `json:"green_area_m2"`
	PinZones               PinZoneMeta `json:"pin_zones"`
	Flags                  []string    `json:"flags"`
	NeedsReview            bool        `json:"needs_review"`
	VerticalFidelity       string      `json:"vertical_fidelity"`
}

// PinZoneMeta is the meta.json summary of a green's legal hole-location map
// (green_maps Stage 4.5).
type PinZoneMeta struct {
	Definition      string                 `json:"definition"`
	EdgeSetbackM    float64                `json:"edge_setback_m"`
	CupBenchRadiusM float64                `json:"cup_bench_radius_m"`
	HeadlineTier    string                 `json:"headline_tier"`
	LegalAreaM2     float64                `json:"legal_area_m2"`
	LegalFraction   float64                `json:"legal_fraction"`
	ScarceLegalArea bool                   `json:"scarce_legal_area"`
	Tiers           map[string]PinTierMeta `json:"tiers"`
}

type PinTierMeta struct {
	SlopeMaxPct     float64 `json:"slope_max_pct"`
	AreaM2          float64 `json:"area_m2"`
	FractionOfGreen float64 `json:"fraction_of_green"`
	NZones          int     `json:"n_zones"`
}

// Green is one loaded green: its surface in the local frame (origin at the
// green centroid) plus provenance for manifests.
type Green struct {
	Info        GreenInfo
	Meta        Meta
	Surf        *green.Heightmap
	Pins        *PinZones // legal hole-location tiers; nil if the green has none
	GreenAreaM2 float64   // putting surface recovered from the buffered grid
	NPZPath     string
	NPZSize     int64
	NPZSHA256   string
}

// LoadIndex reads outputs/greens/index.json under repoDir.
func LoadIndex(repoDir string) (*Index, error) {
	path := filepath.Join(repoDir, "outputs", "greens", "index.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	if len(idx.Greens) == 0 {
		return nil, fmt.Errorf("%s: no greens listed", path)
	}
	idx.Dir = repoDir
	return &idx, nil
}

// LoadGreen loads one green by label, recentering the grid to its local
// origin so the green centroid is (0, 0) at elevation ~0.
func LoadGreen(idx *Index, label string, decel float64) (*Green, error) {
	var info *GreenInfo
	for i := range idx.Greens {
		if idx.Greens[i].Label == label {
			info = &idx.Greens[i]
			break
		}
	}
	if info == nil {
		var have []string
		for _, g := range idx.Greens {
			have = append(have, g.Label)
		}
		return nil, fmt.Errorf("green %q not in index (have: %s)", label, strings.Join(have, ", "))
	}

	npzPath := filepath.Join(idx.Dir, filepath.FromSlash(info.Artifacts.HeightmapNPZ))
	raw, err := os.ReadFile(npzPath)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	arrays, err := npz.ReadFile(npzPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", npzPath, err)
	}

	layout, ok := arrays["layout"]
	if !ok || !strings.Contains(layout.Str, layoutContract) || !strings.Contains(layout.Str, "row step -dx") {
		return nil, fmt.Errorf("%s: layout %q does not match the %q north-to-south contract",
			npzPath, layout.Str, layoutContract)
	}
	z, ok := arrays["z"]
	if !ok || len(z.Shape) != 2 {
		return nil, fmt.Errorf("%s: missing 2-D z array", npzPath)
	}
	rows, cols := z.Shape[0], z.Shape[1]
	if info.GridShape != [2]int{0, 0} && (rows != info.GridShape[0] || cols != info.GridShape[1]) {
		return nil, fmt.Errorf("%s: z shape (%d,%d) does not match index grid_shape %v",
			npzPath, rows, cols, info.GridShape)
	}
	scalar := func(name string) (float64, error) {
		a, ok := arrays[name]
		if !ok || len(a.Data) != 1 {
			return 0, fmt.Errorf("%s: missing scalar %q", npzPath, name)
		}
		return a.Data[0], nil
	}
	x0, err := scalar("x0")
	if err != nil {
		return nil, err
	}
	y0, err := scalar("y0")
	if err != nil {
		return nil, err
	}
	dx, err := scalar("dx")
	if err != nil {
		return nil, err
	}
	if dx <= 0 {
		return nil, fmt.Errorf("%s: cell size %g must be positive", npzPath, dx)
	}
	lo, ok := arrays["local_origin"]
	if !ok || len(lo.Data) != 3 {
		return nil, fmt.Errorf("%s: missing local_origin[3]", npzPath)
	}

	zLocal := make([]float64, len(z.Data))
	for i, v := range z.Data {
		zLocal[i] = v - lo.Data[2] // NaN propagates
	}
	surf, err := green.NewHeightmap(zLocal, rows, cols, x0-lo.Data[0], y0-lo.Data[1], dx, decel)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", npzPath, err)
	}

	g := &Green{
		Info:      *info,
		Surf:      surf,
		NPZPath:   npzPath,
		NPZSize:   int64(len(raw)),
		NPZSHA256: hex.EncodeToString(sum[:]),
	}

	metaPath := filepath.Join(idx.Dir, filepath.FromSlash(info.Artifacts.MetaJSON))
	if mb, err := os.ReadFile(metaPath); err == nil {
		if err := json.Unmarshal(mb, &g.Meta); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v (continuing without meta)\n", metaPath, err)
			g.Meta = Meta{}
		}
	} else {
		fmt.Fprintf(os.Stderr, "warning: %s missing (continuing without meta)\n", metaPath)
	}

	g.GreenAreaM2 = surf.InsetGreen(CollarBufferM)
	if want := g.Meta.GreenAreaM2; want > 0 {
		if off := math.Abs(g.GreenAreaM2-want) / want; off > greenAreaTolerance {
			fmt.Fprintf(os.Stderr,
				"warning: %s: eroding the %g m collar gives a %.0f m² putting surface but the pipeline reports %.0f m² (%.0f%% off) — has the upstream buffer changed?\n",
				label, CollarBufferM, g.GreenAreaM2, want, 100*off)
		}
	}

	if p := info.Artifacts.PinZonesNPZ; p != "" {
		pins, err := loadPinZones(filepath.Join(idx.Dir, filepath.FromSlash(p)), x0, y0, dx, lo.Data, rows, cols)
		if err != nil {
			// A green without usable pin zones is still usable for putting.
			fmt.Fprintf(os.Stderr, "warning: %s: %v (continuing without pin zones)\n", label, err)
		} else {
			pins.Meta = g.Meta.PinZones
			g.Pins = pins
		}
	}
	return g, nil
}

// GitDescribe reports the greens repository's version for manifests;
// "unknown" when the directory is not a git checkout.
func GitDescribe(repoDir string) string {
	out, err := exec.Command("git", "-C", repoDir, "describe", "--always", "--dirty").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
