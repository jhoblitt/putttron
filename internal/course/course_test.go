package course

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhoblitt/putttron/internal/course/coursetest"
	"github.com/jhoblitt/putttron/internal/npz/npztest"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	specs := []coursetest.Spec{
		coursetest.PlaneSpec("hole_01", 40, 40, 2),
		{
			Label: "hole_02", Hole: 2, Rows: 40, Cols: 40,
			Z: func(i, j int) float64 { return 0.01 * float64(i) },
			NaN: func(i, j int) bool {
				return i < 4 || j < 4 || i >= 36 || j >= 36
			},
			Flags:                []string{"max_sustained_10.9%_gt_8%"},
			NeedsReview:          true,
			SlopeMaxSustainedPct: 10.9,
		},
	}
	if err := coursetest.Build(dir, specs); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadIndex(t *testing.T) {
	idx, err := LoadIndex(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Greens) != 2 {
		t.Fatalf("loaded %d greens, want 2", len(idx.Greens))
	}
	if idx.Greens[0].Label != "hole_01" || idx.Greens[1].Label != "hole_02" {
		t.Errorf("labels = %q, %q", idx.Greens[0].Label, idx.Greens[1].Label)
	}
	if !idx.Greens[1].NeedsReview || len(idx.Greens[1].Flags) != 1 {
		t.Errorf("hole_02 review flags not carried: %+v", idx.Greens[1])
	}
	if _, err := LoadIndex(t.TempDir()); err == nil {
		t.Error("missing index.json did not error")
	}
}

// The loaded surface must be recentered: the green's local origin maps to
// (0, 0) at roughly zero elevation, even though the file carries UTM
// coordinates and ~715 m absolute elevations.
func TestLoadGreenRecenters(t *testing.T) {
	idx, err := LoadIndex(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	g, err := LoadGreen(idx, "hole_01", 0.55)
	if err != nil {
		t.Fatal(err)
	}
	if z := g.Surf.Elevation(0, 0); math.Abs(z) > 0.5 {
		t.Errorf("elevation at the local origin = %.3f m, want ~0 (recentered)", z)
	}
	rows, cols := g.Surf.GridSize()
	if rows != 40 || cols != 40 {
		t.Errorf("grid %dx%d, want 40x40", rows, cols)
	}
	if !g.Surf.OnGreen(0, 0) {
		t.Error("center of a fully-valid green reported off-green")
	}
	// The synthetic plane falls 2% toward +X. The tolerance absorbs the
	// float32 quantization of the absolute (~715 m) elevations the file
	// format stores: ~4e-5 m per node over 0.25 m cells is ~2e-4 of grade,
	// three orders below the source LiDAR's own 5-10 cm vertical RMSE.
	gx, gy := g.Surf.Gradient(0, 0)
	if math.Abs(gx+0.02) > 1e-3 || math.Abs(gy) > 1e-3 {
		t.Errorf("gradient = (%.5f, %.5f), want (-0.02, 0)", gx, gy)
	}
	if g.NPZSHA256 == "" || g.NPZSize == 0 || !strings.HasSuffix(g.NPZPath, "heightmap.npz") {
		t.Errorf("provenance incomplete: %+v", struct {
			P string
			S int64
			H string
		}{g.NPZPath, g.NPZSize, g.NPZSHA256})
	}
	if g.Meta.SlopeMaxSustainedPct != 0 && g.Meta.VerticalFidelity == "" {
		t.Error("meta.json not loaded")
	}
}

func TestLoadGreenMaskAndMeta(t *testing.T) {
	idx, err := LoadIndex(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	g, err := LoadGreen(idx, "hole_02", 0.55)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Meta.NeedsReview || g.Meta.SlopeMaxSustainedPct != 10.9 {
		t.Errorf("meta not loaded: %+v", g.Meta)
	}
	if !g.Surf.OnGreen(0, 0) {
		t.Error("center reported off-green")
	}
	// The border four cells deep is NaN; the far corner must be off-green.
	x0, y0 := g.Surf.Origin()
	if g.Surf.OnGreen(x0, y0) {
		t.Error("NaN corner reported on-green")
	}
	if _, err := LoadGreen(idx, "hole_99", 0.55); err == nil {
		t.Error("unknown label did not error")
	}
}

// A grid whose layout string does not promise north-up rows must be rejected
// rather than silently read upside down.
func TestLoadGreenRejectsForeignLayout(t *testing.T) {
	dir := fixture(t)
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "outputs", "greens", "hole_01", "heightmap.npz")
	z := make([]float32, 40*40)
	members := map[string]npztest.Member{
		"z":            {Shape: []int{40, 40}, F32: z},
		"x0":           {Shape: []int{}, F64: []float64{0}},
		"y0":           {Shape: []int{}, F64: []float64{0}},
		"dx":           {Shape: []int{}, F64: []float64{0.25}},
		"local_origin": {Shape: []int{3}, F64: []float64{0, 0, 0}},
		"layout":       {Str: "column-major south-up: who knows"},
	}
	if err := npztest.Write(path, members, false); err != nil {
		t.Fatal(err)
	}
	_, err = LoadGreen(idx, "hole_01", 0.55)
	if err == nil || !strings.Contains(err.Error(), "layout") {
		t.Errorf("foreign layout error = %v, want a layout complaint", err)
	}
}

func TestGitDescribe(t *testing.T) {
	if got := GitDescribe(t.TempDir()); got != "unknown" {
		t.Errorf("GitDescribe on a non-repo = %q, want \"unknown\"", got)
	}
}

// TestRealGreens exercises the loader against the actual LiDAR data when a
// clone is available; CI has none, so it skips.
func TestRealGreens(t *testing.T) {
	dir := os.Getenv("PUTTTRON_GREENS_DIR")
	if dir == "" {
		t.Skip("set PUTTTRON_GREENS_DIR to a crooked_tree_greens clone")
	}
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range idx.Greens {
		g, err := LoadGreen(idx, info.Label, 0.55)
		if err != nil {
			t.Errorf("%s: %v", info.Label, err)
			continue
		}
		if z := g.Surf.Elevation(0, 0); math.Abs(z) > 2 {
			t.Errorf("%s: elevation at local origin = %.2f m, want within 2 m of 0", info.Label, z)
		}
		rows, cols := g.Surf.GridSize()
		if rows != info.GridShape[0] || cols != info.GridShape[1] {
			t.Errorf("%s: grid %dx%d, index says %v", info.Label, rows, cols, info.GridShape)
		}
	}
}
