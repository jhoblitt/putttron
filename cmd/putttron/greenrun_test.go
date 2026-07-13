package main

import (
	"math"
	"testing"

	"github.com/jhoblitt/putttron/internal/green"
	"github.com/jhoblitt/putttron/internal/physics"
)

// planeGreen is a tilted plane as a heightmap: slopePct% falling toward +X,
// exactly what green.NewPlanar defines, optionally masked off beyond edgeX.
func planeGreen(t *testing.T, slopePct, edgeX float64) *green.Heightmap {
	t.Helper()
	const rows, cols, dx = 120, 120, 0.25
	x0, y0 := -15.0, 15.0
	z := make([]float64, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			x := x0 + float64(j)*dx
			if x > edgeX {
				z[i*cols+j] = math.NaN()
				continue
			}
			z[i*cols+j] = -slopePct / 100 * x
		}
	}
	h, err := green.NewHeightmap(z, rows, cols, x0, y0, dx, physics.DecelFromStimp(10))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// The fall-line clock on a real green must mean exactly what it meant on the
// planar sweep: 12 o'clock is directly upslope of the pin (so it putts
// downhill), and the hours run clockwise from there.
func TestClockGeometryMatchesPhase1(t *testing.T) {
	surf := planeGreen(t, 3, 100)
	pin := physics.Vec2{}
	const distFt = 20.0
	distM := distFt * 0.3048

	balls, fallback := ringBalls(surf, pin, distM, []int{12, 3, 6, 9}, "fall")
	if fallback {
		t.Fatal("a 3% plane fell back to compass bearings")
	}
	for i, hour := range []int{12, 3, 6, 9} {
		got := balls[i].Pos
		if hour == 9 {
			// Phase 1 never simulated 9 o'clock (it mirrors 3 on a plane).
			want := physics.Vec2{Y: -distM}
			if got.Sub(want).Norm() > 1e-6 {
				t.Errorf("9 o'clock at %+v, want %+v", got, want)
			}
			continue
		}
		want := clockPos(hour, distM)
		if got.Sub(want).Norm() > 1e-6 {
			t.Errorf("%d o'clock at %+v, but the planar sweep puts it at %+v", hour, got, want)
		}
	}

	// The 12 o'clock ball must be above the hole: putting from it runs downhill.
	up := surf.Elevation(balls[0].Pos.X, balls[0].Pos.Y)
	if up <= surf.Elevation(pin.X, pin.Y) {
		t.Error("the 12 o'clock ball is not above the hole")
	}
}

func TestClockCompassAndFlatFallback(t *testing.T) {
	surf := planeGreen(t, 3, 100)
	pin := physics.Vec2{}

	balls, _ := ringBalls(surf, pin, 3, []int{12, 3}, "compass")
	if math.Abs(balls[0].Pos.Y-3) > 1e-6 || math.Abs(balls[0].Pos.X) > 1e-6 {
		t.Errorf("compass 12 o'clock at %+v, want grid north", balls[0].Pos)
	}
	if math.Abs(balls[1].Pos.X-3) > 1e-6 || math.Abs(balls[1].Pos.Y) > 1e-6 {
		t.Errorf("compass 3 o'clock at %+v, want grid east", balls[1].Pos)
	}

	flat := planeGreen(t, 0, 100)
	balls, fallback := ringBalls(flat, pin, 3, []int{12}, "fall")
	if !fallback {
		t.Fatal("a flat green did not report the fall-line fallback")
	}
	if balls[0].Mode != "compass" {
		t.Errorf("fallback ball mode = %q, want compass", balls[0].Mode)
	}
	if math.Abs(balls[0].Pos.Y-3) > 1e-6 {
		t.Errorf("fallback 12 o'clock at %+v, want grid north", balls[0].Pos)
	}
}

// Ring positions that land off the green are reported, not silently dropped.
func TestRingOffGreenAndPinValidation(t *testing.T) {
	surf := planeGreen(t, 2, 1.0) // valid only west of x = 1 m
	pin := physics.Vec2{X: -3}

	balls, _ := ringBalls(surf, pin, 6, []int{12, 3, 6, 9}, "compass")
	off := 0
	for _, b := range balls {
		if b.Status == statusOffGreen {
			off++
		}
	}
	if off == 0 {
		t.Error("no ring position was marked off-green on a half-masked surface")
	}
	if len(balls) != 4 {
		t.Errorf("ring returned %d balls, want all 4 hours accounted for", len(balls))
	}

	if err := validatePin(surf, physics.Vec2{X: -5}); err != nil {
		t.Errorf("a pin in the middle of the green was rejected: %v", err)
	}
	if err := validatePin(surf, physics.Vec2{X: 5}); err == nil {
		t.Error("a pin off the green was accepted")
	}
	if err := validatePin(surf, physics.Vec2{X: 0.9}); err == nil {
		t.Error("a pin hard against the edge was accepted")
	}
}

// Seeds must pair trials across the rollout axis (common random numbers) and
// separate everything else.
func TestSeedScheme(t *testing.T) {
	pin := physics.Vec2{X: 1.25, Y: -0.5}
	base := cellSeed(1, "hole_07", pin, 0, "tour")
	if base != cellSeed(1, "hole_07", pin, 0, "tour") {
		t.Error("cell seed is not stable across calls")
	}
	for _, tc := range []struct {
		name string
		seed uint64
	}{
		{"another ball", cellSeed(1, "hole_07", pin, 1, "tour")},
		{"another skill", cellSeed(1, "hole_07", pin, 0, "mid")},
		{"another green", cellSeed(1, "hole_01", pin, 0, "tour")},
		{"another pin", cellSeed(1, "hole_07", physics.Vec2{X: 1.26, Y: -0.5}, 0, "tour")},
		{"another master seed", cellSeed(2, "hole_07", pin, 0, "tour")},
	} {
		if tc.seed == base {
			t.Errorf("%s collides with the base cell seed", tc.name)
		}
	}
	if fieldSeed(1, "hole_07", pin, "tour") == base {
		t.Error("the field seed collides with a cell seed")
	}
}
