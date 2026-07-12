package npz_test

import (
	"os"
	"slices"
	"testing"

	"github.com/jhoblitt/putttron/internal/npz"
)

// Checks the reader against an archive downloaded from the real green_maps
// pipeline; skipped when the file is absent so the suite stays hermetic.
func TestRealHole07File(t *testing.T) {
	const path = "/tmp/claude-1000/-home-jhoblitt-github-putttron/71d251d5-beff-4adb-b611-c2202f9e27b5/scratchpad/hole07.npz"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("real fixture unavailable: %v", err)
	}
	arrays, err := npz.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := arrays["z"].Shape; !slices.Equal(got, []int{206, 214}) {
		t.Errorf("z shape = %v, want [206 214]", got)
	}
	for key, want := range map[string]float64{"x0": 494416.25, "y0": 3581425.0, "dx": 0.25} {
		a := arrays[key]
		if len(a.Shape) != 0 || len(a.Data) != 1 || a.Data[0] != want {
			t.Errorf("%s = %+v, want scalar %v", key, a, want)
		}
	}
	if lo := arrays["local_origin"]; len(lo.Data) != 3 {
		t.Errorf("local_origin has %d values, want 3", len(lo.Data))
	}
	const wantLayout = "row-major north-up: z[0,0] node at (x0, y0), row step -dx north->south"
	if got := arrays["layout"].Str; got != wantLayout {
		t.Errorf("layout = %q, want %q", got, wantLayout)
	}
}
